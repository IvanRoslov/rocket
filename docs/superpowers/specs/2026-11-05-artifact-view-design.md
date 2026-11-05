# Published HTML artifact viewing

Accepted by creative-work-orch for creative-work deliverable 2 (spec v6 / plan v5).

Open the selected immutable published HTML version in a separate browser tab while retaining artifact history, review/context identity, media previews and optional download. No daemon restart, configuration mutation, real creative runs or public publishing.

## Approach

Reuse the configured separate-host preview listener. An authenticated version-specific GET view route issues a random, five-minute capability for exactly that immutable file. The privileged content route continues to download HTML. A same-origin active document is inappropriate; another hosting platform is unnecessary.

The isolated listener serves a trusted wrapper identifying filename/version and explaining that only self-contained HTML is supported, with the actual file in an opaque-origin sandbox. Keep the notice outside artifact control so blocked dependencies do not silently look like a successful blank preview. Allow inline scripts/styles and data/blob media/fonts; block external resources, network APIs, forms, popups, frames and same-origin privileges. Apply sandbox in response headers as well as the iframe. Use no-store, no-referrer, nosniff and opener isolation. No Rocket tokens appear in URLs; only the bounded capability grants access. Device revocation invalidates its issued capabilities. Unknown, expired, wrong-path/method, missing content and missing listener setup fail closed with useful messages.

## Boundaries

Only published single HTML files; sibling assets, remote dependencies and server-side applications are unsupported. Existing preview-origin configuration and private reverse forwarding are required. No automatic configuration or restarts. HTML viewing retains existing device authentication and validates agent project identity. Media content serving is unchanged.

Artifacts.tsx and its tests belong to this task; workspace-tabs owns App/RunWorkspace/composition CSS. Preserve Artifacts props/onShown. Implementation details (five-minute TTL, bounded in-memory registry, wrapper UI) are worker decisions.

## Verification

Targeted Go tests for immutable bytes, auth/revocation, capability expiry, listener/path/method/host boundaries, headers and unavailable files/setup. React tests for selected/review HTML links and retained media/download. Build creative-web and run its tests. Synthetic browser checks only; real-case evaluation belongs to the human.
