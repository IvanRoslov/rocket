# Creative chat window visual checks

Task 6066 for creative-messages (task 6061, spec v6). Fixtures use an item with twelve displayed artifact versions and twelve references on its messages. All HTTP requests are mocked; screenshots do not change a running daemon. The dashboard is light-only, and the chat inherits its light surfaces regardless of OS color scheme or host theme attribute.

| Scope | Theme | Width | Before | After |
| --- | --- | --- | --- | --- |
| stage | light | 1600 | [Before](before-stage-light.png) | [After](after-stage-light.png) |
| stage | light | 390 | [Before](before-stage-light-narrow.png) | [After](after-stage-light-narrow.png) |
| item | light | 1600 | [Before](before-item-light.png) | [After](after-item-light.png) |
| item | light | 390 | [Before](before-item-light-narrow.png) | [After](after-item-light-narrow.png) |
| run | light | 1600 | [Before](before-run-light.png) | [After](after-run-light.png) |
| run | light | 390 | [Before](before-run-light-narrow.png) | [After](after-run-light-narrow.png) |

Additional states: [new-message pill](after-new-message-pill.png), [pending proposal](after-pending-proposal.png).

The browser check emulates both dark and light OS color schemes, with unset, dark, and light host theme attributes, at both widths and in all three scopes. It asserts that the discussion and composer match the light dashboard surface and that history matches the shared secondary surface. Screenshots show the light dashboard; obsolete chat-only dark captures were removed.

The browser check also verifies that polling preserves the draft and the scroll position, a height change above the visible message preserves its screen position, clicking the pill follows the latest, incoming messages follow the bottom, pending navigation works, the composer fits the viewport, and Ctrl+Enter sends all twelve version references with the original item target.

To reproduce, serve this worktree with `npm run dev --prefix creative-web -- --host 127.0.0.1 --port 5177`, import `captureChatWindow` from `creative-web/browser/chat-window-check.mjs` into a Playwright runner, and call `captureChatWindow(page, "after")`. The before set was captured before the implementation.
