// Fixture data for msw handlers, dev mocking and tests.
//
// Deliberately consistent with the design mockups (docs/design/SUMMARY.md):
// project "billing" (main `api`, linked `web`+`infra`), task #12 "Billing v2"
// with subtasks/sessions, question Q3 (open, awaiting user), spec/plan docs,
// a journal, and a message thread.

import type {
  GithubRepo,
  Message,
  Project,
  Question,
  Repo,
  Session,
  Settings,
  SystemInfo,
  Task,
  TaskDoc,
  TaskLogEntry,
} from '../lib/types'

// Fixed "now" reference so relative fixture timestamps stay stable across
// runs. Matches roughly "today" for the dev environment.
const NOW = 1_800_000_000 // seconds since epoch
const MIN = 60
const HOUR = 60 * MIN
const DAY = 24 * HOUR

export const repos: Repo[] = [
  {
    id: 'api',
    path: '/home/dev/repos/api',
    default_branch: 'main',
    auto_cleanup: true,
    env: {},
    symlinks: [],
    post_create: [],
    created_at: NOW - 200 * DAY,
  },
  {
    id: 'web',
    path: '/home/dev/repos/web',
    default_branch: 'main',
    auto_cleanup: true,
    env: {},
    symlinks: ['.env.local'],
    post_create: ['npm install'],
    created_at: NOW - 200 * DAY,
  },
  {
    id: 'infra',
    path: '/home/dev/repos/infra',
    default_branch: 'main',
    auto_cleanup: false,
    env: {},
    symlinks: [],
    post_create: [],
    created_at: NOW - 200 * DAY,
  },
  {
    id: 'data',
    path: '/home/dev/repos/data',
    default_branch: 'main',
    auto_cleanup: true,
    env: {},
    symlinks: [],
    post_create: [],
    created_at: NOW - 200 * DAY,
  },
]

export const projects: Project[] = [
  {
    id: 'billing',
    name: 'Billing',
    main: 'api',
    linked: ['web', 'infra'],
    live_sessions: 3,
    tasks: { backlog: 1, in_progress: 1, review: 1, done: 1 },
    created_at: NOW - 150 * DAY,
  },
  {
    id: 'analytics',
    name: 'Analytics',
    main: 'data',
    linked: [],
    live_sessions: 0,
    tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
    created_at: NOW - 100 * DAY,
  },
]

export const sessions: Session[] = [
  {
    id: 's-billing-v2-orch',
    kind: 'orchestrator',
    project_id: 'billing',
    repo_id: 'api',
    feature_slug: 'billing-v2',
    agent: 'claude',
    branch: 'feature/billing-v2',
    worktree_path: '/home/dev/.rocket/worktrees/billing-v2-orch',
    tmux_name: 'billing-v2-orch',
    state: 'running',
    activity: 'active',
    activity_ts: NOW - 2 * MIN,
    created_at: NOW - 3 * DAY,
    updated_at: NOW - 2 * MIN,
  },
  {
    id: 's-billing-v2-w1',
    kind: 'worker',
    project_id: 'billing',
    repo_id: 'api',
    feature_slug: 'billing-v2',
    parent_id: 's-billing-v2-orch',
    agent: 'claude',
    branch: 'feature/billing-v2-schema',
    worktree_path: '/home/dev/.rocket/worktrees/billing-v2-w1',
    tmux_name: 'billing-v2-w1',
    state: 'running',
    activity: 'ready',
    activity_ts: NOW - 20 * MIN,
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 20 * MIN,
  },
  {
    id: 's-billing-v2-w2',
    kind: 'worker',
    project_id: 'billing',
    repo_id: 'web',
    feature_slug: 'billing-v2',
    parent_id: 's-billing-v2-orch',
    agent: 'claude',
    branch: 'feature/billing-v2-ui',
    worktree_path: '/home/dev/.rocket/worktrees/billing-v2-w2',
    tmux_name: 'billing-v2-w2',
    state: 'running',
    activity: 'blocked',
    activity_ts: NOW - 40 * MIN,
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 40 * MIN,
  },
]

export const tasks: Task[] = [
  {
    id: 9,
    title: 'Legacy invoice migration',
    description: 'Backfill legacy invoices into the new billing schema.',
    project_id: 'billing',
    status: 'done',
    feature_slug: 'legacy-migration',
    created_by: 'user',
    created_at: NOW - 60 * DAY,
    updated_at: NOW - 40 * DAY,
    completed_at: NOW - 40 * DAY,
  },
  {
    id: 10,
    title: 'Invoice PDF export',
    description: 'Let customers download a PDF copy of any invoice.',
    project_id: 'billing',
    status: 'backlog',
    created_by: 'user',
    created_at: NOW - 10 * DAY,
    updated_at: NOW - 10 * DAY,
  },
  {
    id: 11,
    title: 'Webhook retry backoff',
    description: 'Exponential backoff for failed billing webhooks.',
    project_id: 'billing',
    status: 'review',
    feature_slug: 'webhook-retries',
    created_by: 'orchestrator',
    created_at: NOW - 8 * DAY,
    updated_at: NOW - 1 * DAY,
  },
  {
    id: 12,
    title: 'Billing v2',
    description:
      'Rework the billing subsystem: new schema, prorated plan changes, ' +
      'and a redesigned billing UI.',
    project_id: 'billing',
    status: 'in_progress',
    feature_slug: 'billing-v2',
    session_id: 's-billing-v2-orch',
    created_by: 'user',
    created_at: NOW - 3 * DAY,
    updated_at: NOW - 2 * MIN,
  },
]

export const subtasks: Task[] = [
  {
    id: 13,
    parent_id: 12,
    title: 'Migrate billing schema',
    description: 'Add prorated line items to the billing schema.',
    project_id: 'billing',
    repo_id: 'api',
    status: 'in_progress',
    feature_slug: 'billing-v2',
    session_id: 's-billing-v2-w1',
    created_by: 'orchestrator',
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 20 * MIN,
  },
  {
    id: 14,
    parent_id: 12,
    title: 'New billing UI',
    description: 'Redesign the billing settings screen.',
    project_id: 'billing',
    repo_id: 'web',
    status: 'review',
    feature_slug: 'billing-v2',
    session_id: 's-billing-v2-w2',
    created_by: 'orchestrator',
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 40 * MIN,
  },
]

export const taskDocs: TaskDoc[] = [
  {
    id: 1,
    task_id: 12,
    kind: 'spec',
    title: 'Billing v2 spec',
    body:
      '# Billing v2\n\nSupport prorated mid-cycle plan changes and a new ' +
      'invoice schema.',
    version: 2,
    created_at: NOW - 2 * DAY,
  },
  {
    id: 2,
    task_id: 12,
    kind: 'plan',
    title: 'Billing v2 plan',
    body:
      '1. Migrate schema (api)\n2. Rebuild billing UI (web)\n3. Cut over ' +
      'and monitor.',
    version: 1,
    created_at: NOW - 2 * DAY,
  },
]

export const taskLog: TaskLogEntry[] = [
  {
    id: 1,
    task_id: 12,
    kind: 'status',
    body: 'Orchestrator spawned (billing-v2-orch).',
    author: 'system',
    created_at: NOW - 3 * DAY,
  },
  {
    id: 2,
    task_id: 12,
    kind: 'decision',
    body: 'Decided to add a new `billing_line_items` table instead of reusing `invoices`.',
    author: 's-billing-v2-orch',
    created_at: NOW - 2 * DAY,
  },
  {
    id: 3,
    task_id: 12,
    kind: 'problem',
    body: 'Prorated refund rounding disagreed with finance spreadsheet by 1 cent on edge cases.',
    author: 's-billing-v2-w1',
    created_at: NOW - 1 * DAY,
  },
]

export const questions: Question[] = [
  {
    id: 3,
    task_id: 12,
    seq: 3,
    body: 'Should we support prorated refunds for mid-cycle downgrades?',
    context:
      'Current plan only prorates upgrades. Downgrades take effect at the ' +
      'next billing cycle. Finance wants to know if v2 should change that.',
    status: 'open',
    created_at: NOW - 30 * MIN,
    messages: [
      {
        id: 1,
        question_id: 3,
        author: 'orchestrator',
        kind: 'reply',
        body: 'Opened this to unblock the schema migration — refunds table shape depends on the answer.',
        created_at: NOW - 30 * MIN,
      },
    ],
  },
]

export const messages: Message[] = [
  {
    id: 801,
    to: 's-billing-v2-orch',
    body: 'Please prioritize the schema migration before the UI work.',
    status: 'delivered',
    attempts: 1,
    created_at: NOW - 2 * DAY,
    delivered_at: NOW - 2 * DAY + 5,
  },
  {
    id: 802,
    from: 's-billing-v2-orch',
    to: 'user',
    body: 'Understood — schema migration is now the priority. Kicking off worker for `billing-v2-schema`.',
    status: 'delivered',
    attempts: 1,
    created_at: NOW - 2 * DAY + 60,
    delivered_at: NOW - 2 * DAY + 65,
  },
  {
    id: 812,
    from: 'user',
    to: 's-billing-v2-w2',
    body: 'Ping — any update on the UI work?',
    status: 'failed',
    attempts: 3,
    reason: 'recipient busy',
    created_at: NOW - 5 * MIN,
  },
]

// System — internal/api/system.go. Mirrors docs/design/System.dc.html:
// three live sessions reconciled with the store (orchestrator + 2 workers,
// matching `sessions` above), one orphan tmux with no store record
// (`webhook-retries-w1`), a killed session's leftover worktree (state
// "killed", not an orphan — it still has a store record), queue depth
// 2 queued / 1 failed (see the failed msg#812 above), and a short log tail.
export const systemInfo: SystemInfo = {
  daemon: {
    version: 'rocketd 0.4.1',
    uptime_s: 2 * DAY + 4 * HOUR,
    port: 7420,
    socket: '127.0.0.1:7420',
    db_path: '/home/dev/.rocket/rocket.db',
    config_path: '/home/dev/.rocket/config.toml',
  },
  queue: { queued: 2, failed: 1 },
  tmux: [
    { name: 'billing-v2-orch', session_id: 's-billing-v2-orch', state: 'running', orphan: false },
    { name: 'billing-v2-w1', session_id: 's-billing-v2-w1', state: 'running', orphan: false },
    { name: 'billing-v2-w2', session_id: 's-billing-v2-w2', state: 'running', orphan: false },
    { name: 'webhook-retries-w1', orphan: true },
  ],
  worktrees: [
    {
      path: '/home/dev/.rocket/worktrees/billing-v2-orch',
      session_id: 's-billing-v2-orch',
      size_bytes: 642_000_000,
      state: 'running',
      orphan: false,
    },
    {
      path: '/home/dev/.rocket/worktrees/billing-v2-w1',
      session_id: 's-billing-v2-w1',
      size_bytes: 522_000_000,
      state: 'running',
      orphan: false,
    },
    {
      path: '/home/dev/.rocket/worktrees/legacy-migration-orch',
      session_id: 's-legacy-migration-orch',
      size_bytes: 251_000_000,
      state: 'killed',
      orphan: false,
    },
  ],
  log_tail: [
    '12:04:18  spawn      billing-v2-w1          agent=claude  ok',
    '12:05:02  activity   billing-v2-w2          idle→blocked',
    '12:11:33  deliver    msg#809 → billing-v2-orch  delivered',
    '12:12:57  deliver    msg#812 → billing-v2-w2     FAILED (busy, attempt 3)',
    '12:13:20  reconcile  webhook-retries-w1     orphan: in tmux, not in db',
  ],
}

// GitHub repos — contract type (phase 4), docs/09-github.md. A handful of
// repos on the fictional "acme" account, matching docs/design/NewProject.dc.html.
export const githubRepos: GithubRepo[] = [
  { full_name: 'acme/api', private: true, default_branch: 'main' },
  { full_name: 'acme/web', private: true, default_branch: 'main' },
  { full_name: 'acme/infra', private: true, default_branch: 'main' },
  { full_name: 'acme/docs', private: false, default_branch: 'main' },
  { full_name: 'acme/billing-sdk', private: true, default_branch: 'main' },
  { full_name: 'acme/notifications', private: true, default_branch: 'develop' },
]

// Settings — contract type (phase 4). No GitHub token configured by default,
// so the GitHub tab in the New Project wizard shows the "Connect GitHub"
// placeholder unless a test explicitly sets one via PUT /v1/settings.
export const settings: Settings = {}
