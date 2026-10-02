package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ModelProfile is one launch profile of the registry (task #5026,
// choose-model): which agent runs, with which model and effort, and what the
// profile is good for. Empty Model/Effort mean the agent's own default.
// Enabled is the global permission; Position orders lists.
type ModelProfile struct {
	Name        string
	Agent       string
	Model       string
	Effort      string
	Description string
	Enabled     bool
	Position    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Settings keys of the model-profile registry. The two defaults name the
// profile an orchestrator / a worker gets when nobody picks one; the marker
// records that the starter profiles were seeded, so a registry the human
// emptied stays empty across daemon restarts.
const (
	SettingDefaultOrchestratorProfile = "default_orchestrator_profile"
	SettingDefaultWorkerProfile       = "default_worker_profile"
	SettingModelProfilesSeeded        = "model_profiles_seeded"
)

const modelProfileColumns = `name, agent, model, effort, description, enabled, position, created_at, updated_at`

// ListModelProfiles returns the whole registry ordered by position, name.
func (s *Store) ListModelProfiles() ([]ModelProfile, error) {
	rows, err := s.db.Query(`SELECT ` + modelProfileColumns + ` FROM model_profiles ORDER BY position, name`)
	if err != nil {
		return nil, fmt.Errorf("query model profiles: %w", err)
	}
	defer rows.Close()

	var out []ModelProfile
	for rows.Next() {
		p, err := scanModelProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetModelProfile returns the profile named name, or ErrNotFound.
func (s *Store) GetModelProfile(name string) (ModelProfile, error) {
	row := s.db.QueryRow(`SELECT `+modelProfileColumns+` FROM model_profiles WHERE name = ?`, name)
	return scanModelProfile(row)
}

// CreateModelProfile inserts p, stamping created_at/updated_at with now.
// Returns ErrExists if the name is taken. Validation of the name, agent and
// effort is the caller's job.
func (s *Store) CreateModelProfile(p ModelProfile) error {
	return insertModelProfile(s.db, p, time.Now().UTC())
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertModelProfile(db execer, p ModelProfile, now time.Time) error {
	ts := now.Format(time.RFC3339Nano)
	_, err := db.Exec(
		`INSERT INTO model_profiles (`+modelProfileColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Agent, p.Model, p.Effort, p.Description, boolToInt(p.Enabled), p.Position, ts, ts,
	)
	if isUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("insert model profile: %w", err)
	}
	return nil
}

// UpdateModelProfile rewrites every mutable field of the profile named
// p.Name and refreshes updated_at. Returns ErrNotFound if it doesn't exist.
func (s *Store) UpdateModelProfile(p ModelProfile) error {
	res, err := s.db.Exec(
		`UPDATE model_profiles SET agent = ?, model = ?, effort = ?, description = ?,
		        enabled = ?, position = ?, updated_at = ?
		 WHERE name = ?`,
		p.Agent, p.Model, p.Effort, p.Description, boolToInt(p.Enabled), p.Position,
		time.Now().UTC().Format(time.RFC3339Nano), p.Name,
	)
	if err != nil {
		return fmt.Errorf("update model profile: %w", err)
	}
	return checkRowsAffected(res)
}

// DeleteModelProfile removes the profile named name. Returns ErrNotFound if
// it doesn't exist. Task allowlists naming it are left alone: the name simply
// stops matching.
func (s *Store) DeleteModelProfile(name string) error {
	res, err := s.db.Exec(`DELETE FROM model_profiles WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete model profile: %w", err)
	}
	return checkRowsAffected(res)
}

// seedModelProfiles are the starter profiles of an empty registry.
var seedModelProfiles = []ModelProfile{
	{Name: "claude-opus", Agent: "claude-code", Model: "opus",
		Description: "Сложные задачи: архитектура, рефакторинг, трудные баги, оркестрация"},
	{Name: "claude-sonnet", Agent: "claude-code", Model: "sonnet",
		Description: "Обычная разработка по ясному брифу, тесты, средние правки"},
	{Name: "codex", Agent: "codex",
		Description: "Codex с моделью по умолчанию: тексты, доки, механические правки"},
}

// SeedModelProfiles fills an empty registry with the starter profiles and
// points both default-profile settings at claude-opus when defaultAgent is
// claude-code, otherwise at the first starter profile of defaultAgent (left
// unset when there is none). It runs at most once per database: the
// model_profiles_seeded marker keeps a registry the human emptied empty. A
// registry that already has profiles is never touched; only the marker is
// written.
func (s *Store) SeedModelProfiles(defaultAgent string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if committed

	var marker int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, SettingModelProfilesSeeded).Scan(&marker); err != nil {
		return fmt.Errorf("read seed marker: %w", err)
	}
	if marker > 0 {
		return nil
	}

	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM model_profiles`).Scan(&count); err != nil {
		return fmt.Errorf("count model profiles: %w", err)
	}
	if count == 0 {
		now := time.Now().UTC()
		for i, p := range seedModelProfiles {
			p.Enabled = true
			p.Position = i
			if err := insertModelProfile(tx, p, now); err != nil {
				return err
			}
		}
		if def := seedDefaultProfile(defaultAgent); def != "" {
			for _, k := range []string{SettingDefaultOrchestratorProfile, SettingDefaultWorkerProfile} {
				if err := setSettingOn(tx, k, def); err != nil {
					return err
				}
			}
		}
	}
	if err := setSettingOn(tx, SettingModelProfilesSeeded, "true"); err != nil {
		return err
	}
	return tx.Commit()
}

// seedDefaultProfile picks the starter profile both defaults point at.
func seedDefaultProfile(defaultAgent string) string {
	if defaultAgent == "claude-code" {
		return "claude-opus"
	}
	for _, p := range seedModelProfiles {
		if p.Agent == defaultAgent {
			return p.Name
		}
	}
	return ""
}

func setSettingOn(db execer, key, value string) error {
	_, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

func scanModelProfile(row interface{ Scan(...any) error }) (ModelProfile, error) {
	var p ModelProfile
	var enabled int
	var created, updated string
	err := row.Scan(&p.Name, &p.Agent, &p.Model, &p.Effort, &p.Description, &enabled, &p.Position, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelProfile{}, ErrNotFound
	}
	if err != nil {
		return ModelProfile{}, fmt.Errorf("scan model profile: %w", err)
	}
	p.Enabled = enabled != 0
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return p, nil
}
