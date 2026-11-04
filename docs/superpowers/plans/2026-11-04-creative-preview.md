# Creative Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Register and access interactive creative landing previews from a remote Rocket browser without sharing Rocket's privileged origin.
**Architecture:** A dedicated preview-only listener proxies a capability-prefixed, base-path-aware loopback dev server. Authenticated Rocket routes resolve core creative context and expose current availability; a separate CLI command supports agents without owning their launch runtime.
**Tech Stack:** Go net/http, httputil.ReverseProxy, existing Cobra/SQLite creative context.
**Spec:** docs/superpowers/specs/2026-11-04-creative-preview-design.md

## Global Constraints
- Work only in Rocket; no public deployment, demo projects or live daemon restart.
- Explicit separate reachable preview hostname; prefix-aware dev servers only.
- No arbitrary target URLs or privileged credential forwarding.
- Core owns context; agents own launch; dashboard owns rendering.

## Review Focus
- Missing/unreachable preview installation must return actionable unavailable status.
- Cross-context or cross-project registrations must fail closed.
- Rocket cookies, authorization and agent headers must never reach upstream.
- Stopped upstream, bad capability and revocation must never appear ready.
- Assets and WebSocket traffic must retain the registered prefix and stay bound to the registered loopback port.

### Task 1: Preview registry and isolated proxy
**Files:** Create internal/preview/preview.go, internal/preview/preview_test.go.
**Interfaces:** New(Config) *Registry; Register(Context, port) (Access,error); List(Context) []Access; Revoke(id); ServeHTTP. Context carries run/stage/item IDs. Config supplies origin, ports and excluded ports.
- [x] Write failing tests for prelaunch registration, context lookup, allowlist, credentials, assets, WS, upstream failures and revocation.
- [x] Run go test ./internal/preview; observe intended failures.
- [x] Implement memory registry with crypto-random capabilities, bounded probes, fixed loopback proxy target and security headers. Registrations expire on daemon restart deliberately: never resurrect a stale port capability.
- [x] Run package tests with race detector and commit.

### Task 2: Installation and listener lifecycle
**Files:** Modify internal/config/config.go, internal/config/config_test.go, internal/api/server.go. Create internal/api/creative_preview_listener_test.go.
**Interfaces:** Config PreviewHost, PreviewPort, PreviewPublicURL, PreviewPorts; Deps Previews *preview.Registry; shared registry between management API and dedicated listener.
- [x] Test invalid origins/configuration, same-host reuse, unavailable configuration and listener separation before implementation.
- [x] Implement config validation and optional listener lifecycle; listener failures mark unavailable without changing Rocket access.
- [x] Run focused config/API tests and commit.

### Task 3: Creative API/CLI integration and rollout documentation
**Files:** Create internal/api/creative_previews.go, internal/api/creative_previews_test.go, internal/cli/creative_preview.go, internal/cli/creative_preview_test.go, docs/creative-previews.md; modify route/CLI registration.
**Interfaces:** POST/GET /v1/creative/previews, DELETE /v1/creative/previews/{id}; rocket creative-preview register/list/revoke. Core ResolveCreativeContext and same-project session authorization.
- [x] Write failing tests for context validation, authenticated access, same-project callers, prelaunch base path and unavailable upstream.
- [x] Implement context routes and CLI, consuming core implementation commit once available.
- [x] Document setup, origin separation, base path, lifecycle and limitations; send exact contracts to agents/dashboard.
- [x] Run focused tests, go vet and full Go suite; inspect diff, request independent review, fix acceptance failures and open one PR.
