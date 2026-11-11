# Creative chat theme implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this single task inline.

**Goal:** Keep creative chat on the surrounding light dashboard palette under dark OS preferences.

**Architecture:** Remove discussion-only theme overrides so chat inherits shared dashboard tokens. Extend the existing mocked Playwright acceptance script to compare rendered surfaces with the dashboard under light/dark OS preferences and unset/light/dark theme attributes.

**Tech Stack:** CSS, React, Playwright browser check, Go and Vitest suites.

**Spec:** Orchestrator brief for creative-messages task 6061 / worker 6069.

## Global constraints

- Dashboard remains light-only; no new dark theme.
- Remove obsolete dark screenshots and documentation claims.
- Run make test and make creative-web-build before PR to main.

## Review focus

- Default unset theme on dark OS must stay light (primary regression).
- Explicit dark attribute must not darken chat alone.
- Explicit light attribute and light OS must continue matching dashboard.
- Stage, item and run conversations at desktop/mobile widths retain their shared surfaces.
- Existing polling, scroll anchoring, pending navigation and keyboard sending checks remain intact.

### Task 1: Inherit dashboard palette

**Files:** creative-web/src/style.css; creative-web/browser/chat-window-check.mjs; docs/screenshots/creative-feed/README.md; obsolete dark PNGs in that directory.

**Interface:** captureChatWindow(page, phase) remains the mocked browser acceptance entry point.

- [x] Add rendered-surface assertions under emulated light/dark color schemes for unset/light/dark theme attributes. Compare discussion/composer to topbar surface and history to body surface; pin light dashboard baseline.
- [x] Run browser check before CSS change; expect dark OS with unset theme to fail on a dark discussion background.
- [x] Remove the two chat-only dark override blocks.
- [x] Run browser check; expect all theme/scope/width combinations and existing interaction checks to pass. Capture only light-theme screenshots.
- [x] Remove obsolete dark screenshots and README rows; document system preference regression coverage.
- [x] Run make test, make creative-web-build, git diff --check; review diff, commit and open PR to main referencing creative-messages task 6061.
