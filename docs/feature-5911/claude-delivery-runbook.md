# Claude-compatible delivery ownership

Task5914 implements approved task5911 Spec v3/Plan v3, Go3 gate259 (continuing Go253). Deployment and live verification require separate operational authorization. All implementation verification uses temporary fixtures; this runbook does not authorize live sends, replay, probes, registry/hook edits, daemon replacement or worker restore.

Claude Code and DeepSeek inherit the optional Claude socket capability. With SocketDelivery enabled, an unavailable or ambiguous exact launch stays on that route under the existing configured queued-message policy; it never falls back to tmux. Legacy workers without a persisted launch anchor remain unavailable. New hooks require Python3 and emit bounded metadata only. Missing tools, unverifiable process ancestry, incomplete registry evidence and journal uncertainty reduce availability rather than establish readiness or death. Generic UI newest-transcript selection is unchanged.

Before the first write, an atomic claim saves the immutable target, wire ID, exact envelope/reply path, complete journal boundary/device/inode and deadline. The persisted transport marker or surviving Claude side row owns recovery even if configuration or current launch changes. A possible write, silence or held outcome is never automatically resent. Proven prewrite failure also uses a conservative single-invocation policy. Denied/expired receipts are authoritative only after full UUID/message/socket/current-root validation; a receipt without UUID is advisory. Receipt approval is not final delivery.

Delivery requires one exact incoming user envelope in the pinned UUID journal after the complete prewrite byte boundary. Consumption is atomic in the physical Claude UUID/device/inode/record-ID domain across all Rocket generations. Equal pending envelopes cannot be allocated arbitrarily, even with two matching records. Missing/corrupt historical ownership, replacement files with unresolved same-UUID history and competing equal envelopes fail closed. A positive QueueTimeout persists the exact age deadline, message CreatedAt plus that configured value. The ordinary loaded Rocket default is unchanged. Explicit QueueTimeout<=0 persists zero time.Time plus SQL deadline_ns=0, disabling age expiry for possible-write/accepted; it introduces no Claude-only numeric fallback. Unmarked queued housekeeping retains the baseline zero/negative cutoff and status CAS. Later configuration changes never reinterpret an existing snapshot's age mode. Positive expiry fails delivery_unconfirmed; later evidence never reopens terminal failure.

For either age mode, independently verified saved PID/start death or terminal/missing Rocket session with the same saved-launch evidence triggers one final exact proof scan, then failed/delivery_unconfirmed if not proven. Current Resolve, a replacement generation's state, raw exited or SessionEnd never prove an older target dead. If deletion removes generation evidence, absence alone is uncertainty and independent saved-root liveness remains necessary. An old live/unknown target with disabled age may remain pending without a finite age limit; it is never resent. Lifecycle closure does not establish a final writer-closed horizon for purge.

Socket I/O retains its finite operation timeout and shorter caller/positive age bounds. Both initial and atomic proof scans use the same caller-derived maximum5s context. The proof transaction fails closed on SQLite writer contention using a checked-out connection-local timeout, restoring its original setting before pool reuse or discarding on restoration failure. Proof confirms receipt of feedback, not execution of its contents.

## Retention exception

Unresolved possible-write history includes public terminal delivery_unconfirmed. These main message, side snapshot and required consumed-record rows are protected from ordinary30day PurgeOld **without a finite retention limit**. A deadline, stable mtime, launch replacement, root death alone or an inode change does not prove that no surviving writer can append later. The current CLI contract supplies no trustworthy final writer-closed horizon, so no timer-based cleanup, GC endpoint, eviction or archive is added. Native/legacy retention remains unchanged; unrelated resolved Claude history can purge, with foreign keys cascading safely. Damaged, unattributable Claude history conservatively protects Claude evidence until it can be validated.

This exception can exceed30days and cause database/storage growth. The complete immutable envelope is retained because exact comparison is required; hook payload content is never stored. Disk-full or claim-persistence failure sends nothing. No metrics endpoint or telemetry is introduced.

The following diagnostic queries are read-only examples for an operator's separately authorized database inspection. They were not run against production during this task. Payload bytes are logical body/snapshot lengths, not SQLite page or WAL allocation.

```sql
SELECT c.claude_uuid, c.journal_device, c.journal_inode, c.journal_path,
       c.phase, CASE WHEN c.deadline_ns=0 THEN 'disabled' ELSE 'positive' END AS age_expiry,
       COUNT(*) AS unresolved_rows,
       SUM(length(CAST(c.snapshot_json AS BLOB)) +
           length(CAST(m.body AS BLOB))) AS logical_payload_bytes,
       MIN(m.created_at) AS oldest_created_at_unix,
       unixepoch() - MIN(m.created_at) AS oldest_age_seconds
FROM messages m JOIN claude_deliveries c ON c.message_id = m.id
WHERE c.phase IN ('possible_write','unknown','held','accepted','unconfirmed')
GROUP BY c.claude_uuid, c.journal_device, c.journal_inode, c.journal_path, c.phase, (c.deadline_ns=0);

-- A positive marker lacking its side row remains owned and unavailable.
SELECT COUNT(*) AS missing_snapshots,
       SUM(length(CAST(m.body AS BLOB))) AS body_bytes,
       MIN(m.created_at) AS oldest_created_at_unix,
       unixepoch() - MIN(m.created_at) AS oldest_age_seconds
FROM messages m LEFT JOIN claude_deliveries c ON c.message_id = m.id
WHERE m.delivery_transport = 'claude-socket-v1' AND c.message_id IS NULL;

-- Redundant marker damage does not erase side-row ownership.
SELECT COUNT(*) AS side_rows_with_empty_main_marker
FROM messages m JOIN claude_deliveries c ON c.message_id = m.id
WHERE m.delivery_transport = '';
```

Do not remove unresolved rows merely to reclaim space or make a later message confirm. Safe removal requires exact journal-delivered proof or a fully correlated definitive negative, with no overlapping competitor needing that history, or a separately verified final writer-closed journal horizon and correlation resolution. No operational deletion procedure is released by this task.

## Lifecycle compatibility

The exact verified UUID journal owns Claude lifecycle activity. A foreign newer journal cannot clear the real root's API-error blocked state or impose a foreign error block. Missing/corrupt/wrong-UUID/future journal evidence is uncertainty; verified OS liveness is not readiness. SessionEnd remains a hint; only independent exact process/runtime evidence establishes death. Current-generation activity and event high-water transitions are atomic, with hook `at`/`last_event_at` in UnixNano and zero accepted future skew. Heartbeat consumes the persisted activity; it supplies no independent OS-death proof.

Qualified waiting_input retains an actual prompt, pending quiz or capture uncertainty. Only consecutive prompt-free misses in the same launch generation may clear stale waiting, and unknown exact-journal evidence cannot synthesize ready. Generic correction behavior for other adapters is unchanged. ClaudeCode/DeepSeek do not implement TurnFailureReporter or StartupProber on the pinned baseline: their generic turn-failure/start-screen calls were no-ops. This task preserves exact-journal API-error blocking without adding a new parent-alert or startup capability.

## Conservative availability limits

Registry validation rejects unreadable/incomplete numeric entries rather than silently dropping potential competing writers. Exact confirmation scans at most32MiB after the pinned boundary, reassembling complete records within that total bound. Large unrelated records do not hide later exact envelopes; large exact records and JSON escaping/metadata are parsed rather than skipped. Incomplete records cannot prove reception, and later duplicates still reject unique proof. Lifecycle reads the last1MiB. A larger total tail or uncertain input remains unconfirmed/unknown. Historical snapshot validation is deliberately comprehensive so an indexed scalar mismatch cannot hide ownership. Large retained histories can reduce confirmation availability; cancellation/deadline failure never permits a resend or proof consumption. Independent whole-branch review and separately authorized rollout remain required.
