package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise the normal Open upgrade, never a user's database. Capture every
// persisted run-graph column so a migration cannot silently drop review state.
func TestMigrateCreativeGlobalCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := sql.Open("sqlite", "file:"+escapeDSNPath(path)+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)`)
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names[:30] {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(body))
		exec(`INSERT INTO schema_migrations VALUES(?)`, i+1)
	}
	old := &Store{db: db}
	mustSetup(t, old)
	if err := old.AddProject(Project{ID: "other", MainRepo: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := old.AddSession(Session{ID: "creative-upgrade", Kind: "orchestrator", ProjectID: "billing", RepoID: "api", Agent: "fake", State: "running"}); err != nil {
		t.Fatal(err)
	}
	const stages = `[{"name":"Scenes","description":"Saved work","expected_artifacts":"Clip","repeated":true,"approval":"item"}]`
	exec(`INSERT INTO creative_pipelines VALUES(7,'billing','Same',?,10,20),(9,'other','Same',?,11,21),(100,'billing','Deleted',?,1,1)`, stages, stages, stages)
	exec(`DELETE FROM creative_pipelines WHERE id=100`)
	exec(`INSERT INTO creative_runs VALUES(17,'billing',7,'Same','Active','Saved brief','creative-upgrade',30,40)`)
	exec(`INSERT INTO creative_stages VALUES(27,17,0,?,'in_progress',65,'Working',3,'[47]')`, `{"name":"Saved scene","description":"Run snapshot","expected_artifacts":"Clip","repeated":true,"approval":"item"}`)
	exec(`INSERT INTO creative_items VALUES(37,17,27,0,'Shot','Saved shot','Clip','awaiting_approval',100,'Review',4,'[47]')`)
	exec(`INSERT INTO creative_artifacts VALUES(43,17,27,37,'Clip','result',47,50)`)
	exec(`INSERT INTO creative_artifact_versions VALUES(47,43,1,'clip.mp4','video/mp4',12,'hash','blob-preserved','creative-upgrade',51)`)
	exec(`INSERT INTO creative_session_contexts VALUES('creative-upgrade',17,27,37)`)
	exec(`INSERT INTO questions(id,body,title,asked_at) VALUES(57,'Review clip','Review clip',60)`)
	exec(`INSERT INTO creative_discussion_contexts VALUES(NULL,57,17,27,37,'[47]')`)
	exec(`INSERT INTO question_messages(question_id,author,kind,body,created_at) VALUES(57,'human','reply','Keep the opening',61)`)
	// Seed historical rows with historical SQL: current write APIs require
	// the current schema, which Open has deliberately not migrated yet.
	exec(`INSERT INTO messages(id,to_session,body,status,created_at) VALUES(67,'creative-upgrade','Review this exact version','queued',62)`)
	exec(`INSERT INTO creative_discussion_contexts VALUES(67,NULL,17,27,37,'[47]')`)

	tables := []string{"messages", "creative_runs", "creative_stages", "creative_items", "creative_artifacts", "creative_artifact_versions", "creative_session_contexts", "creative_discussion_contexts", "sessions", "questions", "question_messages"}
	dump := func(db *sql.DB, table string) string {
		t.Helper()
		columnsToRead := "*"
		if table == "creative_runs" {
			// Compare every pre-upgrade column; later additive migrations may
			// introduce new columns with their own independently checked defaults.
			columnsToRead = "id,project_id,pipeline_id,pipeline_name,name,brief,orchestrator_session_id,created_at,updated_at"
		}
		rows, err := db.Query(`SELECT ` + columnsToRead + ` FROM ` + table + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var result [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			result = append(result, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = dump(db, table)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, table := range tables {
		if got := dump(s.db, table); got != before[table] {
			t.Errorf("%s changed:\nbefore %s\nafter %s", table, before[table], got)
		}
	}
	run, err := s.GetCreativeRun(17)
	if err != nil || run.AllowedProfiles == nil || len(run.AllowedProfiles) != 0 {
		t.Fatalf("migrated run allowlist: %v %v", run.AllowedProfiles, err)
	}
	if run.ClosureState != "open" || run.AcceptedAt != 0 || run.AcceptanceRequestedAt != 0 {
		t.Fatalf("upgrade silently accepted work: %+v", run)
	}
	// Column removal must also remove the project FK without changing run FKs.
	var schema string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='creative_pipelines'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO creative_pipelines(name,stages,created_at,updated_at) VALUES('Global',?,70,70)`, stages); err != nil {
		t.Fatalf("projectless template after upgrade: %v; schema %s", err, schema)
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM creative_pipelines WHERE name='Global'`).Scan(&id); err != nil || id <= 100 {
		t.Fatalf("sequence: %d %v", id, err)
	}
	for _, want := range []struct {
		id               int64
		created, updated int64
	}{{7, 10, 20}, {9, 11, 21}} {
		p, err := s.GetCreativePipeline(want.id)
		if err != nil || p.Name != "Same" || p.CreatedAt != want.created || p.UpdatedAt != want.updated {
			t.Fatalf("template: %+v %v", p, err)
		}
		var got []CreativeStageDefinition
		if err := json.Unmarshal([]byte(stages), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p.Stages, got) {
			t.Fatalf("definitions changed: %+v", p)
		}
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("broken foreign key after upgrade")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var fk int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("FK enforcement: %d %v", fk, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, table := range tables {
		if got := dump(s.db, table); got != before[table] {
			t.Errorf("%s changed on reopen", table)
		}
	}
	if err := s.SetCreativeRunAllowedProfiles(17, []string{"cheap", "deep"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run, err = s.GetCreativeRun(17)
	if err != nil || !reflect.DeepEqual(run.AllowedProfiles, []string{"cheap", "deep"}) {
		t.Fatalf("reopened allowlist: %v %v", run.AllowedProfiles, err)
	}
	if err := s.SetCreativeRunAllowedProfiles(17, nil); err != nil {
		t.Fatal(err)
	}
	run, err = s.GetCreativeRun(17)
	if err != nil || run.AllowedProfiles == nil || len(run.AllowedProfiles) != 0 {
		t.Fatalf("clear: %v %v", run.AllowedProfiles, err)
	}
	if err := s.SetCreativeRunAllowedProfiles(9999, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing run: %v", err)
	}

}
