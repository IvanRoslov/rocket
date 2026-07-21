package store

import (
	"errors"
	"testing"
)

func TestAttachmentRoundTrip(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddAttachment(Attachment{MIME: "image/png", Size: 1234})
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	got, err := s.GetAttachment(id)
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if got.ID != id || got.MIME != "image/png" || got.Size != 1234 {
		t.Errorf("mismatch: %+v", got)
	}
	if got.CreatedAt == 0 {
		t.Errorf("CreatedAt not defaulted")
	}
}

func TestGetAttachment_NotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetAttachment(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
