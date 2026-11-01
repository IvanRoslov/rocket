---
name: orchestrator-planning
description: "Use when you are a feature ORCHESTRATOR and have an approved (or about-to-be-gated) spec: decompose it into deliverables for workers — who builds what, in what order, and how each piece is accepted. Not an implementation plan; workers write those."
---

# Planning a Feature as Its Orchestrator

You have a spec. Your plan turns it into work for a team of workers: a
handful of deliverables, each owned by one worker, each landing as one PR,
each with a clear way to accept it. It is the document the human reads at
the gate next to the spec, and the source of every worker brief.

It is NOT an implementation plan. Each worker brainstorms and writes its own
implementation plan — files, signatures, test cases, steps of a few minutes
each — inside its worktree, against your brief. If your plan already holds
that detail, the worker either repeats it or argues with it, and you end up
reviewing code in a planning document.

## Altitude

**In the plan:**

- the deliverables: what each one changes for the product, in one or two
  sentences;
- where each one lands: the repo, and the component or team it belongs to;
- dependencies and order: which deliverable needs which, what can run in
  parallel;
- the contracts between deliverables, as promises: "the store reports one
  stop per container restart, with its cause when known" — not the type,
  table or function that will carry it;
- acceptance per deliverable: what you will check before it counts as done
  (behaviour, a scenario, a measurable limit);
- the feature-level verification: how, once everything is merged, we show
  the original problem is solved;
- risks and the rollout boundary: what is off by default, what needs a
  separate approval (staging resources, production, publishing);
- what is deliberately left to workers.

**Not in the plan:** file paths, function or type names, signatures, table
and column layouts, SQL, lock order, test names, step-by-step checklists.
When you are tempted to write one, write the requirement it serves into
the deliverable's acceptance instead.

Reading code while planning is fine — to learn which repo and component
owns something, or whether a capability already exists. Bring back the
fact, not the code.

**Scope of a deliverable is a closed list.** Name what it delivers:
the screens, the data, the actions, the states. Words like "whatever is
needed", "if necessary", "within the scope of the page" without a list
let a worker grow the deliverable on its own. If you cannot list it yet,
that is a storm question, not a plan line. When a worker's design needs
something not on the list, it comes back to you, and a product-visible
addition goes to your brainstorm partner.

## Shape

1. **Mitigation first.** If a small change stops the harm the human
   reported, it is deliverable 1, with no dependency on anything else. A
   fix the customer is waiting for never waits behind infrastructure you
   are still building.
2. **Few, independent deliverables.** Prefer three to six. Each one fits
   one worker, one branch, one PR, and is useful or at least safely
   shippable on its own. If a deliverable needs a long paragraph to
   describe, split it; if two always change together, merge them.
3. **Thin dependency chains.** A long chain (1 → 2 → 3 → 4) means
   nothing ships until the last link; look for a cut that lets the early
   links ship and be used.
4. **Defer what the problem does not need.** Improvements found while
   planning become follow-up tasks you propose to the human, not extra
   deliverables.

## Format

```markdown
# <Feature> — Plan

**Spec:** <task doc version>. **Goal:** <one sentence, the problem solved>.

## Deliverables

| # | Deliverable | Repo / owner | Depends on | Accepted when |
|---|---|---|---|---|
| 1 | <what changes, in product terms> | <repo> | — | <observable check> |
| 2 | ... | ... | 1 | ... |

## Contracts between deliverables
- <promise one deliverable makes to another, in plain words>

## Order and parallelism
<what starts first, what runs in parallel, what waits>

## Verification of the whole feature
<the scenario that proves the original problem is gone>

## Rollout and approvals
<default-off parts, what needs a separate Go: resources, production>

## Left to workers
<the decisions you are deliberately not making>
```

A plan in this shape is usually one to two screens. If yours is much
longer, it has drifted into implementation detail or the scope has grown.

## Self-review

Before storing the plan, check:

1. **Coverage:** every requirement of the spec lands in some deliverable's
   acceptance.
2. **Altitude:** no file paths, signatures, schemas or step lists.
3. **Closed scope:** every deliverable lists what it delivers; no
   "whatever is needed" without a list.
4. **Mitigation:** if one exists, it is deliverable 1 and independent.
5. **Each deliverable can be briefed alone:** a worker who reads only its
   row, the contracts and the spec knows what to build and how it will be
   accepted.

Fix issues inline, store the plan (`rocket task doc put <task> --kind
plan`), and request the gate together with the spec.

## From plan to worker briefs

Each worker brief is one deliverable expanded: goal, the relevant part of
the spec, the contracts it must honour or provide, constraints, acceptance,
and how to verify. Tell the worker to brainstorm its own design and write
its own implementation plan; you accept that plan the way cto or the
human accepts yours — does it deliver the brief and meet the acceptance —
not line by line.
