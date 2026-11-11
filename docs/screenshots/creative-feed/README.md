# Creative chat window visual checks

Tasks 6066 and 6069 for creative-messages (task 6061, spec v8). Fixtures use an item with twelve displayed artifact versions and twelve references on its messages. All HTTP requests are mocked; screenshots do not change a running daemon. The dashboard is light-only, and the chat inherits its light surfaces regardless of OS color scheme or host theme attribute.

| Scope | Theme | Width (before → after) | Before | After |
| --- | --- | --- | --- | --- |
| stage | light | 1600 → 1440 | [Before](before-stage-light.png) | [After](after-stage-light.png) |
| stage | light | 390 | [Before](before-stage-light-narrow.png) | [After](after-stage-light-narrow.png) |
| item | light | 1600 → 1440 | [Before](before-item-light.png) | [After](after-item-light.png) |
| item | light | 390 | [Before](before-item-light-narrow.png) | [After](after-item-light-narrow.png) |
| run | light | 1600 → 1440 | [Before](before-run-light.png) | [After](after-run-light.png) |
| run | light | 390 | [Before](before-run-light-narrow.png) | [After](after-run-light-narrow.png) |

Stage layout at 1440×900 and 390×844:

| Review | Desktop | Narrow |
| --- | --- | --- |
| Awaiting decision | [Pending](stage-review-pending-light.png) | [Pending narrow](stage-review-pending-light-narrow.png) |
| In progress | [Idle](stage-review-idle-light.png) | [Idle narrow](stage-review-idle-light-narrow.png) |

The desktop captures show the review above Sessions beside a chat filling the remaining viewport. Narrow captures show review above chat. The fixture includes a long code block to check wrapping. The browser check asserts no desktop page scrolling, visible review placement/highlighting, bounded input growth preserving both the latest and older visible messages, and archive navigation preserving the draft.

Additional states: [new-message pill](after-new-message-pill.png), [pending proposal](after-pending-proposal.png).

The browser check emulates both dark and light OS color schemes, with unset, dark, and light host theme attributes, at both widths and in all three scopes. It asserts that the discussion and composer match the light dashboard surface and that history matches the shared secondary surface. Screenshots show the light dashboard; obsolete chat-only dark captures were removed.

The browser check also verifies that polling preserves the draft and the scroll position, a height change above the visible message preserves its screen position, clicking the pill follows the latest, incoming messages follow the bottom, pending navigation works, the composer fits the viewport, and Ctrl+Enter keeps the original stage/item target and sends no automatic version references. Historical messages still carry twelve references in this fixture.

To reproduce, serve this worktree with `npm run dev --prefix creative-web -- --host 127.0.0.1 --port 5177`, import `captureChatWindow` from `creative-web/browser/chat-window-check.mjs` into a Playwright runner, and call `captureChatWindow(page, "after")`. Call `captureChatWindow(page, "after", { reviewing: true })` as well to capture the pending-review layout. The before set was captured before the messenger implementation.

## Composer and message polish (task 6072, spec v9)

[Desktop, 1440×900](polish-stage-light.png) and [narrow, 390×844](polish-stage-light-narrow.png) show the tinted, bordered header, contrasting message pane, white orchestrator and indigo human bubbles, stronger metadata, and a decided proposal with its own accent edge. The stage fixture contains nine mixed entries, including pending and replaced proposals. [Pending proposal](after-pending-proposal.png) shows the accent border and decision actions.

The composer starts at four rows (104px), grows to 192px, then scrolls internally. About stays left; the keyboard hint precedes the rightmost Send button, including while Discard draft is present. On narrow screens the context wraps above the actions. The browser check asserts message spacing, distinct surfaces, composer geometry, the long-draft cap, and the existing draft/scroll/keyboard behavior in the light dashboard under both OS color schemes. Run the same captureChatWindow(page, "after") workflow above to reproduce the polish captures.

Human messages without attached versions omit the context disclosure entirely. The polish captures show those compact messages; messages with attachments retain their collapsible version references.
