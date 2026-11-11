# Creative final acceptance — release 1

Gate 331, task 6073 / delivery 6074. Keep derived stage completion separate from explicit human acceptance. Existing completed runs remain active until accepted.

## Approach

Use durable closure state on each run: open, closing, accepted, with acceptance timestamp and last closure error. Human acceptance first atomically checks every stage is completed and freezes the run in closing. Under the existing manager launch/restore lock, stop only sessions bound to this run, preserving workspace and publications. Confirm runtime removal using the error-reporting runtime inventory (ordinary Kill intentionally ignores Destroy errors). Persist accepted only after every stop and session-state save succeeds. Failure leaves closing visible in active work with its error and an explicit retry action; never silently unfreeze partially stopped work. Concurrent acceptance requests are idempotent.

An alternative of stopping before freezing races agent writes/launches and stale stage reopens. Marking accepted before stopping would hide incomplete closure. Neither satisfies the brief.

## Service and persistence

Guard Creative writes transactionally while closing or accepted, including stages/items, artifact publication/current pointers, feed/cards, discussion writes and run configuration. Preserve read access and read receipts. Reject launch/restore at manager entry points, and generic session/message/interactive mutations for bound closed runs at service boundaries. Ensure no other run/project or Development session is stopped. Retain immutable stage decisions, feed history, artifact versions and downloadable files. No bulk migration of completed runs.

## Existing UI

Show an always-visible final acceptance panel when all stages complete, including the last stage view. Explain permanence, stopped agents and retained results. Keep the selected run and show accepted success after acceptance and reload. Closing errors show incomplete closure and Retry closure. Accepted views hide editing, feed/card/review and agent launch/restore controls. Add project-scoped Completed works access; exclude accepted runs from active cards and the top work selector while preserving direct links.

## Verification

TDD at store, manager, API and React boundaries: eligibility, human identity, duplicate requests, stop and save errors/retry, cross-run isolation, stale requests, restore rejection, readable retained histories/results and old completed-run compatibility. Run repository Go tests, Creative UI tests/build, Development UI regression tests/build and an isolated browser scenario if supported. No live deployment/restart or global dashboard.
