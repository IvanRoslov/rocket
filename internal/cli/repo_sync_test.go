package cli

import (
	"reflect"
	"testing"
)

// TestSelectMirrorsAll: with no ids given, every mirror is selected and
// nothing is reported as unknown.
func TestSelectMirrorsAll(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}}

	got, unknown := selectMirrors(mirrors, nil)

	if !reflect.DeepEqual(got, mirrors) {
		t.Fatalf("selected = %v, want %v", got, mirrors)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsByID keeps the requested mirrors in the order the user
// named them, so the report reads back in the order it was asked for.
func TestSelectMirrorsByID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}, {ID: "web"}}

	got, unknown := selectMirrors(mirrors, []string{"web", "rocket"})

	want := []repoRow{{ID: "web"}, {ID: "rocket"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsUnknownID: an id that is not a mirror is reported back
// rather than silently skipped — a typo that syncs nothing must say so.
func TestSelectMirrorsUnknownID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}}

	got, unknown := selectMirrors(mirrors, []string{"rocket", "nope"})

	if len(got) != 1 || got[0].ID != "rocket" {
		t.Fatalf("selected = %v, want [rocket]", got)
	}
	if !reflect.DeepEqual(unknown, []string{"nope"}) {
		t.Fatalf("unknown = %v, want [nope]", unknown)
	}
}
