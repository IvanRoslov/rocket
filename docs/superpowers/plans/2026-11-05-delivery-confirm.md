# Delivery confirmation implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Confirm strictly correlated Claude mid-turn consumption using existing shared finalization.
**Architecture:** Add an internal journal-record matcher called by FindProof. Retain its pinned-file bounds, ambiguity counting and existing store atomic proof consumption unchanged.
**Tech Stack:** Go, JSONL, SQLite, existing queue test harness.
**Spec:** ../specs/2026-11-05-delivery-confirm-design.md

## Global constraints

No live restart, resend, session mutation or database repair. Synthetic fixtures only. Existing exact identity, at-most-once and deadline rules remain authoritative.

## Review focus

Missing removal, foreign correlation, incomplete render, duplicate consumption and terminal recovery must fail closed. Existing user records and metadata-only rejection must remain unchanged.

### Task 1: Strict mid-turn proof and queue finalization

Files: create internal/claudeinstance/confirmation_midturn.go and confirmation_midturn_test.go; modify confirmation.go; add internal/queue/claude_midturn_test.go. Existing FindProof signature and Proof physical-record identity stay unchanged.

- [x] Add synthetic parser table tests: valid removal + rendered attachment yields attachment UUID/offsets; missing/mismatched identity, envelope, command/delivery/wire/address, wrapper, incomplete record, duplicate and pre-boundary evidence reject.
- [x] Add queue integration tests: accepted becomes delivered with durable consumed attachment proof exactly once, ordinary proof still works; foreign/incomplete proof remains accepted; failed/deadline rows cannot resurrect; sends remain one.
- [x] Run focused tests and observe valid mid-turn cases fail on current parser.
- [x] Implement a private matcher retaining matching post-boundary removal IDs and requiring the observed complete attachment shape and rendered wrapper. Count its match alongside ordinary user records without changing finalization/store semantics.
- [x] Run focused tests GREEN and commit the coherent fix.
- [x] Run claudeinstance/store/queue suites, race checks and repository Go tests/vet. Review the diff for scope and proof weakening.
- [x] Record evidence/recovery limits; deliver one PR for creative-work with URL and verification to orchestrator.
