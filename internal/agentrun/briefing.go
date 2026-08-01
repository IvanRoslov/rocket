package agentrun

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// dossierStore is the slice of the store BuildBriefing needs; it keeps the
// builder testable and documents that briefing assembly is read-only.
type dossierStore interface {
	ListAgentItems(roleID, state string) ([]store.AgentItem, error)
	GetTask(id int64) (store.Task, error)
}

// BuildBriefing renders the first message of a role instance: the batch of
// inbox events that woke it, the slice of the dossier that matters right now,
// the role's memory index and a reminder of how a run ends.
//
// Dossier selection (per the spec): entries referenced by the events, plus
// everything deferred or waiting_team, plus everything with a rocket task
// attached (annotated with the task's current status). Anything else the role
// can look up itself with `rocket agent state ls`.
func BuildBriefing(st dossierStore, home string, role store.Agent, events []store.AgentInboxEvent) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "[rocket wake] You are role %q. %s\n\n", role.ID, plural(len(events), "new inbox event", "new inbox events"))

	b.WriteString("## Inbox\n\n")
	if len(events) == 0 {
		b.WriteString("(empty)\n")
	}
	refs := make(map[string]bool)
	for _, e := range events {
		payload := decodePayload(e.Payload)
		ref := stringField(payload, "ref")
		if ref != "" {
			refs[ref] = true
			ref = " " + ref
		}
		fmt.Fprintf(&b, "- #%d %s%s%s\n", e.ID, e.Kind, ref, senderSuffix(payload))
		for _, line := range strings.Split(eventBody(e, payload), "\n") {
			fmt.Fprintf(&b, "      %s\n", line)
		}
	}

	items, err := selectDossier(st, role.ID, refs)
	if err != nil {
		return "", err
	}

	b.WriteString("\n## Dossier\n\n")
	if len(items) == 0 {
		b.WriteString("(empty)\n")
	}
	for _, it := range items {
		fmt.Fprintf(&b, "- %s:%s [%s]%s%s\n", it.Kind, it.ExternalRef, it.State,
			taskSuffix(st, it), noteSuffix(it.Note))
	}

	b.WriteString("\n## Memory\n\n")
	b.WriteString(readMemoryIndex(home, role.ID))

	b.WriteString("\n## Reminder\n\n")
	b.WriteString("Act on the events above following your triage policy (the system prompt " +
		"carries its current version). Record what you decide in the dossier " +
		"(`rocket agent state set <kind>:<ref> <state> --note \"...\"`, `--until` for " +
		"anything you are deferring), write durable facts into your memory files and " +
		"update MEMORY.md. Reply to whoever wrote to you. When the inbox is worked " +
		"through, finish the run with `rocket agent done`.\n")

	return b.String(), nil
}

// selectDossier returns the dossier rows worth putting in front of the role,
// deduped and ordered by most recently updated.
func selectDossier(st dossierStore, roleID string, refs map[string]bool) ([]store.AgentItem, error) {
	all, err := st.ListAgentItems(roleID, "")
	if err != nil {
		return nil, fmt.Errorf("list dossier: %w", err)
	}

	var out []store.AgentItem
	for _, it := range all {
		relevant := refs[it.ExternalRef] ||
			it.State == "deferred" || it.State == "waiting_team" ||
			it.TaskID != 0
		if relevant {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// taskSuffix annotates a dossier entry that carries a rocket task with that
// task's title and current status — the whole point of waking on task
// updates is that the role sees where its tasks stand.
func taskSuffix(st dossierStore, it store.AgentItem) string {
	if it.TaskID == 0 {
		return ""
	}
	task, err := st.GetTask(it.TaskID)
	if err != nil {
		return fmt.Sprintf(" (task #%d: unavailable)", it.TaskID)
	}
	return fmt.Sprintf(" (task #%d %q — %s)", task.ID, task.Title, task.Status)
}

func noteSuffix(note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	return " — " + note
}

// senderSuffix renders "from" when the payload names a sender.
func senderSuffix(payload map[string]any) string {
	if from := stringField(payload, "from"); from != "" {
		return " from " + from
	}
	return ""
}

// eventBody renders the human-readable part of an event: the text/body field
// when there is one, the raw payload otherwise (task_update, cron and future
// sources carry structured data the role reads as JSON).
func eventBody(e store.AgentInboxEvent, payload map[string]any) string {
	for _, field := range []string{"text", "body", "title"} {
		if v := stringField(payload, field); v != "" {
			return v
		}
	}
	if len(payload) == 0 {
		return "(no payload)"
	}
	compact, err := json.Marshal(payload)
	if err != nil {
		return strings.TrimSpace(e.Payload)
	}
	return string(compact)
}

func decodePayload(raw string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	// Drop empty scalars so "(no payload)" and the suffixes below don't
	// render placeholders for fields the enqueuer left blank.
	for k, v := range m {
		if s, ok := v.(string); ok && s == "" {
			delete(m, k)
		}
	}
	return m
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	s, _ := payload[key].(string)
	return s
}

// readMemoryIndex returns the role's MEMORY.md verbatim; a missing or empty
// index is reported as such rather than failing the wake — a role with no
// memory yet is perfectly normal.
func readMemoryIndex(home, roleID string) string {
	data, err := os.ReadFile(roles.MemoryIndexPath(home, roleID))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "(empty — write durable facts into " + roles.MemoryDir(home, roleID) + " and index them in MEMORY.md)\n"
	}
	out := string(data)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s.", n, one)
	}
	return fmt.Sprintf("%d %s.", n, many)
}
