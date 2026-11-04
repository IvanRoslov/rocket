# Creative uses Rocket UI

Task 5993 corrects the presentation of feature creative-work (5986). Preserve the separate entry/build, workspace arrangement, API calls, auth return, and all existing controls/context bindings.

## Sharing approach

Import Rocket's existing `web/src/styles/tokens.css`, `base.css` and actual dependency-free UI components directly from the sibling source tree. Reuse Button, Badge, Card, Dot, Tabs and EmptyState. Extract only the existing Rocket brand markup into a presentation-only shared RocketBrand component used by both headers. Keep the development header/navigation behavior untouched.

Direct source imports are the smallest practical arrangement for these adjacent Vite builds. Vite deduplicates React so shared components use the consuming app's runtime. A new UI package would add unnecessary packaging; copying the palette would retain the parallel design system and fail acceptance.

## Creative-specific presentation

Keep layout/media/context styles in Creative, consuming shared tokens for surfaces, text, borders, radii and fonts. Replace the dark 80px header with Rocket's compact light header treatment, a Creative badge and subtle existing indigo accent. Keep project/work selectors and existing actions. Native form fields follow existing Rocket form styling; no form abstraction or development navigation imports.

## Validation

Run existing Creative behavior tests, relevant shared UI/AppShell tests and both production builds. No CSS test suite, dependency upgrades, demo data, paid calls, daemon restart or installed binary changes. Human owns real-case visual evaluation.
