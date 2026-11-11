# Global Creative overview — release 2 verification

Task 6076, parent 6073, feature ui-improve-3. Gate 332 spec v2 / plan v2. Based on release 1 PR #217, commit 4f3cb50. Orchestrator accepted the implementation design before coding.

## Acceptance evidence

| Scenario | Evidence |
| --- | --- |
| Same-name works across projects | App/Overview tests and browser show Opening in Film studio and Music studio as distinct global cards. Each opens the authoritative run/project context. Neither top selector remains. All works, reload, real browser back/forward, Completed reload and stage/item deep links pass. A stale project query cannot override the run's project. |
| Existing unread semantics | Real per-run useUnread caches poll at two seconds on visible cards. Browser changes an unseen stage's count from 2 to 5 without any read POST. Opening the actual stage marks through entry 41, then returning shows no badge. Error hides stale count and shows Unread unavailable; work still opens and recovers. No conversation/read marker mounts on overview. |
| Final acceptance and global Completed | Tests/browser retain derived-completed and closing works in Active. Fixture acceptance moves Film work to Completed alongside an accepted Music work. Opening accepted work retains release 1 read-only behavior and direct links. Existing final-acceptance tests cover retry and retained artifact selection. Backend lifecycle code is unchanged. |
| Explicit New work project | From overview Project is blank even with a legacy project-only query; Create work stays disabled until an explicit choice. Browser POST creates Music intro with project_id=music and opens its Music context. From run scope the project initializes explicitly; New work reload/Cancel/back/forward preserve stage/item context. Pipelines and Development remain reachable. |
| Honest empty/loading/error and responsive UI | Tests distinguish Loading works, list failures and known empty Active, including pending project catalog. New work and Completed remain accessible from empty Active. Browser list failure cannot pretend to be empty. Desktop, workspace and 390px mobile screenshots inspected; mobile has no horizontal overflow. |

## TDD and review

Original App baseline: 13 tests passed. Seven new global overview/creation tests failed before implementation. Updated removed-selector interactions retain existing workspace/profile/archive assertions. A fresh reviewer found two acceptance failures: New work dropped item context, and cached run data masked failed refetches. Reproducing tests failed first and both are fixed. The additional pending-project-catalog test also failed first and now passes. Visual QA found invisible unread badge text; an isolated computed-color assertion reproduced it before the CSS fix, and the final screenshot visibly shows the number.

## Verification results

- `npm test --prefix creative-web`: **105 passed**, 14 files.
- `npm run build --prefix creative-web`: TypeScript and Vite passed.
- `npm test --prefix web -- --run`: **722 passed**, 62 files.
- `npm run build --prefix web`: passed; existing large-bundle advisory remains.
- `go test -p 2 ./...`: passed across the repository.
- `git diff --check`: passed.
- `creative-web/browser/overview-check.mjs`, executed with Playwright against worktree-only Vite at port 15576: **178 intercepted API requests, zero page errors**, correct explicit Music POST, Active=3 / Completed=2 after fixture acceptance. Browser assertions cover actual history, reload, item context, unread polling/read/error, creation, acceptance, archive, Pipelines and responsive overflow.

Tests emit the existing Node localStorage experimental warning. Browser deliberately injects 503 responses for unread/list error evidence; those expected HTTP console errors are not page exceptions. No live Rocket API, user work, paid provider, deployment or restart was used. Runtime closure and archive retention are unchanged and remain covered by release 1 backend tests/full Go suite.

Screenshots: [overview desktop](../screenshots/creative-overview-desktop.png), [mobile](../screenshots/creative-overview-mobile.png), [opened item/header](../screenshots/creative-overview-workspace.png).

## Limits

The existing global list returns full run graphs and unread uses one existing endpoint per visible work card. This release introduces no new pagination or aggregate endpoint. Very large catalogs could also benefit from those later, outside gate 332's closed scope.
