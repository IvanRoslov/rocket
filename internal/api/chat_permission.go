package api

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// permissionMark is the part of a chat cursor that tracks the permission
// journal for one client: the highest resolved permission_prompts id it
// was given and the newest entry ts it has seen. It rides on the opaque
// next_cursor as a "#p<id>.<ts>" suffix, so incremental reads deliver each
// resolved dialog exactly once without any client change.
type permissionMark struct {
	present bool
	id      int64
	ts      int64
}

// chatCursorMark matches the suffix. Adapter cursors end in ":<offset>",
// so a trailing "#p<digits>.<digits>" cannot be part of one.
var chatCursorMark = regexp.MustCompile(`#p([0-9]+)\.([0-9]+)$`)

// splitChatCursor separates a client cursor into the agent adapter's cursor
// and the permission mark.
func splitChatCursor(cursor string) (string, permissionMark) {
	m := chatCursorMark.FindStringSubmatchIndex(cursor)
	if m == nil {
		return cursor, permissionMark{}
	}
	id, err1 := strconv.ParseInt(cursor[m[2]:m[3]], 10, 64)
	ts, err2 := strconv.ParseInt(cursor[m[4]:m[5]], 10, 64)
	if err1 != nil || err2 != nil {
		return cursor, permissionMark{}
	}
	return cursor[:m[0]], permissionMark{present: true, id: id, ts: ts}
}

// joinChatCursor is the inverse of splitChatCursor. An empty adapter cursor
// stays empty: clients read "" as "no cursor yet".
func joinChatCursor(adapterCursor string, mark permissionMark) string {
	if adapterCursor == "" || !mark.present {
		return adapterCursor
	}
	return adapterCursor + "#p" + strconv.FormatInt(mark.id, 10) + "." + strconv.FormatInt(mark.ts, 10)
}

// mergePermissionEntries turns transcript entries plus the session's
// resolved permission rows into the response entries, and computes the
// mark for next_cursor.
//
// Which rows go in:
//   - tail read (no cursor), or a cursor read the adapter answered from
//     scratch because its cursor was stale (detected by the first entry
//     being older than the newest one the client already saw): every row,
//     each in place, so the batch looks exactly like a tail read and the
//     clients' rollback handling finds their own entries in it;
//   - an ordinary cursor read: rows resolved since the client's mark;
//   - a cursor without a mark (a client loaded before this existed): none
//     — its next tail read shows the history in place.
//
// A row sits at its asked_at, before the first transcript entry that is
// newer; transcript order itself is never changed (entries without a
// timestamp keep their place).
func mergePermissionEntries(entries []agent.ChatEntry, rows []store.PermissionPromptRow, tail bool, mark permissionMark) ([]chatEntryResponse, permissionMark) {
	var maxID int64
	for _, r := range rows {
		if r.ID > maxID {
			maxID = r.ID
		}
	}
	lastTS := mark.ts
	for _, e := range entries {
		if e.TS > lastTS {
			lastTS = e.TS
		}
	}

	rescan := len(entries) > 0 && entries[0].TS > 0 && entries[0].TS < mark.ts
	var pick []store.PermissionPromptRow
	for _, r := range rows {
		switch {
		case tail || (mark.present && rescan):
			pick = append(pick, r)
		case mark.present && r.ID > mark.id:
			pick = append(pick, r)
		}
	}
	sort.SliceStable(pick, func(i, j int) bool { return pick[i].AskedAt < pick[j].AskedAt })

	out := make([]chatEntryResponse, 0, len(entries)+len(pick))
	for _, e := range entries {
		for len(pick) > 0 && e.TS > 0 && e.TS > pick[0].AskedAt {
			out = append(out, toPermissionEntry(pick[0]))
			pick = pick[1:]
		}
		out = append(out, toChatEntryResponse(e))
	}
	for _, r := range pick {
		out = append(out, toPermissionEntry(r))
	}

	next := permissionMark{present: maxID > 0 || mark.present, id: maxID, ts: lastTS}
	if mark.id > next.id {
		next.id = mark.id
	}
	return out, next
}

func toPermissionEntry(r store.PermissionPromptRow) chatEntryResponse {
	return chatEntryResponse{
		Role: "permission",
		Text: r.Title,
		TS:   r.AskedAt,
		Permission: &permissionEntryResponse{
			Title:       r.Title,
			Context:     r.Context,
			AnswerLabel: r.AnswerLabel,
			AnsweredVia: r.AnsweredVia,
		},
	}
}
