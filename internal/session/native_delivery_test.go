package session

import (
	"context"
	"errors"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

func TestNativeLaunchProviderAppliedBeforeRuntime(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.nativeLaunch = agent.NativeLaunch{
		Transport: "pi-uds-v1", InstanceID: "launch-1", Endpoint: "/tmp/native.sock",
	}
	snapshotAtCreate := make(chan store.Session, 1)
	rt.onCreate = func() {
		sess, err := st.GetSession("native-core")
		if err != nil {
			t.Errorf("GetSession at runtime.Create: %v", err)
			return
		}
		snapshotAtCreate <- sess
	}

	sess, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.DeliveryTransport != "pi-uds-v1" || sess.DeliveryInstance != "launch-1" || sess.DeliveryEndpoint != "/tmp/native.sock" {
		t.Fatalf("returned native snapshot = %+v", sess)
	}
	atCreate := <-snapshotAtCreate
	if atCreate.DeliveryTransport != "pi-uds-v1" || atCreate.DeliveryInstance != "launch-1" || atCreate.DeliveryEndpoint != "/tmp/native.sock" {
		t.Fatalf("snapshot before runtime.Create = %+v", atCreate)
	}
	if len(testFakeAgent.setupCalls) != 1 || len(testFakeAgent.launchCalls) != 1 || len(testFakeAgent.envCalls) != 1 {
		t.Fatalf("adapter calls setup/launch/env = %d/%d/%d", len(testFakeAgent.setupCalls), len(testFakeAgent.launchCalls), len(testFakeAgent.envCalls))
	}
	assertNativeSpec(t, testFakeAgent.setupCalls[0], testFakeAgent.nativeLaunch)
	assertNativeSpec(t, testFakeAgent.launchCalls[0], testFakeAgent.nativeLaunch)
	assertNativeSpec(t, testFakeAgent.envCalls[0], testFakeAgent.nativeLaunch)
}

func TestNativeLaunchProviderErrorDoesNotCreateRuntime(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.nativeErr = errors.New("native handshake failed")

	_, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"})
	if err == nil || err.Error() != "native handshake failed" {
		t.Fatalf("Spawn error = %v, want provider error", err)
	}
	if len(rt.created) != 0 {
		t.Fatalf("runtime created after provider failure: %+v", rt.created)
	}
	if len(testFakeAgent.setupCalls) != 0 || len(testFakeAgent.launchCalls) != 0 || len(testFakeAgent.envCalls) != 0 {
		t.Fatal("provider failure reached legacy adapter launch path")
	}
	sess, getErr := st.GetSession("native-core")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if sess.State != "errored" || sess.DeliveryTransport != "" {
		t.Fatalf("session after provider failure = %+v", sess)
	}
}

func TestNativeRestoreChangesInstance(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.nativeLaunch = agent.NativeLaunch{
		Transport: "pi-uds-v1", InstanceID: "launch-1", Endpoint: "/tmp/one.sock",
	}
	if _, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSessionState("native-core", "killed"); err != nil {
		t.Fatal(err)
	}
	testFakeAgent.nativeLaunch = agent.NativeLaunch{
		Transport: "pi-uds-v1", InstanceID: "launch-2", Endpoint: "/tmp/two.sock",
	}

	if err := m.Restore(context.Background(), "native-core", RestoreOpts{}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if testFakeAgent.nativeCalls != 2 {
		t.Fatalf("PrepareNativeLaunch calls = %d, want 2", testFakeAgent.nativeCalls)
	}
	assertNativeSpec(t, testFakeAgent.setupCalls[1], testFakeAgent.nativeLaunch)
	assertNativeSpec(t, testFakeAgent.launchCalls[1], testFakeAgent.nativeLaunch)
	assertNativeSpec(t, testFakeAgent.envCalls[1], testFakeAgent.nativeLaunch)
	sess, err := st.GetSession("native-core")
	if err != nil {
		t.Fatal(err)
	}
	if sess.DeliveryInstance != "launch-2" || sess.DeliveryEndpoint != "/tmp/two.sock" {
		t.Fatalf("restored native snapshot = %+v", sess)
	}
}

// TestNativeLaunchUnsupportedDeclarationFailsBeforeRuntime is review case 3:
// a provider that declares an unknown or incomplete native transport must stop
// the launch before SetupWorkspace/LaunchCommand/Env and before runtime.Create,
// never leaving the session to start on another transport.
func TestNativeLaunchUnsupportedDeclarationFailsBeforeRuntime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		native agent.NativeLaunch
	}{
		{"unsupported transport", agent.NativeLaunch{Transport: "other-v1", InstanceID: "i", Endpoint: "/tmp/x.sock"}},
		{"missing instance", agent.NativeLaunch{Transport: "pi-uds-v1", Endpoint: "/tmp/x.sock"}},
		{"missing endpoint", agent.NativeLaunch{Transport: "pi-uds-v1", InstanceID: "i"}},
		{"declaration without transport", agent.NativeLaunch{InstanceID: "i", Endpoint: "/tmp/x.sock"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, st, _, rt, _ := testManager(t)
			seedProjectRepo(t, st, "p", "r")
			testFakeAgent.nativeLaunch = tc.native

			_, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"})
			if err == nil {
				t.Fatal("Spawn accepted an unsupported/incomplete native declaration")
			}
			if len(rt.created) != 0 {
				t.Fatalf("runtime created after invalid native declaration: %+v", rt.created)
			}
			if len(testFakeAgent.setupCalls) != 0 || len(testFakeAgent.launchCalls) != 0 || len(testFakeAgent.envCalls) != 0 {
				t.Fatalf("invalid declaration reached adapter launch path: setup=%d launch=%d env=%d",
					len(testFakeAgent.setupCalls), len(testFakeAgent.launchCalls), len(testFakeAgent.envCalls))
			}
			sess, getErr := st.GetSession("native-core")
			if getErr != nil {
				t.Fatal(getErr)
			}
			if sess.State != "errored" || sess.DeliveryTransport != "" || sess.DeliveryInstance != "" || sess.DeliveryEndpoint != "" {
				t.Fatalf("session after invalid declaration = %+v", sess)
			}
		})
	}
}

// TestNativeOrchestratorInvalidDeclarationFailsBeforeRuntime covers the
// SpawnOrchestrator call site of the same validation.
func TestNativeOrchestratorInvalidDeclarationFailsBeforeRuntime(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	testFakeAgent.nativeLaunch = agent.NativeLaunch{Transport: "other-v1", InstanceID: "i", Endpoint: "/tmp/x.sock"}

	_, err = m.SpawnOrchestrator(context.Background(), store.Task{ID: 42, Title: "Add login page", ProjectID: "proj1"}, proj, "fake", LaunchProfile{})
	if err == nil {
		t.Fatal("SpawnOrchestrator accepted an unsupported native declaration")
	}
	if len(rt.created) != 0 {
		t.Fatalf("runtime created after invalid native declaration: %+v", rt.created)
	}
	if len(testFakeAgent.setupCalls) != 0 || len(testFakeAgent.launchCalls) != 0 || len(testFakeAgent.envCalls) != 0 {
		t.Fatalf("invalid declaration reached adapter launch path: setup=%d launch=%d env=%d",
			len(testFakeAgent.setupCalls), len(testFakeAgent.launchCalls), len(testFakeAgent.envCalls))
	}
}

// TestNativeRestoreInvalidDeclarationFailsBeforeRuntime covers the Restore
// call site: a now-invalid declaration must not start a second runtime or
// erase the session's existing native snapshot.
func TestNativeRestoreInvalidDeclarationFailsBeforeRuntime(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.nativeLaunch = agent.NativeLaunch{Transport: "pi-uds-v1", InstanceID: "launch-1", Endpoint: "/tmp/one.sock"}
	if _, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSessionState("native-core", "killed"); err != nil {
		t.Fatal(err)
	}
	testFakeAgent.nativeLaunch = agent.NativeLaunch{Transport: "pi-uds-v1", InstanceID: "launch-2"}

	if err := m.Restore(context.Background(), "native-core", RestoreOpts{}); err == nil {
		t.Fatal("Restore accepted an incomplete native declaration")
	}
	if len(rt.created) != 1 {
		t.Fatalf("runtime creates = %d, want only the initial spawn", len(rt.created))
	}
	if len(testFakeAgent.setupCalls) != 1 || len(testFakeAgent.launchCalls) != 1 || len(testFakeAgent.envCalls) != 1 {
		t.Fatalf("invalid restore reached adapter launch path: setup=%d launch=%d env=%d",
			len(testFakeAgent.setupCalls), len(testFakeAgent.launchCalls), len(testFakeAgent.envCalls))
	}
	sess, err := st.GetSession("native-core")
	if err != nil {
		t.Fatal(err)
	}
	if sess.DeliveryInstance != "launch-1" || sess.DeliveryEndpoint != "/tmp/one.sock" {
		t.Fatalf("restore clobbered the native snapshot before validating: %+v", sess)
	}
}

// TestNativeLaunchEmptyDeclarationKeepsLegacy pins the other side of review
// case 3: an empty declaration is still the supported legacy transport.
func TestNativeLaunchEmptyDeclarationKeepsLegacy(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.nativeLaunch = agent.NativeLaunch{}

	sess, err := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "native", Task: "core", AgentName: "fake"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(rt.created) != 1 {
		t.Fatalf("runtime creates = %d, want 1", len(rt.created))
	}
	if sess.DeliveryTransport != "" || sess.DeliveryInstance != "" || sess.DeliveryEndpoint != "" {
		t.Fatalf("empty declaration produced a native snapshot: %+v", sess)
	}
}

func assertNativeSpec(t *testing.T, spec agent.LaunchSpec, want agent.NativeLaunch) {
	t.Helper()
	if spec.NativeDelivery != want {
		t.Fatalf("NativeDelivery = %+v, want %+v", spec.NativeDelivery, want)
	}
}
