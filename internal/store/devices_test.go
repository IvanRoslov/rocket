package store

import (
	"errors"
	"testing"
)

func TestPairingRedeemCreatesDevice(t *testing.T) {
	s := openTestStore(t)
	if err := s.CreatePairingCode("h1", 100, 700); err != nil {
		t.Fatal(err)
	}
	d, err := s.RedeemPairingCode("h1", 200, "iphone", "mobile", "tok1")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 || d.Name != "iphone" || d.Kind != "mobile" || d.CreatedAt != 200 {
		t.Fatalf("device = %+v", d)
	}
	got, err := s.DeviceByTokenHash("tok1")
	if err != nil || got.ID != d.ID {
		t.Fatalf("DeviceByTokenHash = %+v, %v", got, err)
	}
}

func TestPairingCodeSingleUseAndExpiry(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("h1", 100, 700)
	if _, err := s.RedeemPairingCode("h1", 200, "a", "web", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairingCode("h1", 201, "b", "web", "t2"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reuse err = %v, want ErrInvalidCode", err)
	}
	_ = s.CreatePairingCode("h2", 100, 700)
	if _, err := s.RedeemPairingCode("h2", 700, "c", "web", "t3"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expired err = %v, want ErrInvalidCode", err)
	}
	if _, err := s.RedeemPairingCode("nope", 200, "d", "web", "t4"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("unknown err = %v, want ErrInvalidCode", err)
	}
}

func TestCreatePairingCodePrunesDeadCodes(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("old", 0, 50)
	_ = s.CreatePairingCode("new", 100, 700)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pairing_codes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pairing_codes rows = %d, want 1 (expired pruned)", n)
	}
}

func TestRevokeAndListDevices(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("a", 0, 1000)
	_ = s.CreatePairingCode("b", 0, 1000)
	d1, _ := s.RedeemPairingCode("a", 1, "one", "web", "t1")
	d2, _ := s.RedeemPairingCode("b", 2, "two", "mobile", "t2")
	if err := s.RevokeDevice(d1.ID, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDevice(d1.ID, 6); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double revoke err = %v, want ErrNotFound", err)
	}
	if _, err := s.DeviceByTokenHash("t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked lookup err = %v, want ErrNotFound", err)
	}
	list, err := s.ListDevices()
	if err != nil || len(list) != 1 || list[0].ID != d2.ID {
		t.Fatalf("ListDevices = %+v, %v", list, err)
	}
	if err := s.TouchDevice(d2.ID, 42); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DeviceByTokenHash("t2")
	if got.LastSeenAt == nil || *got.LastSeenAt != 42 {
		t.Fatalf("LastSeenAt = %v, want 42", got.LastSeenAt)
	}
}
