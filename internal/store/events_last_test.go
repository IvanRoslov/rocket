package store

import "testing"

func TestLastSessionEventTS(t *testing.T) {
	s := openTestStore(t)
	if _, ok, err := s.LastSessionEventTS("sess1", "session.restored"); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v, want none", ok, err)
	}
	for _, e := range []Event{
		{TS: 100, Type: "session.restored", SessionID: "sess1"},
		{TS: 200, Type: "session.restored", SessionID: "sess1"},
		{TS: 300, Type: "session.activity_changed", SessionID: "sess1"},
		{TS: 400, Type: "session.restored", SessionID: "other"},
	} {
		if _, err := s.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	ts, ok, err := s.LastSessionEventTS("sess1", "session.restored")
	if err != nil || !ok || ts != 200 {
		t.Errorf("LastSessionEventTS = %d, %v, %v; want 200, true, nil", ts, ok, err)
	}
}
