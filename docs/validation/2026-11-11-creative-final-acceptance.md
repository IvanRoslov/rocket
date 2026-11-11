# Creative final acceptance — release 1 verification

Task 6073 / subtask 6074, gate 331, spec v1 and plan v1. The orchestrator accepted the design before implementation. Later spec v2 navigation remains separate.

## Scenarios and evidence

| Acceptance scenario | Evidence |
| --- | --- |
| Final action after last-stage approval, remain selected, archive survives reload | App tests and isolated Playwright scenario start at stage 11; acceptance retains its URL, hides mutations, and reload shows accepted state. Existing stage review suite remains green. |
| Old completed work remains active | Store eligibility test distinguishes `completed` from `closure_state=open`; upgrade test preserves legacy graph and checks open defaults. |
| Stop/save failure is honest and retryable | API tests inject Destroy error, partial shutdown, inventory error, lingering runtime, session-state save failure and final archive save failure. Each stays closing/visible, rejects restore, then succeeds on intentional retry. UI/browser inject a 500, show Closure incomplete and Retry closure, and never show accepted early. |
| Archive rejects stale mutations and restores | Store tests cover rework/feed/config/discussion/artifact writes; API tests cover run mutation, generic messaging, kill cleanup, launch, direct manager restore, current-version changes, upload and preview registration/proxy access. Generic legacy question writes have SQL barriers. |
| Retain history/results and isolate other work | API verifies exact published content/download bytes after acceptance, all bound sessions killed, an unrelated session still alive and no workspace deletion. Store retention test retains Creative messages. Existing project/run binding tests and Development regression suites pass. |
| Duplicate/concurrent acceptance | Four simultaneous requests receive success for one durable decision; repeated requests preserve acceptance timestamp. Acceptance tests pass with the race detector. |

## Commands

- `go test ./...` and `go test -p 2 ./...` — passed after final runtime error-classification fix.
- `go test -race ./internal/api -run CreativeAcceptance -count=1` — passed, including partial-stop and session-state-save retry cases.
- `go test ./internal/runtime -run TmuxClosureProof -count=1` — RED for swallowed silent/permission failures, then GREEN; uses an isolated fake tmux executable through the real command adapter.
- `npm test --prefix creative-web` — 95 tests / 13 files passed.
- `npm run build --prefix creative-web` — passed (TypeScript and Vite).
- `npm test --prefix web` — 722 tests / 62 files passed.
- `npm run build --prefix web` — passed; existing bundle-size advisory remains.
- `git diff --check` — passed.

The first unconstrained Go run, concurrent with UI tests, exceeded the existing Claude hook fixture's three-second deadline. It also exposed the archive middleware changing unknown-session behavior on unrelated endpoints. A reproducing test now protects that policy; the complete rerun passes. No unrelated lifecycle refactor was needed.

Fresh review identified one acceptance failure: production tmux inventory swallowed ambiguous command failures. The runtime tests reproduced it; inventory now propagates errors and Destroy/List recognize only positive absent-server diagnostics. No other concrete acceptance failure was found.

## Browser evidence and limits

`creative-web/browser/final-acceptance-check.mjs` was exercised with Playwright against a worktree-only Vite server at port 15574. Every `/v1` request was intercepted; no live Rocket daemon or paid provider was contacted. It covers failure/retry, same-work selection, reload, retained download link, project Completed works, read-only controls and mobile width. Runtime stopping and file bytes are covered separately through real store/manager/API integration with runtime doubles. Production tmux diagnostic classification is exercised by the isolated command-adapter test. This is not live-user acceptance or deployment.

Screenshots: [desktop](../screenshots/creative-final-acceptance-desktop.png), [mobile](../screenshots/creative-final-acceptance-mobile.png).
