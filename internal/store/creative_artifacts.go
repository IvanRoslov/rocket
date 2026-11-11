package store

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// CreativeArtifact is a named source or result in one immutable context.
type CreativeArtifact struct {
	ID               int64  `json:"id"`
	RunID            int64  `json:"run_id"`
	StageID          int64  `json:"stage_id,omitempty"`
	ItemID           int64  `json:"item_id,omitempty"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	CurrentVersionID int64  `json:"current_version_id"`
	CreatedAt        int64  `json:"created_at"`
}

// CreativeArtifactVersion metadata and bytes are never updated after publication.
type CreativeArtifactVersion struct {
	ID              int64  `json:"id"`
	ArtifactID      int64  `json:"artifact_id"`
	RunID           int64  `json:"run_id"`
	StageID         int64  `json:"stage_id,omitempty"`
	ItemID          int64  `json:"item_id,omitempty"`
	VersionNumber   int64  `json:"version_number"`
	Filename        string `json:"filename"`
	MIME            string `json:"mime"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
	StorageKey      string `json:"-"`
	AuthorSessionID string `json:"author_session_id,omitempty"`
	CreatedAt       int64  `json:"created_at"`
}

// CreativeArtifactPublication describes bytes already durably stored by the caller.
// ArtifactID appends to that artifact; zero finds/creates by context/name/kind.
type CreativeArtifactPublication struct {
	ArtifactID                          int64
	RunID, StageID, ItemID              int64
	Name, Kind                          string
	Filename, MIME                      string
	Size                                int64
	SHA256, StorageKey, AuthorSessionID string
}

const artifactColumns = `id,run_id,COALESCE(stage_id,0),COALESCE(item_id,0),name,kind,COALESCE(current_version_id,0),created_at`
const artifactVersionColumns = `v.id,v.artifact_id,a.run_id,COALESCE(a.stage_id,0),COALESCE(a.item_id,0),v.version_number,v.filename,v.mime,v.size,v.sha256,v.storage_key,v.author_session_id,v.created_at`

type artifactScanner interface{ Scan(...any) error }

func scanCreativeArtifact(row artifactScanner) (a CreativeArtifact, err error) {
	err = row.Scan(&a.ID, &a.RunID, &a.StageID, &a.ItemID, &a.Name, &a.Kind, &a.CurrentVersionID, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func scanCreativeArtifactVersion(row artifactScanner) (v CreativeArtifactVersion, err error) {
	err = row.Scan(&v.ID, &v.ArtifactID, &v.RunID, &v.StageID, &v.ItemID, &v.VersionNumber, &v.Filename, &v.MIME, &v.Size, &v.SHA256, &v.StorageKey, &v.AuthorSessionID, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}
func artifactOptionalID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Store) GetCreativeArtifact(id int64) (CreativeArtifact, error) {
	return scanCreativeArtifact(s.db.QueryRow(`SELECT `+artifactColumns+` FROM creative_artifacts WHERE id=?`, id))
}
func (s *Store) ListCreativeArtifacts(runID, stageID, itemID int64) ([]CreativeArtifact, error) {
	if _, err := s.ResolveCreativeContext(runID, stageID, itemID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+artifactColumns+` FROM creative_artifacts WHERE run_id=? AND COALESCE(stage_id,0)=? AND COALESCE(item_id,0)=? ORDER BY id`, runID, stageID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []CreativeArtifact{}
	for rows.Next() {
		a, err := scanCreativeArtifact(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
func (s *Store) GetCreativeArtifactVersion(id int64) (CreativeArtifactVersion, error) {
	return scanCreativeArtifactVersion(s.db.QueryRow(`SELECT `+artifactVersionColumns+` FROM creative_artifact_versions v JOIN creative_artifacts a ON a.id=v.artifact_id WHERE v.id=?`, id))
}
func (s *Store) ListCreativeArtifactVersions(artifactID int64) ([]CreativeArtifactVersion, error) {
	if _, err := s.GetCreativeArtifact(artifactID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+artifactVersionColumns+` FROM creative_artifact_versions v JOIN creative_artifacts a ON a.id=v.artifact_id WHERE a.id=? ORDER BY v.version_number`, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []CreativeArtifactVersion{}
	for rows.Next() {
		v, err := scanCreativeArtifactVersion(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, v)
	}
	return list, rows.Err()
}

// ValidateCreativeArtifactVersions checks ancestry even for an empty reference list.
// Stage contexts include their item descendants for whole-stage reviews.
// Item and run contexts match exactly; no latest/current lookup is performed.
func (s *Store) ValidateCreativeArtifactVersions(runID, stageID, itemID int64, ids []int64) error {
	if _, err := s.ResolveCreativeContext(runID, stageID, itemID); err != nil {
		return err
	}
	for _, id := range ids {
		v, err := s.GetCreativeArtifactVersion(id)
		if err != nil {
			return fmt.Errorf("artifact version %d: %w", id, err)
		}
		itemMatches := v.ItemID == itemID || (stageID > 0 && itemID == 0)
		if v.RunID != runID || v.StageID != stageID || !itemMatches {
			return fmt.Errorf("%w: artifact version %d belongs to another context", ErrCreativeInvalid, id)
		}
	}
	return nil
}

func (s *Store) PublishCreativeArtifact(p CreativeArtifactPublication) (CreativeArtifact, CreativeArtifactVersion, error) {
	fail := func(err error) (CreativeArtifact, CreativeArtifactVersion, error) {
		return CreativeArtifact{}, CreativeArtifactVersion{}, err
	}
	p.Name = strings.TrimSpace(p.Name)
	hash, err := hex.DecodeString(p.SHA256)
	if err != nil || len(hash) != 32 || p.Size <= 0 || strings.TrimSpace(p.Filename) == "" || p.MIME == "" || p.StorageKey == "" || p.StorageKey == "." || filepath.Base(p.StorageKey) != p.StorageKey || strings.ContainsAny(p.StorageKey, "/\\") {
		return fail(fmt.Errorf("%w: invalid artifact file metadata", ErrCreativeInvalid))
	}
	if _, err := s.ResolveCreativeContext(p.RunID, p.StageID, p.ItemID); err != nil {
		return fail(err)
	}
	if p.ArtifactID == 0 && (p.Name == "" || len(p.Name) > 512 || (p.Kind != "source" && p.Kind != "result")) {
		return fail(fmt.Errorf("%w: artifact needs name and source/result kind", ErrCreativeInvalid))
	}
	tx, err := s.creativeWrite(p.RunID)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	// Acquire SQLite's write lock before reads, avoiding deferred-transaction
	// snapshot upgrades racing concurrent publication of the same named result.
	if _, err := tx.Exec(`UPDATE creative_runs SET id=id WHERE id=?`, p.RunID); err != nil {
		return fail(err)
	}
	var a CreativeArtifact
	if p.ArtifactID != 0 {
		a, err = scanCreativeArtifact(tx.QueryRow(`SELECT `+artifactColumns+` FROM creative_artifacts WHERE id=?`, p.ArtifactID))
		if err != nil {
			return fail(err)
		}
		if a.RunID != p.RunID || a.StageID != p.StageID || a.ItemID != p.ItemID {
			return fail(fmt.Errorf("%w: artifact context mismatch", ErrCreativeInvalid))
		}
	} else {
		a, err = scanCreativeArtifact(tx.QueryRow(`SELECT `+artifactColumns+` FROM creative_artifacts WHERE run_id=? AND COALESCE(stage_id,0)=? AND COALESCE(item_id,0)=? AND name=? AND kind=?`, p.RunID, p.StageID, p.ItemID, p.Name, p.Kind))
		if errors.Is(err, ErrNotFound) {
			a = CreativeArtifact{RunID: p.RunID, StageID: p.StageID, ItemID: p.ItemID, Name: p.Name, Kind: p.Kind, CreatedAt: time.Now().Unix()}
			res, e := tx.Exec(`INSERT INTO creative_artifacts(run_id,stage_id,item_id,name,kind,created_at) VALUES(?,?,?,?,?,?)`, a.RunID, artifactOptionalID(a.StageID), artifactOptionalID(a.ItemID), a.Name, a.Kind, a.CreatedAt)
			if e != nil {
				return fail(e)
			}
			a.ID, err = res.LastInsertId()
		}
		if err != nil {
			return fail(err)
		}
	}
	v := CreativeArtifactVersion{ArtifactID: a.ID, RunID: a.RunID, StageID: a.StageID, ItemID: a.ItemID, Filename: p.Filename, MIME: p.MIME, Size: p.Size, SHA256: p.SHA256, StorageKey: p.StorageKey, AuthorSessionID: p.AuthorSessionID, CreatedAt: time.Now().Unix()}
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version_number),0)+1 FROM creative_artifact_versions WHERE artifact_id=?`, a.ID).Scan(&v.VersionNumber); err != nil {
		return fail(err)
	}
	res, err := tx.Exec(`INSERT INTO creative_artifact_versions(artifact_id,version_number,filename,mime,size,sha256,storage_key,author_session_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, a.ID, v.VersionNumber, v.Filename, v.MIME, v.Size, v.SHA256, v.StorageKey, v.AuthorSessionID, v.CreatedAt)
	if err != nil {
		return fail(err)
	}
	v.ID, err = res.LastInsertId()
	if err != nil {
		return fail(err)
	}
	if _, err := tx.Exec(`UPDATE creative_artifacts SET current_version_id=? WHERE id=?`, v.ID, a.ID); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	a.CurrentVersionID = v.ID
	return a, v, nil
}

func (s *Store) SetCreativeArtifactCurrent(artifactID, versionID int64) error {
	a, err := s.GetCreativeArtifact(artifactID)
	if err != nil {
		return err
	}
	tx, err := s.creativeWrite(a.RunID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE creative_artifacts SET current_version_id=? WHERE id=? AND EXISTS(SELECT 1 FROM creative_artifact_versions WHERE id=? AND artifact_id=?)`, versionID, artifactID, versionID, artifactID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
