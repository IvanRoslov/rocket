# Artifact View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open selected immutable HTML artifacts in an isolated separate browser tab.
**Architecture:** Authenticated API issues a five-minute random capability. Existing preview listener serves a trusted explanatory wrapper and sandboxed immutable HTML; media/download paths stay intact.
**Tech Stack:** Go net/http, React, Vitest.
**Spec:** docs/superpowers/specs/2026-11-05-artifact-view-design.md

## Global Constraints

- Work only in this worktree; no live restart, real creative mutations, provider calls or secrets access.
- Preserve Artifacts props/onShown and existing media/download/version/review behavior.
- Reuse configured preview origin; never serve active HTML on privileged origin.

## Review Focus

- Device revocation after a link is issued must stop new accesses (task 1).
- Alternate Rocket request hostname must not become the preview hostname (task 1).
- Missing or replaced blob must produce unavailable instead of blank content (task 1).
- Failed media must retain download and useful error (task 2).
- Selected retained/review version must determine link, independent of current version (task 2).

### Task 1: Isolated version access

Files: create internal/api/creative_artifact_views.go and _test.go; modify server.go, creative_preview_listener.go, creative_artifacts.go.
Interfaces: GET /v1/creative/artifact-versions/{version}/view issues redirect; preview-only /a/{token}/ and /a/{token}/content serve wrapper/file. Deps shares *artifactViewRegistry between listeners.
- [x] Write API tests for authenticated redirect, immutable version bytes, sandbox/no-store/referrer headers, no privileges, download unchanged, missing setup/blob, wrong host/path/method, expiry and revocation.
- [x] Run go test ./internal/api -run TestCreativeArtifactView -count=1; observe missing-route failure.
- [x] Implement bounded registry (256 entries, five-minute expiry), authenticated issuer and separate listener handler with strict sandbox and trusted wrapper.
- [x] Run targeted API tests plus existing preview/artifact tests; expect PASS.
- [x] Commit server feature and tests.

### Task 2: Selected-version links and errors

Files: modify creative-web/src/Artifacts.tsx, Artifacts.test.tsx; docs/creative-artifacts-api.md.
Interfaces: Media derives authenticated /view path from immutable version ID for text/html; link target=_blank rel=noopener noreferrer; no change to Artifacts props.
- [x] Write tests for submitted/selected HTML version links and downloads, unsupported notice, media error/download and existing image/video/audio elements.
- [x] Run npm test -- Artifacts.test.tsx; observe missing link/notice failure.
- [x] Add HTML link and visible self-contained/setup explanation, unsupported-format notice; retain media branches.
- [x] Run creative-web full tests/build and Go relevant packages; expect PASS.
- [x] Document setup/access/HTML limits; commit frontend and documentation.

### Task 3: Verification and delivery

- [x] Exercise synthetic browser fixture for sandbox/storage/opener/network and visible wrapper; record limits.
- [x] Review full diff using fresh reviewer, fix acceptance/security findings with regression tests.
- [ ] Run final targeted Go checks, creative-web tests/build, git diff --check.
- [ ] Push feature/creative-work/artifact-view, create one PR referencing creative-work; notify orchestrator with URL and constraints.

## Execution ledger

Design accepted; native execution per worker instructions. Existing isolated worktree verified on feature/creative-work/artifact-view, initially clean and matching fetched origin/main.

Task 1 verification: targeted artifact/preview Go tests pass. Initial RED missing view handler; fixture corrections used stored (non-JSON) storage key and existing pairing helper hostname. No production scope deviation.

Task 2 complete: frontend RED missing HTML link/unsupported notice, GREEN 25/25 tests; production build passes after installing existing shared web dependencies. Generated dist placeholder restored.
Task 3 browser verification: synthetic Chrome HTTPS-to-local-preview new-tab path rendered selected HTML; inline script ran, cookie/storage/parent/opener/network isolation passed; wrapper visually inspected. Local configuration validation is unchanged pending orchestrator gate.
Final review: fresh security reviewer found one Important acceptance failure: same-size replacement bypassed size check. Regression reproduced HTTP 200 for replaced bytes; digest verification now rejects mismatched SHA256 on issuance and existing capabilities. No other actionable findings.
Ruling: live proxy/DNS availability and real-case evaluation remain operator-owned; no restart/config changes performed. Browser evidence is Chrome-only; other engines not claimed. Pending local-mode delta is outside this completed implementation until separately approved.
