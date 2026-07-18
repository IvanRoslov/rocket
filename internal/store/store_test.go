package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rocket.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpen_ReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rocket.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.AddRepo(Repo{ID: "api", Path: "/tmp/api"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	r, err := s2.GetRepo("api")
	if err != nil {
		t.Fatalf("GetRepo after reopen: %v", err)
	}
	if r.Path != "/tmp/api" {
		t.Fatalf("Path = %q, want /tmp/api", r.Path)
	}
}

func TestOpen_PragmasOnFreshConnections(t *testing.T) {
	s := openTestStore(t)

	ctx := context.Background()

	// Get two concurrent connections from the pool
	conn1, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn 1: %v", err)
	}
	defer conn1.Close()

	conn2, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn 2: %v", err)
	}
	defer conn2.Close()

	// Verify foreign_keys pragma is set on both connections
	var fkEnabled1 int
	if err := conn1.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkEnabled1); err != nil {
		t.Fatalf("Query PRAGMA foreign_keys on conn1: %v", err)
	}
	if fkEnabled1 != 1 {
		t.Fatalf("foreign_keys on conn1 = %d, want 1", fkEnabled1)
	}

	var fkEnabled2 int
	if err := conn2.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkEnabled2); err != nil {
		t.Fatalf("Query PRAGMA foreign_keys on conn2: %v", err)
	}
	if fkEnabled2 != 1 {
		t.Fatalf("foreign_keys on conn2 = %d, want 1", fkEnabled2)
	}
}

func TestRepoCRUD_JSONRoundTrip(t *testing.T) {
	s := openTestStore(t)

	r := Repo{
		ID:            "api",
		Path:          "/repos/api",
		DefaultBranch: "main",
		AutoCleanup:   true,
		Env:           map[string]string{"FOO": "bar"},
		Symlinks:      []string{".env", "config/local.yaml"},
		PostCreate:    []string{"npm install"},
	}
	if err := s.AddRepo(r); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	got, err := s.GetRepo("api")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Path != r.Path || got.DefaultBranch != r.DefaultBranch || got.AutoCleanup != r.AutoCleanup {
		t.Fatalf("GetRepo scalar mismatch: got %+v", got)
	}
	if !reflect.DeepEqual(got.Env, r.Env) {
		t.Fatalf("Env round-trip: got %v, want %v", got.Env, r.Env)
	}
	if !reflect.DeepEqual(got.Symlinks, r.Symlinks) {
		t.Fatalf("Symlinks round-trip: got %v, want %v", got.Symlinks, r.Symlinks)
	}
	if !reflect.DeepEqual(got.PostCreate, r.PostCreate) {
		t.Fatalf("PostCreate round-trip: got %v, want %v", got.PostCreate, r.PostCreate)
	}
	if got.CreatedAt == 0 {
		t.Fatalf("CreatedAt not set")
	}

	if err := s.AddRepo(Repo{ID: "api", Path: "/other"}); !errors.Is(err, ErrExists) {
		t.Fatalf("AddRepo duplicate: got %v, want ErrExists", err)
	}

	r.Path = "/repos/api-v2"
	r.Env = map[string]string{"FOO": "baz"}
	if err := s.UpdateRepo(r); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	got, err = s.GetRepo("api")
	if err != nil {
		t.Fatalf("GetRepo after update: %v", err)
	}
	if got.Path != "/repos/api-v2" || got.Env["FOO"] != "baz" {
		t.Fatalf("UpdateRepo did not persist: %+v", got)
	}

	if err := s.AddRepo(Repo{ID: "web", Path: "/repos/web"}); err != nil {
		t.Fatalf("AddRepo web: %v", err)
	}
	list, err := s.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListRepos len = %d, want 2", len(list))
	}

	if _, err := s.GetRepo("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRepo missing: got %v, want ErrNotFound", err)
	}

	if err := s.DeleteRepo("web"); err != nil {
		t.Fatalf("DeleteRepo web: %v", err)
	}
	if _, err := s.GetRepo("web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRepo after delete: got %v, want ErrNotFound", err)
	}
}

func TestDeleteRepo_InUseByProject(t *testing.T) {
	s := openTestStore(t)

	mustAddRepo(t, s, "api")
	mustAddRepo(t, s, "web")
	mustAddRepo(t, s, "extra")

	if err := s.AddProject(Project{ID: "billing", Name: "Billing", MainRepo: "api", LinkedRepos: []string{"web"}}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	if err := s.DeleteRepo("api"); !errors.Is(err, ErrRepoInUse) {
		t.Fatalf("DeleteRepo main repo: got %v, want ErrRepoInUse", err)
	}
	if err := s.DeleteRepo("web"); !errors.Is(err, ErrRepoInUse) {
		t.Fatalf("DeleteRepo linked repo: got %v, want ErrRepoInUse", err)
	}
	if err := s.DeleteRepo("extra"); err != nil {
		t.Fatalf("DeleteRepo unused repo: %v", err)
	}
}

func TestProjectCRUD_LinkUnlink(t *testing.T) {
	s := openTestStore(t)

	mustAddRepo(t, s, "api")
	mustAddRepo(t, s, "web")
	mustAddRepo(t, s, "worker")

	p := Project{ID: "billing", Name: "Billing", MainRepo: "api"}
	if err := s.AddProject(p); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	got, err := s.GetProject("billing")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Name != "Billing" || got.MainRepo != "api" {
		t.Fatalf("GetProject mismatch: %+v", got)
	}
	if got.CreatedAt == 0 {
		t.Fatalf("CreatedAt not set")
	}
	if !reflect.DeepEqual(got.Repos(), []string{"api"}) {
		t.Fatalf("Repos() = %v, want [api]", got.Repos())
	}

	got.LinkedRepos = []string{"web", "worker"}
	if err := s.UpdateProject(got); err != nil {
		t.Fatalf("UpdateProject link: %v", err)
	}
	got, err = s.GetProject("billing")
	if err != nil {
		t.Fatalf("GetProject after link: %v", err)
	}
	if !reflect.DeepEqual(got.Repos(), []string{"api", "web", "worker"}) {
		t.Fatalf("Repos() after link = %v", got.Repos())
	}

	got.LinkedRepos = []string{"web"}
	if err := s.UpdateProject(got); err != nil {
		t.Fatalf("UpdateProject unlink: %v", err)
	}
	got, err = s.GetProject("billing")
	if err != nil {
		t.Fatalf("GetProject after unlink: %v", err)
	}
	if !reflect.DeepEqual(got.Repos(), []string{"api", "web"}) {
		t.Fatalf("Repos() after unlink = %v", got.Repos())
	}

	if err := s.AddProject(Project{ID: "billing", Name: "dup", MainRepo: "api"}); !errors.Is(err, ErrExists) {
		t.Fatalf("AddProject duplicate: got %v, want ErrExists", err)
	}

	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListProjects len = %d, want 1", len(list))
	}

	if err := s.DeleteProject("billing"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := s.GetProject("billing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject after delete: got %v, want ErrNotFound", err)
	}
}

func mustAddRepo(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.AddRepo(Repo{ID: id, Path: "/repos/" + id}); err != nil {
		t.Fatalf("AddRepo %s: %v", id, err)
	}
}

func mustSetup(t *testing.T, s *Store) {
	t.Helper()
	mustAddRepo(t, s, "api")
	if err := s.AddProject(Project{ID: "billing", Name: "Billing", MainRepo: "api"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
}

func TestSessionAddListFilters(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	sessions := []Session{
		{ID: "s1", Kind: "orchestrator", ProjectID: "billing", RepoID: "api", FeatureSlug: "f1", Agent: "claude-code", Branch: "b1", WorktreePath: "/wt/s1", TmuxName: "t1", State: "running"},
		{ID: "s2", Kind: "worker", ProjectID: "billing", RepoID: "api", FeatureSlug: "f1", ParentID: "s1", Agent: "claude-code", Branch: "b2", WorktreePath: "/wt/s2", TmuxName: "t2", State: "spawning"},
		{ID: "s3", Kind: "worker", ProjectID: "billing", RepoID: "api", FeatureSlug: "f2", Agent: "claude-code", Branch: "b3", WorktreePath: "/wt/s3", TmuxName: "t3", State: "done"},
	}
	for _, sess := range sessions {
		if err := s.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ParentID != "" {
		t.Fatalf("ParentID for s1 = %q, want empty", got.ParentID)
	}
	if got.Activity != "" || got.ActivityTS != 0 || got.Prompt != "" {
		t.Fatalf("nullable fields not zero: %+v", got)
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Fatalf("timestamps not set: %+v", got)
	}

	got2, err := s.GetSession("s2")
	if err != nil {
		t.Fatalf("GetSession s2: %v", err)
	}
	if got2.ParentID != "s1" {
		t.Fatalf("ParentID for s2 = %q, want s1", got2.ParentID)
	}

	live, err := s.ListSessions(SessionFilter{})
	if err != nil {
		t.Fatalf("ListSessions default: %v", err)
	}
	if ids := sessionIDs(live); !reflect.DeepEqual(ids, []string{"s1", "s2"}) {
		t.Fatalf("default filter ids = %v, want [s1 s2]", ids)
	}

	all, err := s.ListSessions(SessionFilter{All: true})
	if err != nil {
		t.Fatalf("ListSessions all: %v", err)
	}
	if ids := sessionIDs(all); !reflect.DeepEqual(ids, []string{"s1", "s2", "s3"}) {
		t.Fatalf("all filter ids = %v, want [s1 s2 s3]", ids)
	}

	byFeature, err := s.ListSessions(SessionFilter{All: true, Feature: "f2"})
	if err != nil {
		t.Fatalf("ListSessions by feature: %v", err)
	}
	if ids := sessionIDs(byFeature); !reflect.DeepEqual(ids, []string{"s3"}) {
		t.Fatalf("feature filter ids = %v, want [s3]", ids)
	}

	byKind, err := s.ListSessions(SessionFilter{Kind: "worker"})
	if err != nil {
		t.Fatalf("ListSessions by kind: %v", err)
	}
	if ids := sessionIDs(byKind); !reflect.DeepEqual(ids, []string{"s2"}) {
		t.Fatalf("kind filter (live only) ids = %v, want [s2]", ids)
	}

	if err := s.UpdateSessionState("s1", "done"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}
	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after state update: %v", err)
	}
	if got.State != "done" {
		t.Fatalf("State = %q, want done", got.State)
	}

	got.Activity = "idle"
	got.Prompt = "do the thing"
	if err := s.UpdateSession(got); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after full update: %v", err)
	}
	if got.Activity != "idle" || got.Prompt != "do the thing" {
		t.Fatalf("UpdateSession did not persist: %+v", got)
	}

	if _, err := s.GetSession("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession missing: got %v, want ErrNotFound", err)
	}
	if err := s.UpdateSessionState("missing", "done"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSessionState missing: got %v, want ErrNotFound", err)
	}
}

func sessionIDs(sessions []Session) []string {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.ID
	}
	sort.Strings(ids)
	return ids
}

func TestAppendListEvents(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	if err := s.AddSession(Session{ID: "s1", Kind: "orchestrator", ProjectID: "billing", RepoID: "api", FeatureSlug: "f1", Agent: "claude-code", Branch: "b1", WorktreePath: "/wt/s1", TmuxName: "t1", State: "running"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := s.AddSession(Session{ID: "s2", Kind: "worker", ProjectID: "billing", RepoID: "api", FeatureSlug: "f1", Agent: "claude-code", Branch: "b2", WorktreePath: "/wt/s2", TmuxName: "t2", State: "running"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	var lastID int64
	for i := 0; i < 3; i++ {
		id, err := s.AppendEvent(Event{Type: "session.started", SessionID: "s1", Data: map[string]any{"n": float64(i)}})
		if err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		lastID = id
	}
	if _, err := s.AppendEvent(Event{Type: "session.started", SessionID: "s2", Data: map[string]any{"x": "y"}}); err != nil {
		t.Fatalf("AppendEvent s2: %v", err)
	}
	if lastID == 0 {
		t.Fatalf("lastID not assigned")
	}

	all, err := s.ListEvents(0, 0, "")
	if err != nil {
		t.Fatalf("ListEvents all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("ListEvents all len = %d, want 4", len(all))
	}
	if !reflect.DeepEqual(all[0].Data, map[string]any{"n": float64(0)}) {
		t.Fatalf("Data round-trip = %v", all[0].Data)
	}

	since, err := s.ListEvents(all[0].ID, 0, "")
	if err != nil {
		t.Fatalf("ListEvents since: %v", err)
	}
	if len(since) != 3 {
		t.Fatalf("ListEvents since len = %d, want 3", len(since))
	}

	limited, err := s.ListEvents(0, 2, "")
	if err != nil {
		t.Fatalf("ListEvents limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListEvents limited len = %d, want 2", len(limited))
	}

	bySession, err := s.ListEvents(0, 0, "s2")
	if err != nil {
		t.Fatalf("ListEvents by session: %v", err)
	}
	if len(bySession) != 1 || bySession[0].SessionID != "s2" {
		t.Fatalf("ListEvents by session = %+v", bySession)
	}
}
