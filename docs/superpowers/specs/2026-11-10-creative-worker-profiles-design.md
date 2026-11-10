# Creative worker profile allowlist

Task #6060, feature creative-improvement. Add worker model profile selection at orchestrator launch and editing on an existing run. Empty means “all profiles”. Backend and CLI are outside this task.

Use the existing session rail and registry query. A shared native checkbox group shows profile name and model. Launch starts unchecked and includes enabled profiles. The existing-run editor starts from the saved list and includes enabled profiles plus saved disabled profiles. Save replaces the full list; errors keep the draft. Pending requests lock controls. Saved summaries follow refreshed run data; cancel discards edits. Match existing Rocket tokens and buttons.

Approaches: a separate settings page would add navigation for one setting; duplicating checkbox markup would risk divergent behavior. A small shared group in the session rail fits the existing launch form and keeps the change local. This implementation choice is the worker's; the orchestrator accepted the design summary.

Contract: Run.allowed_profiles is string[]; POST /v1/creative/runs/{run}/sessions accepts allowed_profiles with profile; PUT /v1/creative/runs/{run}/models requires {allowed_profiles: string[]} and returns that object. Empty arrays clear restrictions; disabled registered names are allowed. Standard API errors display in place.

Tests cover payloads, successful rendered summary, disabled entries, empty clearing, launch/save error retention and registry loading failure. Update docs/creative-dashboard.md. Backend merge and rebase onto main precede final real-contract verification.
