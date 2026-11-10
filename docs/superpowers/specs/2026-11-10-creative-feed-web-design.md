# Creative conversation feed web design

Task 6063 implements deliverable 2 of creative-messages, parent task 6061 spec v3 and plan v2. The binding API is the Conversation feed HTTP contract in docs/creative-agents.md at cc009df.

Keep the existing React Query workspace and replace Discussion's question composer with a message composer above a newest-ID-first feed. Item selection tags writes but reads its containing stage by default; an explicit item filter changes reads only. Freeze scope, display label and immutable version IDs on the first draft edit. Preserve drafts on failed writes and selection/tab changes.

Separate conversation data/decision HTTP handling, entry/card presentation, and the existing question archive. Reuse shared Markdown and BoundVersions. Pending proposal cards are highlighted with Go/Revise/Deny; decided cards show decision/time; replaced cards resolve the successor via its GET endpoint and navigate to its feed/card, including cross-feed replacements. Keep the existing formal review unchanged.

Poll feed and unread every 2000 ms while a run is open. Read markers use the highest displayed entry ID, including filtered views, only when the discussion is visible. Successful reads refetch unread, successful writes/decisions refetch feed/unread. Handle conflict envelopes locally because the shared API wrapper currently discards the returned entry. Keep all errors in place and retain drafts.

Use the existing English UI conventions: Conversation, Message, Send message, Only this item, Old questions. Archive questions keep exact run/stage/item scope, existing reply drafts and answer actions. No new question creation.

Approaches considered: extending the existing question list would conflate separate HTTP entities; a new standalone messenger app would duplicate workspace/version state. Selected approach: focused components within the current Discussion/workspace and existing query cache.

Validation: component tests with exact MSW contract responses, browser checks with isolated contract mocks and committed screenshots, make test and make build. No daemon restart, binary install, paid calls or API changes. Merge only after core and rebase then.
