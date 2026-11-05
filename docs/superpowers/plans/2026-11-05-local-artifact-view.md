# Local artifact viewing implementation plan

> **For agentic workers:** Use superpowers:executing-plans with focused TDD.

**Goal:** Gate #297 / creative-work spec v9 plan v8: allow the verified same-machine HTML viewing configuration without DNS or certificates.
**Architecture:** Add a narrowly qualified exception inside Config.ValidatePreview; retain existing listener, HTML serving and request-host checks.
**Tech stack:** Go config validation and API regression tests.

## Design and bounds

Only http://artifact-preview.localhost:<preview_port> qualifies, with a literal loopback preview_host and matching explicit URL port. Rocket's configured listen/public hostname and actual requesting hostname must remain distinct. All other validation restrictions remain. No live config edits/restart or changes to HTML isolation/access behavior. Compared with broad localhost or HTTP exemptions, this exact-host exception matches the approved browser evidence and avoids relaxing nonlocal setup.

## Tasks

- [x] Add Load table tests accepting default/explicit loopback binds and rejecting wildcard/LAN binds, missing/wrong URL port, other aliases, shared Rocket hostname, and malformed origins; run focused config tests RED.
- [x] Implement exact-host/HTTP/loopback/port qualification before existing localhost and HTTPS restrictions; run configuration suite GREEN.
- [x] Add API regression exercising validated local configuration, separate-host redirect/content and same-request-host denial; run artifact/preview tests.
- [x] Document exact same-machine config and human restart, preserve public_url and explain remote browsers unsupported.
- [x] Run focused race/full relevant suites and synthetic Chrome through validated local config; fresh review; open separate PR based on merged main.

## Review focus

Local allowance must not permit nonlocal binding, other localhost aliases, mismatched ports, shared configured/request Rocket hostname, or weaken pre-existing nonlocal validation. No changes to capability or sandbox policy.

## Ledger

Design confirmed to orchestrator; gate #297 explicitly authorizes implementation. Existing worktree reused on a new branch from merged main, per orchestrator instruction.

Validation: initial focused config RED rejected all three intended local cases; GREEN after narrow exception. Config/API/preview suites and targeted race checks pass. Full go test ./... passes before review fix; final rerun recorded in delivery log.
Synthetic Chrome with ValidatePreview enabled passed HTTPS-to-local new tab, selected HTML rendering, visible wrapper, denied storage/cookies/parent/network and null opener. Fixture uses loopback temporary listeners and temporary data only. IPv6-only and other-engine connectivity not claimed.
Fresh review found no security regression; one exact-origin acceptance defect allowed bracketed DNS host syntax that browsers reject. Ruling: fix before delivery because accepted local configuration must be browser-usable, despite review labeling it minor. Added bracketed-host rejection test (RED), compare complete hostname:port for the exception (GREEN). Nonlocal validation remains unchanged.
