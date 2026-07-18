package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Repo represents a registered repository.
type Repo struct {
	ID            string
	Path          string
	DefaultBranch string
	AutoCleanup   bool
	Env           map[string]string
	Symlinks      []string
	PostCreate    []string
	CreatedAt     int64
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// AddRepo inserts a new repo. Returns ErrExists if the id is already taken.
func (s *Store) AddRepo(r Repo) error {
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().Unix()
	}

	envJSON, err := marshalOrEmpty(r.Env, "{}")
	if err != nil {
		return fmt.Errorf("marshal env: %w", err)
	}
	symlinksJSON, err := marshalOrEmpty(r.Symlinks, "[]")
	if err != nil {
		return fmt.Errorf("marshal symlinks: %w", err)
	}
	postCreateJSON, err := marshalOrEmpty(r.PostCreate, "[]")
	if err != nil {
		return fmt.Errorf("marshal post_create: %w", err)
	}

	defaultBranch := r.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	_, err = s.db.Exec(
		`INSERT INTO repos (id, path, default_branch, auto_cleanup, env, symlinks, post_create, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Path, defaultBranch, boolToInt(r.AutoCleanup), envJSON, symlinksJSON, postCreateJSON, r.CreatedAt,
	)
	if isUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("insert repo: %w", err)
	}
	return nil
}

// GetRepo returns the repo with the given id, or ErrNotFound.
func (s *Store) GetRepo(id string) (Repo, error) {
	row := s.db.QueryRow(
		`SELECT id, path, default_branch, auto_cleanup, env, symlinks, post_create, created_at
		 FROM repos WHERE id = ?`, id,
	)
	return scanRepo(row)
}

// ListRepos returns all repos ordered by id.
func (s *Store) ListRepos() ([]Repo, error) {
	rows, err := s.db.Query(
		`SELECT id, path, default_branch, auto_cleanup, env, symlinks, post_create, created_at
		 FROM repos ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query repos: %w", err)
	}
	defer rows.Close()

	var out []Repo
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateRepo updates an existing repo. Returns ErrNotFound if it doesn't exist.
func (s *Store) UpdateRepo(r Repo) error {
	envJSON, err := marshalOrEmpty(r.Env, "{}")
	if err != nil {
		return fmt.Errorf("marshal env: %w", err)
	}
	symlinksJSON, err := marshalOrEmpty(r.Symlinks, "[]")
	if err != nil {
		return fmt.Errorf("marshal symlinks: %w", err)
	}
	postCreateJSON, err := marshalOrEmpty(r.PostCreate, "[]")
	if err != nil {
		return fmt.Errorf("marshal post_create: %w", err)
	}

	res, err := s.db.Exec(
		`UPDATE repos SET path = ?, default_branch = ?, auto_cleanup = ?, env = ?, symlinks = ?, post_create = ?
		 WHERE id = ?`,
		r.Path, r.DefaultBranch, boolToInt(r.AutoCleanup), envJSON, symlinksJSON, postCreateJSON, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update repo: %w", err)
	}
	return checkRowsAffected(res)
}

// DeleteRepo deletes the repo with the given id. Returns ErrRepoInUse if the
// repo is referenced as a project's main repo or as one of its linked repos.
// Returns ErrNotFound if the repo doesn't exist.
func (s *Store) DeleteRepo(id string) error {
	projects, err := s.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	for _, p := range projects {
		if p.MainRepo == id {
			return ErrRepoInUse
		}
		for _, lr := range p.LinkedRepos {
			if lr == id {
				return ErrRepoInUse
			}
		}
	}

	res, err := s.db.Exec(`DELETE FROM repos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	return checkRowsAffected(res)
}

func scanRepo(row interface{ Scan(...any) error }) (Repo, error) {
	var r Repo
	var autoCleanup int
	var envJSON, symlinksJSON, postCreateJSON string

	err := row.Scan(&r.ID, &r.Path, &r.DefaultBranch, &autoCleanup, &envJSON, &symlinksJSON, &postCreateJSON, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Repo{}, ErrNotFound
	}
	if err != nil {
		return Repo{}, fmt.Errorf("scan repo: %w", err)
	}

	r.AutoCleanup = autoCleanup != 0

	if err := json.Unmarshal([]byte(envJSON), &r.Env); err != nil {
		return Repo{}, fmt.Errorf("unmarshal env: %w", err)
	}
	if err := json.Unmarshal([]byte(symlinksJSON), &r.Symlinks); err != nil {
		return Repo{}, fmt.Errorf("unmarshal symlinks: %w", err)
	}
	if err := json.Unmarshal([]byte(postCreateJSON), &r.PostCreate); err != nil {
		return Repo{}, fmt.Errorf("unmarshal post_create: %w", err)
	}

	return r, nil
}

func marshalOrEmpty(v any, empty string) (string, error) {
	switch t := v.(type) {
	case map[string]string:
		if len(t) == 0 {
			return empty, nil
		}
	case []string:
		if len(t) == 0 {
			return empty, nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func checkRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
