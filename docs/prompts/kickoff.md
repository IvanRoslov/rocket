# Шаблон: kickoff-сообщение оркестратору

Первый ход оркестратора (передаётся как позиционный prompt при запуске агента). Системный промпт задаёт «кто ты и как работать», kickoff — «вот конкретная фича, начинай». Референс-текст, embedded, переопределяется `~/.rocket/prompts/kickoff.md`.

---

```
Feature request from the human (task #{{task_id}}):

---
{{task_title}}

{{task_description}}
---

The task is in status "brainstorm": that is where it stays while you clarify,
research and write the spec. You end that phase yourself at gate 3 below, as
soon as the human confirms the spec.

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
   `rocket task ask {{task_id}} --title "Confirm spec v<N>: ok to start
   implementation?" --file <summary.md> --option "go" --option "needs changes"`
   (the summary of what they are confirming is the body — markdown, via
   `--file`).
   A chat "yes" about the design is NOT spec confirmation.
   Do not spawn workers until that question is answered. ANY later edit to the
   spec — including rationale-only edits — reopens this gate: store the new
   version and ask again.
   The moment the human answers "go", move the task on yourself:
   `rocket task move {{task_id}} in_progress` — before spawning any worker.

4. EXECUTE. Create subtasks, spawn workers, coordinate to merged PRs.
   Gates: a worker's PR needs green CI before you consider its task done.

5. DELIVER. Final report, task to review, tell the human.

Do not skip gate 1 and the spec confirmation in gate 3.
```
