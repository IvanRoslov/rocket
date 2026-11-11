# Brief: creative chat window + attachment cards (task #6061, spec v6, plan deliverable 3)

## Goal
The human finds the creative Conversation tab unusable: "looks terrible, I can't tell where the chat is, everything jumps around". Make it behave like a normal messenger chat window. Screenshot of the current state: messages are full-width blocks, each human message lists 15+ version links, the context label is printed twice, composer is a plain form above the feed.

## Current facts (main)
- `creative-web/src/Discussion.tsx` (composer + newest-first feed), `FeedEntry.tsx`, `FeedReadMarker.tsx`, `QuestionArchive.tsx`, `feed.ts`. Feed polls every 2s.
- Human messages auto-attach all displayed artifact versions of the selection (`context.artifact_version_ids`) — keep that data behaviour, change only how it is shown.
- Docs: `docs/creative-dashboard.md` (Conversation paragraph says newest-first, composer above — must be updated).

## Required behaviour (spec v6, "Вид чата (v5)" + attachments)
1. Chat is a window of fixed height within the viewport with ITS OWN scroll; the page around does not scroll with messages.
2. Newest messages at the BOTTOM, older above; composer pinned to the bottom of the window. On open, the window is at the bottom (or at the first unread entry if it is above the visible area).
3. If the user is at the bottom, a new message appears and the window stays at the bottom. If the user scrolled up, the window does NOT shift; a "N new ↓" pill appears near the bottom; clicking it scrolls to the latest. No scroll jumps on polling refresh. Typed draft and scroll position survive data refreshes.
4. Header states whose chat it is: "Stage chat «<name>»" / "Run chat"; for an item also the item name, plus the existing "Only this item" toggle.
5. Bubbles: human on the right with accent colour, orchestrator on the left; readable max width. Proposal cards visually distinct; undecided cards obvious. A pinned line above messages "Awaiting decision: N" that jumps to the undecided card(s) without manual scrolling through history.
6. Attachments: versions attached to an ORCHESTRATOR message render as attachment cards in the bubble — preview for images/video (type icon otherwise), readable artifact name, item it belongs to, version number; click opens the existing artifact viewer (HTML keeps isolated viewing). Many attachments: show first few, rest behind "+N more".
7. Auto-attached context versions on the HUMAN's own message are NOT listed: one line "About: <item/stage> · N versions", expandable.
8. Composer: context shown once, short, next to the input ("About: <item/stage> · N versions", expandable); remove the duplicated context label. Send by button and Cmd/Ctrl+Enter. Existing draft-keeps-target behaviour stays.
9. Short time (today: HH:MM), date separators between days.
10. Legacy question archive stays collapsed below the chat window. Unread counters/read marking unchanged (read marking must still work with the new scroll model: opening the visible chat marks read through the highest displayed entry).
11. Works in light and dark themes and narrow screens. UI language: keep whatever the dashboard uses now (English labels) — the human's quoted Russian labels are illustrative.

## Constraints
- Scope is the creative web Conversation tab. Feed data, card decisions, API contracts, agent rules are NOT changed. Exception: if the existing API does not give an attachment's name/type/item/media URL for a version, a minimal read-only addition is allowed in this PR — tell me before you build it.
- Follow your Superpowers workflow: brainstorm your design, send me a SHORT summary (layout approach, scroll model, where attachment metadata comes from) before implementing; TDD; verification before completion.

## Acceptance
- Existing creative-web tests green; new tests for: bottom-anchored order, no shift + "N new" pill when scrolled up, collapsed human context, attachment cards (name/item/version, +N more), Cmd/Ctrl+Enter.
- `tsc` and `make creative-web-build` green; `make test` green.
- Screenshots before/after for stage, item and run chats, light + dark, narrow width, with an item that has 10+ versions — put them under `docs/screenshots/creative-feed/` and in the PR.
- `docs/creative-dashboard.md` updated.
- Open a PR to main and send me the link.
