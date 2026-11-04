# Creative shared Rocket UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Align Creative with the actual shared Rocket style while preserving all behavior.

**Architecture:** Direct sibling source imports of tokens, base CSS and UI primitives. Extract presentation-only RocketBrand from the development header and reuse it in Creative; retain Creative layout CSS using shared tokens.

**Tech Stack:** React, TypeScript, Vite, existing Vitest tests.

**Spec:** docs/superpowers/specs/2026-11-04-creative-style-design.md (accepted by orchestrator).

## Global Constraints

- Preserve both applications' behavior and separate Creative entry/build.
- No dependency upgrades, CSS test suite, demo project, paid calls, daemon restart or installed binary replacement.
- Development branding remains visually unchanged; no development navigation imported into Creative.
- Use existing affected checks and both builds, as explicitly authorized.

## Review Focus

- Buttons inside forms must preserve native submit behavior (shared Button defaults to button).
- Selection and stage/item contexts, review versions and discussion draft bindings remain unchanged.
- Shared imports must resolve one React runtime in Creative.
- Shared component styles must remain effective, without Creative's old global control overrides.
- Main header branding and navigation behavior must remain unchanged.

### Task 1: Share actual Rocket presentation

**Files:** `web/src/components/RocketBrand.tsx`, `web/src/components/AppShell.tsx`, Creative `src/*.tsx`, `src/style.css`, `vite.config.ts`, `docs/creative-dashboard.md`.

**Interfaces:** Consume existing Button, Badge, Card, Dot, Tabs, EmptyState APIs; RocketBrand provides presentation-only existing logo markup. Keep Creative callbacks, attributes and API calls.

- [ ] Run existing Creative tests and affected web UI/AppShell tests as baseline.
- [ ] Extract current logo markup unchanged; use it in both headers.
- [ ] Import shared tokens/base and primitives; deduplicate React in Creative Vite.
- [ ] Replace common controls/status/empty/cards/tabs with actual shared primitives, preserving button types and accessibility labels.
- [ ] Replace independent CSS base and controls with token-based Creative layout styles; compact light header and restrained accent.
- [ ] Document source sharing and run Creative tests, affected web tests and both builds; verify no behavior changes in diff.
- [ ] Commit cohesive implementation and documentation, then open focused PR for creative-work.
