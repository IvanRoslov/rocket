package store

import (
	"errors"
	"testing"
)

func TestAddTask_DefaultsAndInvalidStatus(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "Do the thing", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "backlog" {
		t.Errorf("Status = %q, want backlog", got.Status)
	}
	if got.CreatedBy != "user" {
		t.Errorf("CreatedBy = %q, want user", got.CreatedBy)
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Errorf("timestamps not set: %+v", got)
	}
	if got.ParentID != 0 {
		t.Errorf("ParentID = %d, want 0", got.ParentID)
	}
	if got.RepoID != "" || got.FeatureSlug != "" || got.SessionID != "" {
		t.Errorf("nullable string fields not zero: %+v", got)
	}
	if got.CompletedAt != 0 {
		t.Errorf("CompletedAt = %d, want 0", got.CompletedAt)
	}

	if _, err := s.AddTask(Task{Title: "bad", ProjectID: "billing", Status: "bogus"}); err == nil {
		t.Fatal("AddTask with invalid status: want error, got nil")
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.GetTask(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddTask_ParentAndFieldsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	mustAddTaskSession(t, s, "sess-1")

	parentID, err := s.AddTask(Task{Title: "Parent", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask parent: %v", err)
	}

	childID, err := s.AddTask(Task{
		ParentID:    parentID,
		Title:       "Child",
		Description: "desc",
		ProjectID:   "billing",
		RepoID:      "api",
		Status:      "in_progress",
		FeatureSlug: "feat-x",
		SessionID:   "sess-1",
		CreatedBy:   "orchestrator",
	})
	if err != nil {
		t.Fatalf("AddTask child: %v", err)
	}

	got, err := s.GetTask(childID)
	if err != nil {
		t.Fatalf("GetTask child: %v", err)
	}
	if got.ParentID != parentID {
		t.Errorf("ParentID = %d, want %d", got.ParentID, parentID)
	}
	if got.Description != "desc" || got.RepoID != "api" || got.Status != "in_progress" ||
		got.FeatureSlug != "feat-x" || got.SessionID != "sess-1" || got.CreatedBy != "orchestrator" {
		t.Errorf("field mismatch: %+v", got)
	}
}

func TestGetTaskBySessionID(t *testing.T) {
	s := openTestStore(t)
	mustAddTaskSession(t, s, "sess-1")

	id, err := s.AddTask(Task{
		Title:     "Root",
		ProjectID: "billing",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	got, err := s.GetTaskBySessionID("sess-1")
	if err != nil {
		t.Fatalf("GetTaskBySessionID: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID = %d, want %d", got.ID, id)
	}
}

func TestGetTaskBySessionID_NotFound(t *testing.T) {
	s := openTestStore(t)

	if _, err := s.GetTaskBySessionID("no-such-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListTasks_Filters(t *testing.T) {
	s := openTestStore(t)

	root1, err := s.AddTask(Task{Title: "Root1", ProjectID: "billing", Status: "backlog"})
	if err != nil {
		t.Fatalf("AddTask root1: %v", err)
	}
	root2, err := s.AddTask(Task{Title: "Root2", ProjectID: "other", Status: "in_progress"})
	if err != nil {
		t.Fatalf("AddTask root2: %v", err)
	}
	child1, err := s.AddTask(Task{ParentID: root1, Title: "Child1", ProjectID: "billing", Status: "backlog"})
	if err != nil {
		t.Fatalf("AddTask child1: %v", err)
	}
	child2, err := s.AddTask(Task{ParentID: root1, Title: "Child2", ProjectID: "billing", Status: "done"})
	if err != nil {
		t.Fatalf("AddTask child2: %v", err)
	}

	// No filter: all tasks, ordered by id.
	all, err := s.ListTasks(TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("all len = %d, want 4", len(all))
	}
	wantOrder := []int64{root1, root2, child1, child2}
	for i, want := range wantOrder {
		if all[i].ID != want {
			t.Errorf("all[%d].ID = %d, want %d", i, all[i].ID, want)
		}
	}

	// Filter by project.
	byProject, err := s.ListTasks(TaskFilter{Project: "billing"})
	if err != nil {
		t.Fatalf("ListTasks by project: %v", err)
	}
	if ids := taskIDs(byProject); !idsEqual(ids, []int64{root1, child1, child2}) {
		t.Errorf("byProject ids = %v, want [%d %d %d]", ids, root1, child1, child2)
	}

	// Filter by status.
	byStatus, err := s.ListTasks(TaskFilter{Status: "backlog"})
	if err != nil {
		t.Fatalf("ListTasks by status: %v", err)
	}
	if ids := taskIDs(byStatus); !idsEqual(ids, []int64{root1, child1}) {
		t.Errorf("byStatus ids = %v, want [%d %d]", ids, root1, child1)
	}

	// Root-only filter (ParentSet + Parent==0).
	rootOnly, err := s.ListTasks(TaskFilter{ParentSet: true, Parent: 0})
	if err != nil {
		t.Fatalf("ListTasks root only: %v", err)
	}
	if ids := taskIDs(rootOnly); !idsEqual(ids, []int64{root1, root2}) {
		t.Errorf("rootOnly ids = %v, want [%d %d]", ids, root1, root2)
	}

	// Filter by specific parent.
	byParent, err := s.ListTasks(TaskFilter{ParentSet: true, Parent: root1})
	if err != nil {
		t.Fatalf("ListTasks by parent: %v", err)
	}
	if ids := taskIDs(byParent); !idsEqual(ids, []int64{child1, child2}) {
		t.Errorf("byParent ids = %v, want [%d %d]", ids, child1, child2)
	}

	// Combined filters.
	combined, err := s.ListTasks(TaskFilter{Project: "billing", Status: "done", ParentSet: true, Parent: root1})
	if err != nil {
		t.Fatalf("ListTasks combined: %v", err)
	}
	if ids := taskIDs(combined); !idsEqual(ids, []int64{child2}) {
		t.Errorf("combined ids = %v, want [%d]", ids, child2)
	}
}

func TestUpdateTaskStatus_CompletedAtSetAndCleared(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "T", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := s.UpdateTaskStatus(id, "done"); err != nil {
		t.Fatalf("UpdateTaskStatus done: %v", err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("Status = %q, want done", got.Status)
	}
	if got.CompletedAt == 0 {
		t.Errorf("CompletedAt not set on done")
	}

	if err := s.UpdateTaskStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	got, err = s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CompletedAt != 0 {
		t.Errorf("CompletedAt = %d, want cleared (0)", got.CompletedAt)
	}

	if err := s.UpdateTaskStatus(id, "cancelled"); err != nil {
		t.Fatalf("UpdateTaskStatus cancelled: %v", err)
	}
	got, err = s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.CompletedAt == 0 {
		t.Errorf("CompletedAt not set on cancelled")
	}

	if err := s.UpdateTaskStatus(id, "bogus"); err == nil {
		t.Fatal("UpdateTaskStatus invalid status: want error, got nil")
	}

	if err := s.UpdateTaskStatus(999, "done"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTaskStatus missing: got %v, want ErrNotFound", err)
	}
}

func TestUpdateTask_FieldsOnly(t *testing.T) {
	s := openTestStore(t)
	mustAddTaskSession(t, s, "sess-2")

	id, err := s.AddTask(Task{Title: "Orig", ProjectID: "billing", Status: "in_progress", CreatedBy: "orchestrator"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := s.UpdateTask(Task{
		ID:          id,
		Title:       "Updated",
		Description: "new desc",
		FeatureSlug: "feat-y",
		SessionID:   "sess-2",
		RepoID:      "web",
		// Attempt to change fields that must NOT be updated:
		Status:    "done",
		ParentID:  42,
		ProjectID: "other",
		CreatedBy: "user",
	}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Title != "Updated" || got.Description != "new desc" || got.FeatureSlug != "feat-y" ||
		got.SessionID != "sess-2" || got.RepoID != "web" {
		t.Fatalf("updated fields mismatch: %+v", got)
	}
	// Untouched fields.
	if got.Status != "in_progress" {
		t.Errorf("Status = %q, want unchanged in_progress", got.Status)
	}
	if got.ParentID != 0 {
		t.Errorf("ParentID = %d, want unchanged 0", got.ParentID)
	}
	if got.ProjectID != "billing" {
		t.Errorf("ProjectID = %q, want unchanged billing", got.ProjectID)
	}
	if got.CreatedBy != "orchestrator" {
		t.Errorf("CreatedBy = %q, want unchanged orchestrator", got.CreatedBy)
	}

	if err := s.UpdateTask(Task{ID: 999, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTask missing: got %v, want ErrNotFound", err)
	}
}

func TestTaskDocVersioning(t *testing.T) {
	s := openTestStore(t)

	taskID, err := s.AddTask(Task{Title: "T", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	v1, err := s.PutTaskDoc(TaskDoc{TaskID: taskID, Kind: "spec", Title: "Spec A", Body: "body v1", Author: "sess-1"})
	if err != nil {
		t.Fatalf("PutTaskDoc v1: %v", err)
	}
	if v1.Version != 1 {
		t.Errorf("v1.Version = %d, want 1", v1.Version)
	}
	if v1.ID == 0 {
		t.Error("v1.ID not assigned")
	}
	if v1.CreatedAt == 0 {
		t.Error("v1.CreatedAt not set")
	}

	// Same task+kind+title -> version 2.
	v2, err := s.PutTaskDoc(TaskDoc{TaskID: taskID, Kind: "spec", Title: "Spec A", Body: "body v2"})
	if err != nil {
		t.Fatalf("PutTaskDoc v2: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("v2.Version = %d, want 2", v2.Version)
	}

	// Different title -> version 1.
	other, err := s.PutTaskDoc(TaskDoc{TaskID: taskID, Kind: "spec", Title: "Spec B", Body: "other body"})
	if err != nil {
		t.Fatalf("PutTaskDoc other: %v", err)
	}
	if other.Version != 1 {
		t.Errorf("other.Version = %d, want 1", other.Version)
	}

	// Different kind, same title -> version 1.
	otherKind, err := s.PutTaskDoc(TaskDoc{TaskID: taskID, Kind: "plan", Title: "Spec A", Body: "plan body"})
	if err != nil {
		t.Fatalf("PutTaskDoc otherKind: %v", err)
	}
	if otherKind.Version != 1 {
		t.Errorf("otherKind.Version = %d, want 1", otherKind.Version)
	}

	// history=false: only latest version per (kind,title).
	latest, err := s.ListTaskDocs(taskID, false)
	if err != nil {
		t.Fatalf("ListTaskDocs latest: %v", err)
	}
	if len(latest) != 3 {
		t.Fatalf("latest len = %d, want 3", len(latest))
	}
	byKey := map[string]TaskDoc{}
	for _, d := range latest {
		byKey[d.Kind+"|"+d.Title] = d
	}
	if d := byKey["spec|Spec A"]; d.Version != 2 || d.Body != "body v2" {
		t.Errorf("latest spec|Spec A = %+v, want version 2, body v2", d)
	}
	if d := byKey["spec|Spec B"]; d.Version != 1 {
		t.Errorf("latest spec|Spec B = %+v, want version 1", d)
	}
	if d := byKey["plan|Spec A"]; d.Version != 1 {
		t.Errorf("latest plan|Spec A = %+v, want version 1", d)
	}

	// history=true: all versions, ascending by id.
	history, err := s.ListTaskDocs(taskID, true)
	if err != nil {
		t.Fatalf("ListTaskDocs history: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4", len(history))
	}
	wantIDOrder := []int64{v1.ID, v2.ID, other.ID, otherKind.ID}
	for i, want := range wantIDOrder {
		if history[i].ID != want {
			t.Errorf("history[%d].ID = %d, want %d", i, history[i].ID, want)
		}
	}
	if history[0].Author != "sess-1" {
		t.Errorf("history[0].Author = %q, want sess-1", history[0].Author)
	}
	if history[1].Author != "" {
		t.Errorf("history[1].Author = %q, want empty", history[1].Author)
	}
}

func TestTaskLog_AppendAndKindFilter(t *testing.T) {
	s := openTestStore(t)

	taskID, err := s.AddTask(Task{Title: "T", ProjectID: "billing"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	id1, err := s.AddTaskLog(TaskLogEntry{TaskID: taskID, Kind: "note", Body: "n1", Author: "sess-1"})
	if err != nil {
		t.Fatalf("AddTaskLog n1: %v", err)
	}
	id2, err := s.AddTaskLog(TaskLogEntry{TaskID: taskID, Kind: "decision", Body: "d1"})
	if err != nil {
		t.Fatalf("AddTaskLog d1: %v", err)
	}
	id3, err := s.AddTaskLog(TaskLogEntry{TaskID: taskID, Kind: "note", Body: "n2"})
	if err != nil {
		t.Fatalf("AddTaskLog n2: %v", err)
	}

	all, err := s.ListTaskLog(taskID, "")
	if err != nil {
		t.Fatalf("ListTaskLog all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all len = %d, want 3", len(all))
	}
	wantOrder := []int64{id1, id2, id3}
	for i, want := range wantOrder {
		if all[i].ID != want {
			t.Errorf("all[%d].ID = %d, want %d", i, all[i].ID, want)
		}
	}
	if all[0].Author != "sess-1" {
		t.Errorf("all[0].Author = %q, want sess-1", all[0].Author)
	}
	if all[1].Author != "" {
		t.Errorf("all[1].Author = %q, want empty", all[1].Author)
	}

	notes, err := s.ListTaskLog(taskID, "note")
	if err != nil {
		t.Fatalf("ListTaskLog note: %v", err)
	}
	if ids := logIDs(notes); !idsEqual(ids, []int64{id1, id3}) {
		t.Errorf("notes ids = %v, want [%d %d]", ids, id1, id3)
	}

	decisions, err := s.ListTaskLog(taskID, "decision")
	if err != nil {
		t.Fatalf("ListTaskLog decision: %v", err)
	}
	if ids := logIDs(decisions); !idsEqual(ids, []int64{id2}) {
		t.Errorf("decisions ids = %v, want [%d]", ids, id2)
	}
}

func mustAddTaskSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.AddSession(Session{
		ID: id, Kind: "orchestrator", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b1",
		WorktreePath: "/wt/" + id, TmuxName: "t-" + id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession %s: %v", id, err)
	}
}

func taskIDs(tasks []Task) []int64 {
	ids := make([]int64, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}

func logIDs(entries []TaskLogEntry) []int64 {
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

func idsEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAddTask_BrainstormIsValid(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddTask(Task{Title: "Discuss it", ProjectID: "billing", Status: "brainstorm"})
	if err != nil {
		t.Fatalf("AddTask brainstorm: %v", err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "brainstorm" {
		t.Errorf("Status = %q, want brainstorm", got.Status)
	}

	if err := s.UpdateTaskStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus in_progress: %v", err)
	}
	if err := s.UpdateTaskStatus(id, "brainstorm"); err != nil {
		t.Fatalf("UpdateTaskStatus brainstorm: %v", err)
	}
}

func TestIsActiveTaskStatus(t *testing.T) {
	active := map[string]bool{
		"brainstorm":  true,
		"in_progress": true,
		"backlog":     false,
		"review":      false,
		"done":        false,
		"cancelled":   false,
	}
	for status, want := range active {
		if got := IsActiveTaskStatus(status); got != want {
			t.Errorf("IsActiveTaskStatus(%q) = %v, want %v", status, got, want)
		}
	}

	// ActiveTaskStatuses is the same set in canonical order, and every entry
	// of it must be a status the store accepts.
	want := []string{"brainstorm", "in_progress"}
	if len(ActiveTaskStatuses) != len(want) {
		t.Fatalf("ActiveTaskStatuses = %v, want %v", ActiveTaskStatuses, want)
	}
	for i, s := range ActiveTaskStatuses {
		if s != want[i] {
			t.Errorf("ActiveTaskStatuses[%d] = %q, want %q", i, s, want[i])
		}
		if !validTaskStatuses[s] {
			t.Errorf("ActiveTaskStatuses[%d] = %q is not a valid task status", i, s)
		}
	}
}
