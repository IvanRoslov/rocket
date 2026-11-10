# Creative feed core: isolated orchestrator validation

Date: 2026-11-10. Rocket task 6062, parent 6061, feature creative-messages.

## Environment and method

Used a separately built binary, independent Rocket home/database/socket and a
synthetic local Git repository/project. Launched a real `codex` creative
orchestrator (GPT-6.1-Sol default) through the Creative sessions API with the
standard generated launch context, and a real worker through the worker API.
The run brief requested a short noir scene and an image plan, explicitly
prohibiting provider execution and paid calls. Human actions below used HTTP;
agent replies, proposals, publication and submission used the documented CLI.
No feed-operation coaching was added to their assignments.

Fixture-only setup isolated Codex trust/config/auth/journals and ensured login
shells selected the test binary. Initial trust/PATH retries preceded the recorded
scenarios. Only this isolated daemon was stopped/restarted; the operator's daemon
and installed binary were not changed. The final rebuilt binary was used for the
card button, formal approval and offline restore checks.

Run 1: Script stage 1, Frames stage 2, Scene 3 item 1.
Orchestrator: `creative-1-orch-2`; worker: `creative-1-feed-scope-check`.

## Observed transcript summary

| Scenario/action | Observed result |
| --- | --- |
| Human entry 1 in Script: “Make the ending darker … Please publish the revised short scene.” | Orchestrator replied in Script (entries 2/3), published scene version 1, submitted it for review and posted entry 6 with pinned version `[1]`. |
| Human entry 4 in Frames/Scene 3: hypothetical four-image plan, MockImage/noir-v1, four calls at $0.10, written prompts only, no uploads/execution | Orchestrator published plan version 2 and proposal card 7 with four complete prompts, call count, model, inputs/services and $0.40 estimate/basis; card was pending. |
| Human entry 9: “Make it 2 images: keep the evidence reveal and the final blackout.” | Orchestrator published immutable version 3 and new card 13 with `--replaces 7`; card 7 became `replaced`, linked to 13, and card 13 linked back to 7. New cost: two calls, $0.20. |
| Worker tries human feed and new question API under its real session identity | Feed returned 403; question creation returned 409. Both gave `rocket creative agent send 1 --stage 2 --item 1 --body 'Your message'`. |
| Standard-context worker asks a creative preference | Worker used contextual `send` with version 3 to ask its orchestrator about readable silhouettes versus darkness. Orchestrator relayed the question to the human in Frames/Scene 3 entry 15, with version `[3]`. |
| Human Script entry 10: “Looks good, approve the Script.” | Stage remained `awaiting_approval`. Orchestrator entry 11 said to click Approve on the Script review card and explained that feed text does not change state. |
| Human Frames entry 16: “Go. Keep the barely visible silhouettes already in version 3.” | Card 13 remained `pending`. Entry 17 explicitly said typed Go is discussion, only the proposal-card button records its decision, and stage acceptance uses its separate review card. |
| Human tries Go on replaced card 7 | HTTP 409 returned the current replaced entry and link to card 13. |
| Human clicks Go on card 13 | Card became `decided`, decision `go`, timestamp saved. A second decision (Deny) returned 409 with the existing Go. Orchestrator entry 20 acknowledged Go for the two-image plan specifically. No provider calls occurred. |
| Human formally approves Script via existing review action | Script became `completed`. Human entry 18 after approval succeeded and left it completed. Orchestrator entry 19 confirmed preservation without reopening. |
| Unread before opening/mark-read | `total:11`, run feed 0, Script 4, Frames 7. Marking Script through 11 and Frames through 16 returned total 0. Subsequent new orchestrator replies became unread again. |
| Stop orchestrator; human posts run entry 21 | Entry immediately appeared in the run feed; durable outbox row existed with `message_id:null`. |
| Restore same real orchestrator with standard context | Outbox acquired transport message 11; restored agent read feeds/artifacts and posted run entry 22 acknowledging recovery. The delivered event included entry 21 and `rocket creative agent feed post 1 --body 'Your reply'`. History and decisions remained intact; entries 23/24 confirmed preserved versions 1/2/3, distinguished paid Go from the still-pending Frames stage review, and confirmed no calls/uploads/spending. |
| Legacy question list | GET questions returned `[]`: no new question was opened during the scenarios. |

Selected agent commands (message bodies abbreviated):

```sh
rocket creative agent feed post 1 --stage 1 --version 1 --body 'Published ...'
rocket creative agent feed propose 1 --stage 2 --item 1 --version 2 --file creative-results/scene-3-image-plan.md
rocket creative agent feed propose 1 --stage 2 --item 1 --version 3 --replaces 7 --file creative-results/scene-3-image-plan.md
rocket creative agent send 1 --stage 2 --item 1 --version 3 --body 'Reviewed ...'
rocket creative agent feed card 1 13
rocket creative agent feed post 1 --body 'I have recovered the feeds and current review state. ...'
```

The source-stage version could not be attached to an item in a different stage;
the API refused it and the agent corrected the attachment to the item plan's
version, retaining the script reference in the proposal body. Scope enforcement
and immutable version validation remained active during the real-agent check.

## Automated evidence

- Final `make test`: all Go packages passed; development web 62 files / 722 tests;
  Creative web 8 files / 65 tests.
- `go test -race ./internal/store -run CreativeFeed -count=1`: passed.
- `go vet ./internal/store ./internal/api ./internal/cli ./internal/session ./internal/prompts`: passed.
- `go build -o .rocket/feed-test-rocket.next ./cmd/rocket`: passed.
- Boundary regressions cover item filtering, version pinning, foreign/worker/stale
  writer refusal, invalid scope/version status, atomic decision/replacement races,
  one-time decisions, unread monotonicity, archived replies/answers, delivery
  context/commands and launch/restore memo and offline event recovery.

The first broad run hit an unrelated Claude hook fixture process timeout; its
standalone rerun passed. A second attempt found absent npm test dependencies;
installing lockfile dependencies resolved that. No unrelated source changes were
made; subsequent full runs passed.

One case outside this deliverable: item-tagged immutable feed entries constrain
physical deletion of referenced pending items, as older discussion/session
references already do. This work does not redesign item deletion.
