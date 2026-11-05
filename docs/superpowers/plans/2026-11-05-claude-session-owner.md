# Claude session-owner compatibility implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this single task with focused TDD.

**Goal:** Restore lifecycle readiness when Claude 2.1.318 intersperses timestamp-less session-owner metadata.

**Architecture:** Extend the existing non-lifecycle metadata allowlist. Keep strict JSON decoding, exact session identity, API-error precedence, lifecycle timestamps, and all queue/proof policies unchanged.

**Tech Stack:** Go, SQLite test stores, synthetic JSONL fixtures.

**Spec:** Task #6001 orchestrator acceptance: recognize session-owner; valid lifecycle remains eligible; metadata alone, malformed/unproven/mismatched evidence cannot establish readiness; API errors remain fail-closed.

## Global constraints

No live restart, resend, session mutation, user configuration edit, paid call, or user transcript/account data in fixtures. One focused PR for feature creative-work.

## Review focus

Metadata-only journals; metadata after an API error; top-level/nested API-error flags on metadata; malformed metadata timestamps/JSON; foreign-session lifecycle evidence. Extend existing tests for each.

### Task 1: Recognize session-owner without creating lifecycle authority

**Files:** `internal/monitor/claude_journal.go`, `internal/monitor/claude_journal_test.go`; existing queue eligibility tests validate consumer behavior.

**Interfaces:** Existing `claudeJournalActivity` and monitor polling persist lifecycle state. Queue continues consuming persisted state through `claudeEligible`, with no interface changes.

- [x] Extend synthetic metadata and ordered fixtures with session-owner and empty account/organization values; add owner-specific error and malformed cases.
- [x] Run monitor regressions and observe readiness/block preservation failures before changing production code.
- [x] Add session-owner to the existing error-guarded metadata switch.
- [x] Run monitor, queue, claudeinstance and store tests plus relevant vet checks; inspect diff for unchanged proof/queue semantics.
- [ ] Commit and open one PR with regression evidence and recovery guidance.

## Recovery guidance

An operator must deploy the merged fix through the normal process. Recheck exact identity and monitor activity before relying on delivery. Still-queued, unexpired answers remain eligible for normal processing; do not resend duplicates. Timeout-failed messages remain terminal: review the question's latest answer and send only still-needed current guidance through the normal workflow. Never replay superseded answers or change delivery proof/status by hand. This task performs none of those live actions.

## Validation evidence

Before the fix, synthetic owner records made ordered ready/active/blocked and block-preservation cases fail with ErrNoSignal. After the one-line fix, complete monitor, queue, claudeinstance and store package tests passed, as did go vet for those packages. Monitor ordering tests exercise polling and persisted activity; queue tests exercise eligibility, identity rechecks and exact delivery proof independently. No live delivery was attempted.
