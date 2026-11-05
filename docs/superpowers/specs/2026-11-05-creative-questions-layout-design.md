Design for gate297 specv9/planv8 (no departures):
- Restore three columns: stage rail left, flexible central tab workspace, permanent 312px session rail right, outside tab panels.
- Move existing Creative SessionCard/start/profile/restore controls into that rail; preserve access links and omit development kill/PR controls.
- Keep questions central; sort open questions newest first and resolved questions newest first below, collapsed individually with expandable complete history.
- Reuse Rocket QuestionThreadView/Markdown and its author, reply, option and answer styles; add narrow presentation hooks for read-only history and retained drafts, with existing consumers unchanged.
- Clarify uses existing /v1/questions/:id/reply; explicit answer/choice uses /answer. Existing Creative route handles permissions, history and delivery; no backend work needed.
- Preserve discussion drafts with frozen run/stage/item/version context, thread drafts across navigation, selected versions and review payloads. Leave artifact/preview contract intact.
- TDD regression coverage for rail persistence across tabs, ordering/collapse/full history, clarification vs answer, failure/draft retention and existing contextual review flows; run Creative and relevant shared UI suites/builds. No live changes or paid calls.
