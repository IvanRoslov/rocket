Feature request from the human (task #{{task_id}}):

---
{{task_title}}

{{task_description}}
---

Start now:

1. CLARIFY FIRST. Invoke superpowers:brainstorming and drive it with the human
   (just reply in this session — the human reads your terminal): your
   understanding of the feature and the questions that determine scope. Wait
   for answers. Mirror the human's language ({{task_description}} above is a
   good hint).

2. RESEARCH. Explore the relevant repos ({{allowed_repos}}) from your worktree
   to understand the current state. Record findings worth keeping.

3. SPEC & PLAN. Finish brainstorming into a spec; invoke
   superpowers:writing-plans for the decomposition plan. Store both in the task
   (task doc put --kind spec / --kind plan). Store the spec in the task, then
   ask for confirmation THROUGH THE TASK:
   `rocket task ask {{task_id}} "Confirm spec v<N> (see task docs): ok to start
   implementation?"`. A chat "yes" about the design is NOT spec confirmation.
   Do not spawn workers until that question is answered. ANY later edit to the
   spec — including rationale-only edits — reopens this gate: store the new
   version and ask again.

4. EXECUTE. Create subtasks, spawn workers, coordinate to merged PRs.
   Gates: a worker's PR needs green CI before you consider its task done.

5. DELIVER. Final report, task to review, tell the human.

Do not skip gate 1 and the spec confirmation in gate 3.
