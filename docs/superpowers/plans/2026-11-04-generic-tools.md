# Generic Tools Correction Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Preserve the current user's five generation clients locally and remove their distribution from Rocket, leaving a generic user-owned catalog.

**Architecture:** Perform the approved one-time transfer using the pre-removal installer only after checking destination metadata. Delete the embedded bundle and installation path; leave registry APIs, generic commands, discovery, and payment rules intact.

**Tech Stack:** Go, Cobra, YAML; existing local Node clients.

**Spec:** creative-work 5986 spec v3 / plan v2, gate 292; task 5992 brief delivered in `.rocket/inbox/msg-81057.md` and accepted design in message 81060.

## Global Constraints

- Only this worker branch and one PR; no other repository modifications.
- One-time approved writes to the actual user's Rocket tools directory only; preserve existing registrations, custom files, and authorization.
- No paid/client calls, secrets access, authentication, live binary overwrite, or daemon restart.
- No replacement installer, automatic installation, credentials management, model catalog, or new payment gate.
- Focused checks and isolated-home CLI smoke checks suffice; no broad suite or additional review cycle.

## Review Focus

- Destination already populated: recheck before transfer; do not overwrite.
- Fresh home: list returns an empty array and creates no files.
- Arbitrary local CLI: registration retains argument boundaries and metadata.
- Duplicate name: refuse implicit replacement; explicit update remains supported.
- Removed command: fail as unknown before modifying the supplied home.

### Task 1: Preserve user-owned clients

**Files:** Actual Rocket home `/Users/ivanroslov/.rocket/tools` (authorized one-time transfer).

**Interfaces:** Existing pre-removal CLI and embedded distribution; resulting registry consumed unchanged by generic Load/Register and discovery.

- [x] Build pre-removal `./cmd/rocket` into temporary scratch, never the live binary.
- [x] Recheck actual home, tools directory, and registry absence immediately before invoking old installer once without update; if populated, inspect metadata and preserve or report conflicts instead.
- [x] Verify five names (replicate, laozhang, kie, reapi, byteplus), absolute command file existence, and copied distribution integrity without client execution or credential reads.
- [x] Record destination and preservation outcome with `rocket task log`.

### Task 2: Remove distribution and verify generic catalog

**Files:** Modify `internal/cli/tools.go`, `internal/toolcatalog/catalog_test.go`, `docs/creative-tools.md`, `docs/superpowers/plans/2026-11-04-creative-agents.md`, `docs/superpowers/specs/2026-11-04-creative-agents-design.md`; create `internal/cli/tools_test.go`; delete `internal/toolcatalog/install.go` and `internal/toolcatalog/bundle/`.

**Interfaces:** Keep `Load(home string) ([]Entry, error)`, `Register(home string, entry Entry, replace bool) error`, and list/show/register CLI behavior unchanged. Creative discovery continues consuming these commands.

- [x] Write CLI tests using isolated homes: fresh list returns `[]` without creating tools; register/show/list retain a generic renderer entry with spaced arguments; duplicate registration fails without mutation and explicit update succeeds; removed install invocation errors without creating files.
- [x] Run `go test ./internal/cli -run TestTools -count=1`; expect removed-command test to fail because old installer is still present.
- [x] Delete distribution and installer; remove only installer-specific tests and Cobra branch; describe command as a user-owned CLI catalog.
- [x] Rewrite tools operator docs for an initially empty registry, local ownership and generic registration; remove obsolete installation guidance in the previous implementation plan. Retain payment Go and discovery instructions.
- [x] Run `go test ./internal/toolcatalog ./internal/cli ./internal/prompts -count=1`, focused vet, and temporary binary build. Expect success.
- [x] Exercise list/register/show/duplicate/update and removed command through the built binary with isolated home, then verify the actual transferred catalog and files are unchanged. Do not execute clients.
- [x] Inspect diff and search for remaining distribution/install references; commit focused code/tests and documentation changes.
- [ ] Push worker branch, create one PR referencing creative-work 5986 / task 5992, and send orchestrator PR URL, checks and exact transfer result.
