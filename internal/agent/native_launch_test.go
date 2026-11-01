package agent

import "testing"

func TestNativeLaunchValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		native  NativeLaunch
		wantErr bool
	}{
		{"empty is legacy", NativeLaunch{}, false},
		{"supported declaration", NativeLaunch{Transport: NativeTransportPiUDS, InstanceID: "launch-1", Endpoint: "/tmp/pi.sock"}, false},
		{"unsupported transport", NativeLaunch{Transport: "other-v1", InstanceID: "launch-1", Endpoint: "/tmp/x.sock"}, true},
		{"missing instance", NativeLaunch{Transport: NativeTransportPiUDS, Endpoint: "/tmp/pi.sock"}, true},
		{"missing endpoint", NativeLaunch{Transport: NativeTransportPiUDS, InstanceID: "launch-1"}, true},
		{"declaration without transport", NativeLaunch{InstanceID: "launch-1", Endpoint: "/tmp/pi.sock"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.native.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate(%+v) = nil, want error", tc.native)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate(%+v) = %v, want nil", tc.native, err)
			}
		})
	}
}
