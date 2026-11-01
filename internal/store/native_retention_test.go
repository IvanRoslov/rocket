package store

import (
	"errors"
	"testing"
)

// TestPurgeOldPreservesUnresolvedNativeState is review case 4: retention must
// not delete a native accepted/delivering row that is still waiting for its
// journal proof, while resolved native rows and legacy rows keep the old
// 30-day policy.
func TestPurgeOldPreservesUnresolvedNativeState(t *testing.T) {
	s := openTestStore(t)

	nativeAccepted, _ := s.AddMessage(Message{ToSession: "n", Body: "accepted", Status: "accepted", DeliveryTransport: "pi-uds-v1", CreatedAt: 100})
	nativeDelivering, _ := s.AddMessage(Message{ToSession: "n", Body: "delivering", Status: "delivering", DeliveryTransport: "pi-uds-v1", CreatedAt: 100})
	nativeDelivered, _ := s.AddMessage(Message{ToSession: "n", Body: "done", Status: "delivered", DeliveryTransport: "pi-uds-v1", CreatedAt: 100})
	nativeFailed, _ := s.AddMessage(Message{ToSession: "n", Body: "failed", Status: "failed", DeliveryTransport: "pi-uds-v1", CreatedAt: 100})
	legacyOld, _ := s.AddMessage(Message{ToSession: "l", Body: "legacy", CreatedAt: 100})

	if err := s.PurgeOld(500); err != nil {
		t.Fatalf("PurgeOld: %v", err)
	}
	for name, id := range map[string]int64{
		"native accepted":   nativeAccepted,
		"native delivering": nativeDelivering,
	} {
		if _, err := s.GetMessage(id); err != nil {
			t.Errorf("%s purged: %v", name, err)
		}
	}
	for name, id := range map[string]int64{
		"native delivered": nativeDelivered,
		"native failed":    nativeFailed,
		"legacy queued":    legacyOld,
	} {
		if _, err := s.GetMessage(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s not purged: err=%v", name, err)
		}
	}
}

// TestPurgeOldPreservesProofNeededByPendingNativeConfirmation covers the proof
// ownership half of review case 4: a consumed proof row must survive while a
// later native confirmation on the same recipient can still reference it.
func TestPurgeOldPreservesProofNeededByPendingNativeConfirmation(t *testing.T) {
	s := openTestStore(t)

	proofRow, _ := s.AddMessage(Message{ToSession: "n", Body: "same", CreatedAt: 100})
	if err := s.UpdateMessageStatus(proofRow, "accepted", 1, 0, ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ConfirmAccepted(proofRow, 10, 777, "same"); err != nil || !ok {
		t.Fatalf("ConfirmAccepted(proofRow) = %v, %v", ok, err)
	}
	pending, _ := s.AddMessage(Message{ToSession: "n", Body: "same", Status: "accepted", DeliveryTransport: "pi-uds-v1", CreatedAt: 100})

	unrelatedProof, _ := s.AddMessage(Message{ToSession: "other", Body: "same", CreatedAt: 100})
	if err := s.UpdateMessageStatus(unrelatedProof, "accepted", 1, 0, ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ConfirmAccepted(unrelatedProof, 10, 888, "same"); err != nil || !ok {
		t.Fatalf("ConfirmAccepted(unrelatedProof) = %v, %v", ok, err)
	}

	if err := s.PurgeOld(500); err != nil {
		t.Fatalf("PurgeOld: %v", err)
	}
	for name, id := range map[string]int64{"proof row": proofRow, "pending native": pending} {
		if _, err := s.GetMessage(id); err != nil {
			t.Errorf("%s purged: %v", name, err)
		}
	}
	if _, err := s.GetMessage(unrelatedProof); !errors.Is(err, ErrNotFound) {
		t.Errorf("unrelated proof row not purged: err=%v", err)
	}
}

// TestConfirmAcceptedConsumesJournalProofAtomically is review case 2's atomic
// ownership requirement: one journal record may settle at most one message,
// even when two confirmation paths race.
func TestConfirmAcceptedConsumesJournalProofAtomically(t *testing.T) {
	s := openTestStore(t)

	var ids []int64
	for i := 0; i < 2; i++ {
		id, err := s.AddMessage(Message{ToSession: "r", Body: "same"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateMessageStatus(id, "accepted", 1, 0, ""); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	type result struct {
		id  int64
		ok  bool
		err error
	}
	start := make(chan struct{})
	results := make(chan result, len(ids))
	for _, id := range ids {
		go func(id int64) {
			<-start
			ok, err := s.ConfirmAccepted(id, 1, 777, "same")
			results <- result{id: id, ok: ok, err: err}
		}(id)
	}
	close(start)

	winners := 0
	for range ids {
		r := <-results
		if r.err != nil {
			t.Errorf("ConfirmAccepted(%d): %v", r.id, r.err)
		}
		if r.ok {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("proof settled %d messages, want exactly 1", winners)
	}
}
