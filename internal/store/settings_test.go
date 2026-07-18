package store

import (
	"errors"
	"testing"
)

func TestSettingRoundTrip(t *testing.T) {
	st := openTestStore(t)

	if err := st.SetSetting("github_token", "ghp_abc123"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	got, err := st.GetSetting("github_token")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != "ghp_abc123" {
		t.Errorf("GetSetting = %q, want ghp_abc123", got)
	}
}

func TestSettingOverwrite(t *testing.T) {
	st := openTestStore(t)

	if err := st.SetSetting("github_token", "first"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting("github_token", "second"); err != nil {
		t.Fatalf("SetSetting (overwrite): %v", err)
	}

	got, err := st.GetSetting("github_token")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != "second" {
		t.Errorf("GetSetting = %q, want second", got)
	}
}

func TestSettingDelete(t *testing.T) {
	st := openTestStore(t)

	if err := st.SetSetting("github_token", "value"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.DeleteSetting("github_token"); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}

	if _, err := st.GetSetting("github_token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSetting after delete: err = %v, want ErrNotFound", err)
	}
}

func TestSettingDeleteIdempotent(t *testing.T) {
	st := openTestStore(t)

	if err := st.DeleteSetting("does_not_exist"); err != nil {
		t.Errorf("DeleteSetting (missing key) = %v, want nil", err)
	}
}

func TestSettingMissing(t *testing.T) {
	st := openTestStore(t)

	if _, err := st.GetSetting("missing_key"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSetting (missing key): err = %v, want ErrNotFound", err)
	}
}
