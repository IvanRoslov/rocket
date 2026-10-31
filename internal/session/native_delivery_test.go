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

func assertNativeSpec(t *testing.T, spec agent.LaunchSpec, want agent.NativeLaunch) {
	t.Helper()
	if spec.NativeDelivery != want {
		t.Fatalf("NativeDelivery = %+v, want %+v", spec.NativeDelivery, want)
	}
}
