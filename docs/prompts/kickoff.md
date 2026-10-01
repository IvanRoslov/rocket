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
research and write the spec. You do not end that phase yourself — the human's
Go on the spec gate (step 4) does.

Start now:

1. PROBLEM FIRST. Before any question, store the problem in plain words —
   what hurts, for whom, what success looks like:
   `rocket task doc put {{task_id}} --kind problem --title "Проблема" --file <problem.md>`
   (in the human's language; {{task_description}} above is a good hint).

2. CLARIFY. Then invoke {{brainstorm_skill}} and drive it with the human.
   You may talk it through right here in the terminal, but every fork that
   needs the human's decision is ALSO filed as a storm question —
   one decision per question, never several bundled into one:
   `rocket task ask {{task_id}} --brainstorm --recommend <N> --title "<decision>"
   --brief "<plain-language brief>" --option "<A>" --option "<B>"`
   (`--recommend` is the number of the option you recommend).
   If the human answers in the terminal instead of the dashboard, record it
   at once, their words verbatim:
   `rocket task brainstorm record {{task_id}}/Q<n> [--choose <N>] "<the human's words>"`

3. RESEARCH. Explore the relevant repos ({{allowed_repos}}) from your worktree
   to understand the current state. Record findings worth keeping.

4. SPEC, PLAN, GATE. Finish the brainstorm into a spec; invoke
   superpowers:writing-plans for the decomposition plan. Store both in the
   task (task doc put --kind spec / --kind plan), then request the gate:
   `rocket task gate request {{task_id}}`
   The spec starts with a short plain-language summary in the human's
   language — what gets built, what is deliberately left out — written for
   someone who has not read the storm; the human reads it at the gate before
   pressing Go.
   A chat "yes" about the design is NOT a Go. ANY later edit to the spec —
   including rationale-only edits — means: store the new version and
   `rocket task gate request {{task_id}}` again (the pending gate on the old
   version is superseded automatically).
   Do not spawn workers and do not move the task yourself. On Go the gate
   moves the task to in_progress and you receive
   "[rocket gate] Go …" — that is your signal to start executing. On
   "[rocket gate] Нужны правки …" revise the spec, store it, request again.

5. EXECUTE. Create subtasks, spawn workers, coordinate to merged PRs.
   Gates: a worker's PR needs green CI before you consider its task done.

6. DELIVER. Final report, task to review, tell the human.

Do not skip the problem doc in step 1 and the gate in step 4.
```
