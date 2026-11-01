package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestLegacySessionMigrationDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	openLegacyDeliveryDB(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open migrated store: %v", err)
	}
	defer s.Close()

	sess, err := s.GetSession("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if sess.DeliveryTransport != "" || sess.DeliveryInstance != "" || sess.DeliveryEndpoint != "" {
		t.Fatalf("legacy session native fields = %q/%q/%q, want empty", sess.DeliveryTransport, sess.DeliveryInstance, sess.DeliveryEndpoint)
	}
	msg, err := s.GetMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.DeliveryTransport != "" {
		t.Fatalf("legacy message transport = %q, want empty", msg.DeliveryTransport)
	}
}

func TestSessionDeliverySnapshotRoundTrip(t *testing.T) {
	s := openTestStore(t)
	if err := s.AddSession(Session{
		ID: "s", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "pi", Branch: "main",
		WorktreePath: "/tmp/s", TmuxName: "s", State: "running",
		DeliveryTransport: "pi-uds-v1", DeliveryInstance: "launch-1", DeliveryEndpoint: "/tmp/pi.sock",
	}); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession("s")
	if err != nil {
		t.Fatal(err)
	}
	assertNativeSession(t, sess, "pi-uds-v1", "launch-1", "/tmp/pi.sock")

	if err := s.UpdateSessionDelivery("s", "pi-uds-v1", "launch-2", "/tmp/new.sock"); err != nil {
		t.Fatal(err)
	}
	sess, _ = s.GetSession("s")
	assertNativeSession(t, sess, "pi-uds-v1", "launch-2", "/tmp/new.sock")
	if sess.Agent != "pi" || sess.State != "running" {
		t.Fatalf("delivery update clobbered session: %+v", sess)
	}
}

func TestClaimNativeMessagePersistsBeforeSend(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AddMessage(Message{ToSession: "s", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.ClaimNativeMessage(id, "pi-uds-v1", 1234)
	if err != nil || !ok {
		t.Fatalf("ClaimNativeMessage = %v, %v", ok, err)
	}
	msg, err := s.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Status != "delivering" || msg.DeliveryTransport != "pi-uds-v1" || msg.InjectAt != 1234 {
		t.Fatalf("claimed message = %+v", msg)
	}
	if ok, _ := s.ClaimNativeMessage(id, "pi-uds-v1", 9999); ok {
		t.Fatal("second native claim won while delivering")
	}
	if err := s.SetMessageNativeDelivery(id, "pi-uds-v1", 5678); err != nil {
		t.Fatal(err)
	}
	msg, _ = s.GetMessage(id)
	if msg.InjectAt != 5678 || msg.DeliveryTransport != "pi-uds-v1" {
		t.Fatalf("updated native snapshot = %+v", msg)
	}
}

func TestResetDeliveringNativeBecomesAccepted(t *testing.T) {
	s := openTestStore(t)
	nativeID, _ := s.AddMessage(Message{ToSession: "native", Body: "native"})
	legacyID, _ := s.AddMessage(Message{ToSession: "legacy", Body: "legacy"})
	if ok, err := s.ClaimNativeMessage(nativeID, "pi-uds-v1", 1234); err != nil || !ok {
		t.Fatalf("claim native = %v, %v", ok, err)
	}
	if err := s.UpdateMessageStatus(legacyID, "delivering", 1, 0, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ResetDelivering(); err != nil {
		t.Fatal(err)
	}
	nativeMsg, _ := s.GetMessage(nativeID)
	if nativeMsg.Status != "accepted" || nativeMsg.InjectAt != 1234 || nativeMsg.DeliveryTransport != "pi-uds-v1" {
		t.Fatalf("native recovery = %+v, want accepted with snapshot", nativeMsg)
	}
	legacyMsg, _ := s.GetMessage(legacyID)
	if legacyMsg.Status != "queued" || legacyMsg.DeliveryTransport != "" {
		t.Fatalf("legacy recovery = %+v, want queued", legacyMsg)
	}
}

func assertNativeSession(t *testing.T, sess Session, transport, instance, endpoint string) {
	t.Helper()
	if sess.DeliveryTransport != transport || sess.DeliveryInstance != instance || sess.DeliveryEndpoint != endpoint {
		t.Fatalf("native session = %q/%q/%q, want %q/%q/%q", sess.DeliveryTransport, sess.DeliveryInstance, sess.DeliveryEndpoint, transport, instance, endpoint)
	}
}

func openLegacyDeliveryDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+escapeDSNPath(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names[:25] {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO sessions (
		id, kind, project_id, repo_id, feature_slug, agent, branch, worktree_path, tmux_name, state, created_at, updated_at
	) VALUES ('legacy', 'worker', 'p', 'r', 'f', 'pi', 'main', '/tmp/legacy', 'legacy', 'running', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (to_session, body, created_at) VALUES ('legacy', 'old', 1)`); err != nil {
		t.Fatal(err)
	}
}
