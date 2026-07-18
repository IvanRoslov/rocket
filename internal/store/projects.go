package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Project represents a registered project.
type Project struct {
	ID          string
	Name        string
	MainRepo    string
	LinkedRepos []string
	CreatedAt   int64
}

// Repos returns the project's main repo followed by its linked repos.
func (p Project) Repos() []string {
	out := make([]string, 0, 1+len(p.LinkedRepos))
	out = append(out, p.MainRepo)
	out = append(out, p.LinkedRepos...)
	return out
}

// AddProject inserts a new project. Returns ErrExists if the id is already taken.
func (s *Store) AddProject(p Project) error {
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}

	linkedJSON, err := marshalOrEmpty(p.LinkedRepos, "[]")
	if err != nil {
		return fmt.Errorf("marshal linked_repos: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO projects (id, name, main_repo, linked_repos, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.MainRepo, linkedJSON, p.CreatedAt,
	)
	if isUniqueViolation(err) {
		return ErrExists
	}
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

// GetProject returns the project with the given id, or ErrNotFound.
func (s *Store) GetProject(id string) (Project, error) {
	row := s.db.QueryRow(
		`SELECT id, name, main_repo, linked_repos, created_at FROM projects WHERE id = ?`, id,
	)
	return scanProject(row)
}

// ListProjects returns all projects ordered by id.
func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id, name, main_repo, linked_repos, created_at FROM projects ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateProject updates an existing project (including its linked repos).
// Returns ErrNotFound if it doesn't exist.
func (s *Store) UpdateProject(p Project) error {
	linkedJSON, err := marshalOrEmpty(p.LinkedRepos, "[]")
	if err != nil {
		return fmt.Errorf("marshal linked_repos: %w", err)
	}

	res, err := s.db.Exec(
		`UPDATE projects SET name = ?, main_repo = ?, linked_repos = ? WHERE id = ?`,
		p.Name, p.MainRepo, linkedJSON, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return checkRowsAffected(res)
}

// DeleteProject deletes the project with the given id. Returns ErrNotFound if
// it doesn't exist.
func (s *Store) DeleteProject(id string) error {
	res, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return checkRowsAffected(res)
}

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var linkedJSON string

	err := row.Scan(&p.ID, &p.Name, &p.MainRepo, &linkedJSON, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("scan project: %w", err)
	}

	if err := json.Unmarshal([]byte(linkedJSON), &p.LinkedRepos); err != nil {
		return Project{}, fmt.Errorf("unmarshal linked_repos: %w", err)
	}

	return p, nil
}
