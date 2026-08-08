// Fixture data for msw handlers, dev mocking and tests.
//
// Deliberately consistent with the design mockups (docs/design/SUMMARY.md):
// project "billing" (main `api`, linked `web`+`infra`), task #12 "Billing v2"
// with subtasks/sessions, question Q3 (open, awaiting user), spec/plan docs,
// a journal, and a message thread.

import type {
  Agent,
  AgentInboxMessage,
  AgentQuestion,
  ChatEntry,
  GithubIssue,
  GithubRepo,
  Message,
  PendingQuiz,
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
    created_at: NOW - 150 * DAY,
  },
  {
    id: 'analytics',
    name: 'Analytics',
    main: 'data',
    linked: [],
    live_sessions: 0,
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
    pr_number: 14,
    pr_state: 'open',
    ci_state: 'passing',
  },
  {
    id: 's-billing-v2-w3',
    kind: 'worker',
    project_id: 'billing',
    repo_id: 'infra',
    feature_slug: 'billing-v2',
    parent_id: 's-billing-v2-orch',
    agent: 'claude',
    branch: 'feature/billing-v2-migrations',
    worktree_path: '/home/dev/.rocket/worktrees/billing-v2-w3',
    tmux_name: 'billing-v2-w3',
    state: 'errored',
    activity: 'exited',
    activity_ts: NOW - 90 * MIN,
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 90 * MIN,
  },
  {
    id: 's-billing-v2-w4',
    kind: 'worker',
    project_id: 'billing',
    repo_id: 'infra',
    feature_slug: 'billing-v2',
    parent_id: 's-billing-v2-orch',
    agent: 'claude',
    branch: 'feature/billing-v2-cleanup',
    worktree_path: '',
    tmux_name: 'billing-v2-w4',
    state: 'done',
    activity: 'exited',
    activity_ts: NOW - 3 * HOUR,
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 3 * HOUR,
    pr_number: 400,
    pr_state: 'merged',
    ci_state: 'passing',
  },
  {
    id: 's-quiz-demo-orch',
    kind: 'orchestrator',
    project_id: 'billing',
    repo_id: 'api',
    feature_slug: 'quiz-demo',
    agent: 'claude',
    branch: 'feature/quiz-demo',
    worktree_path: '/home/dev/.rocket/worktrees/quiz-demo-orch',
    tmux_name: 'quiz-demo-orch',
    state: 'running',
    activity: 'blocked',
    activity_ts: NOW - MIN,
    created_at: NOW - DAY,
    updated_at: NOW - MIN,
    pending_quiz: {
      questions: [
        {
          question: 'Какую стратегию мержа выбрать?',
          header: 'Мерж',
          multi_select: false,
          options: [
            { label: 'Merge commit', description: 'история сохраняется' },
            { label: 'Squash', description: 'одним коммитом' },
          ],
        },
        {
          question: 'Что включить в релиз? (можно несколько)',
          header: 'Релиз',
          multi_select: true,
          options: [{ label: 'Доки' }, { label: 'Миграции' }, { label: 'CLI' }],
        },
      ],
      asked_at: NOW - MIN,
    },
  },
]

/** Standalone export for tests that want to POST a live-quiz answer without cloning the whole session fixture. */
export const quizDemoPendingQuiz: PendingQuiz = sessions.find((s) => s.id === 's-quiz-demo-orch')!.pending_quiz!

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
    // Neutral-badge showcase (task-4): questions were raised but none are
    // currently awaiting the user, so the card shows "? N open" instead of
    // the warn-toned "awaiting you" badge.
    open_questions: 1,
    questions_awaiting_user: 0,
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
    open_questions: 0,
    questions_awaiting_user: 0,
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
    open_questions: 0,
    questions_awaiting_user: 0,
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
    // Showcase for the waiting badge: the orchestrator sits on a question
    // nobody has answered, so the card warns that it needs a keystroke.
    waiting_terminal: true,
    created_by: 'user',
    created_at: NOW - 3 * DAY,
    updated_at: NOW - 2 * MIN,
    // Showcase task for the kanban question badge (task-4): one open
    // question (Q3, see `questions` below) is awaiting the user's reply.
    open_questions: 2,
    questions_awaiting_user: 1,
  },
  {
    // Brainstorm showcase (task-1077): the idea is being shaped, but nothing
    // has been committed to the backlog yet — no session, no feature slug.
    id: 17,
    title: 'Metering rewrite',
    description: 'Shape up per-seat vs per-event metering before committing to a plan.',
    project_id: 'billing',
    status: 'brainstorm',
    created_by: 'user',
    created_at: NOW - 5 * DAY,
    updated_at: NOW - 1 * DAY,
    open_questions: 0,
    questions_awaiting_user: 0,
  },
]

/**
 * Milestones (task #1023, spec v2): root tasks outside every project, held by
 * a persistent agent. Kept apart from `tasks` so a project board can never
 * accidentally render one — the daemon separates them the same way.
 */
export const milestones: Task[] = [
  {
    id: 40,
    title: 'Cut the on-call pager noise in half',
    description: 'Audit alert rules, kill the ones nobody acts on.',
    project_id: '',
    status: 'backlog',
    milestone: true,
    created_by: 'user',
    created_at: NOW - 6 * DAY,
    updated_at: NOW - 6 * DAY,
    open_questions: 0,
    questions_awaiting_user: 0,
  },
  {
    id: 41,
    title: 'Own the incident review ritual',
    description: 'Run reviews weekly, publish the write-ups.',
    project_id: '',
    status: 'in_progress',
    milestone: true,
    assigned_role: 'sre',
    // Showcase for the quiet badge (subtask #1032): taken, but the agent has
    // shown no work for longer than milestone_quiet_after.
    quiet: true,
    created_by: 'user',
    created_at: NOW - 12 * DAY,
    updated_at: NOW - 2 * DAY,
    open_questions: 2,
    questions_awaiting_user: 1,
  },
  {
    id: 42,
    title: 'Docs pass over every public README',
    project_id: '',
    status: 'review',
    milestone: true,
    assigned_role: 'librarian',
    created_by: 'agent',
    created_at: NOW - 20 * DAY,
    updated_at: NOW - 5 * HOUR,
    open_questions: 0,
    questions_awaiting_user: 0,
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
    open_questions: 0,
    questions_awaiting_user: 0,
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
    open_questions: 0,
    questions_awaiting_user: 0,
  },
  {
    id: 15,
    parent_id: 12,
    title: 'Data migration + backfill',
    description: 'Backfill existing invoices into the prorated line-item schema.',
    project_id: 'billing',
    repo_id: 'infra',
    status: 'in_progress',
    feature_slug: 'billing-v2',
    session_id: 's-billing-v2-w3',
    created_by: 'orchestrator',
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 90 * MIN,
    open_questions: 0,
    questions_awaiting_user: 0,
  },
  {
    id: 16,
    parent_id: 12,
    title: 'Retire legacy billing cron',
    description: 'Remove the old nightly billing reconciliation cron now superseded by v2.',
    project_id: 'billing',
    repo_id: 'infra',
    status: 'done',
    feature_slug: 'billing-v2',
    session_id: 's-billing-v2-w4',
    created_by: 'orchestrator',
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 3 * HOUR,
    completed_at: NOW - 3 * HOUR,
    open_questions: 0,
    questions_awaiting_user: 0,
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

// Question #3 on task #12: asked by the billing-v2 orchestrator, one
// orchestrator-authored reply already in the thread (author = session id,
// kind "reply") and no user message yet, so whose_turn stays "user" per the
// same rule the daemon uses (whoseTurn in internal/api/questions.go): no
// messages -> "user"; that still holds once at least one orchestrator
// message is appended without a user response after it.
export const questions: Question[] = [
  {
    id: 1,
    task_id: 12,
    ordinal: 1,
    asked_by: 's-billing-v2-orch',
    title: 'Default the v2 flag on for internal test accounts?',
    body: 'Should the v2 flag default on for internal test accounts?',
    status: 'resolved',
    resolution: 'answered',
    local_ref: '12/Q1',
    type: 'decision',
    participants: ['human', 's-billing-v2-orch'],
    waiting_on: [],
    your_turn: false,
    whose_turn: '',
    asked_at: NOW - 2 * DAY,
    resolved_at: NOW - 2 * DAY + 20 * MIN,
    messages: [
      {
        id: 101,
        author: 's-billing-v2-orch',
        kind: 'reply',
        body: 'Should internal test accounts get the v2 flag by default while we build this out?',
        created_at: NOW - 2 * DAY,
      },
      {
        id: 102,
        kind: 'answer',
        body: 'Yes — flip it on for @acme-internal accounts only.',
        created_at: NOW - 2 * DAY + 20 * MIN,
      },
    ],
  },
  {
    id: 2,
    task_id: 12,
    ordinal: 2,
    asked_by: 's-billing-v2-orch',
    title: 'Rounding mode for invoice totals',
    body: 'Which currency rounding mode for invoice totals — half-up or banker’s?',
    status: 'resolved',
    resolution: 'answered',
    local_ref: '12/Q2',
    type: 'decision',
    participants: ['human', 's-billing-v2-orch'],
    waiting_on: [],
    your_turn: false,
    whose_turn: '',
    asked_at: NOW - 1 * DAY,
    resolved_at: NOW - 1 * DAY + 15 * MIN,
    messages: [
      {
        id: 103,
        author: 's-billing-v2-orch',
        kind: 'reply',
        body: 'Invoice totals need a rounding mode — half-up or banker’s rounding?',
        created_at: NOW - 1 * DAY,
      },
      {
        id: 104,
        kind: 'answer',
        body: 'Half-up, to match what finance already uses.',
        created_at: NOW - 1 * DAY + 15 * MIN,
      },
    ],
  },
  {
    id: 3,
    task_id: 12,
    ordinal: 3,
    asked_by: 's-billing-v2-orch',
    title: 'Prorated refunds for mid-cycle downgrades',
    body:
      'Should we support prorated refunds for mid-cycle downgrades?\n\n---\n\n' +
      'Current plan only prorates upgrades. Downgrades take effect at the ' +
      'next billing cycle. Finance wants to know if v2 should change that.',
    status: 'open',
    // The new-model showcase (task #1023): a local ref, answer options that
    // close the thread in one click, and a thread that has gone stale.
    local_ref: '12/Q3',
    type: 'decision',
    options: ['Yes, prorate downgrades', 'No, keep next-cycle'],
    stale: true,
    attention: ['human'],
    // The multi-participant showcase: the human, the orchestrator and the
    // "cto" persistent agent. The human speaks under BOTH wire spellings —
    // "" is what internal/api's wireAuthor() sends today and "human" is what
    // subtask #736 will send — and the dashboard must render them alike.
    participants: ['human', 's-billing-v2-orch', 'cto'],
    waiting_on: ['human'],
    your_turn: true,
    whose_turn: 'user',
    asked_at: NOW - 30 * MIN,
    messages: [
      {
        id: 1,
        author: 's-billing-v2-orch',
        kind: 'reply',
        body: 'Opened this to unblock the schema migration — refunds table shape depends on the answer.',
        created_at: NOW - 30 * MIN,
      },
      {
        id: 2,
        author: '',
        kind: 'reply',
        body: 'Legacy wire: the human still arrives with an empty author.',
        created_at: NOW - 25 * MIN,
      },
      {
        id: 3,
        author: 'human',
        kind: 'reply',
        body: 'Post-#736 wire: the same human, spelled canonically.',
        created_at: NOW - 20 * MIN,
      },
      {
        id: 4,
        author: 'cto',
        kind: 'reply',
        body: 'Finance signed off on prorating downgrades — your call on the cutover date.',
        addressed_to: ['human'],
        created_at: NOW - 15 * MIN,
      },
    ],
  },
  // The fyi showcase (task #1023 spec v1 §«Тип треда»): a status note, born
  // resolved, waiting on nobody. It belongs in the history and must never
  // light a badge or land in an open count.
  {
    id: 6,
    task_id: 12,
    ordinal: 4,
    local_ref: '12/Q4',
    asked_by: 's-billing-v2-orch',
    title: 'Refunds migration deployed to staging',
    body: 'Deployed the refunds migration to staging.',
    status: 'resolved',
    resolution: 'fyi',
    type: 'fyi',
    participants: ['human', 's-billing-v2-orch'],
    waiting_on: [],
    attention: [],
    your_turn: false,
    whose_turn: '',
    asked_at: NOW - 3 * HOUR,
    resolved_at: NOW - 3 * HOUR,
    messages: [],
  },
  // User-opened threads (asked_by "") on task #13, opposite direction from
  // Q1-Q3 above — the human asked the orchestrator, not the other way
  // around. Q4 is fresh (no reply yet, whose_turn stays "orchestrator");
  // Q5 already has an orchestrator reply, flipping whose_turn to "user".
  {
    id: 4,
    task_id: 13,
    ordinal: 1,
    asked_by: '',
    title: 'Backfill existing rows, or new ones only?',
    body: 'Should we backfill existing rows or only handle new ones going forward?',
    status: 'open',
    participants: ['human', 's-billing-v2-w1'],
    waiting_on: ['s-billing-v2-w1'],
    your_turn: false,
    whose_turn: 'orchestrator',
    asked_at: NOW - 10 * MIN,
    messages: [],
  },
  {
    id: 5,
    task_id: 13,
    ordinal: 2,
    asked_by: '',
    title: 'Is the migration safe to run live?',
    body: 'Is the migration safe to run while the app is live, or does it need a maintenance window?',
    status: 'open',
    participants: ['human', 's-billing-v2-w1'],
    waiting_on: ['human'],
    your_turn: true,
    whose_turn: 'user',
    asked_at: NOW - 25 * MIN,
    messages: [
      {
        id: 105,
        author: 's-billing-v2-w1',
        kind: 'reply',
        body: 'It can run live — the new columns are nullable and backfilled asynchronously.',
        created_at: NOW - 18 * MIN,
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
    // No `from`: user-authored messages never populate it (internal/api/
    // messages.go) — the daemon only sets `from` when a session id was
    // supplied via X-Rocket-Session.
    to: 's-billing-v2-w2',
    body: 'Ping — any update on the UI work?',
    status: 'failed',
    attempts: 3,
    reason: 'recipient busy',
    created_at: NOW - 5 * MIN,
  },
]

// Chat — internal/api/chat.go, docs/13-chat.md. One transcript per session
// id, all three roles represented, matching the example response in the
// spec doc (billing test failure -> trace -> Bash tool call -> fix found).
export const chatEntries: Record<string, ChatEntry[]> = {
  's-billing-v2-orch': [
    { role: 'user', text: 'почему упал тест biling_test.go?', ts: NOW - 3 * HOUR },
    { role: 'assistant', text: 'смотрю на трейс', ts: NOW - 3 * HOUR + 2 },
    {
      role: 'tool',
      tool_name: 'Bash',
      text: '{"command":"go test ./internal/billing/...","description":"Run billing tests"}',
      ts: NOW - 3 * HOUR + 3,
    },
    { role: 'assistant', text: 'нашёл: гонка в billing.Reconcile, фикс отправлю', ts: NOW - 3 * HOUR + 10 },
  ],
  's-billing-v2-w1': [
    { role: 'user', text: 'начни со схемы billing_accounts', ts: NOW - 20 * MIN },
    { role: 'assistant', text: 'ок, читаю текущую миграцию', ts: NOW - 19 * MIN },
  ],
  's-billing-v2-w3': [
    { role: 'assistant', text: 'миграция упала на шаге 3, воркер завершён', ts: NOW - 90 * MIN },
  ],
  // A closed AskUserQuestion round (docs/13-chat.md «Квиз-раунды в ленте»):
  // the asking tool entry (`quiz`: raw camelCase tool input) immediately
  // followed by its `quiz_answer` close (`quiz`: raw answers echo, keyed by
  // question text).
  's-quiz-demo-orch': [
    { role: 'user', text: 'что делаем с релизом?', ts: NOW - 2 * HOUR },
    {
      role: 'tool',
      tool_name: 'AskUserQuestion',
      text: '{"questions":[{"question":"Какой линтер подключить?","header":"Линтер"...',
      ts: NOW - 2 * HOUR + 1,
      quiz: {
        questions: [
          {
            question: 'Какой линтер подключить?',
            header: 'Линтер',
            multiSelect: false,
            options: [
              { label: 'ESLint', description: 'уже используется в web' },
              { label: 'Biome', description: 'быстрее' },
            ],
          },
        ],
      },
    },
    {
      role: 'quiz_answer',
      text: 'Какой линтер подключить? → ESLint',
      ts: NOW - 2 * HOUR + 30,
      quiz: {
        questions: [{ question: 'Какой линтер подключить?' }],
        answers: { 'Какой линтер подключить?': 'ESLint' },
      },
    },
  ],
}

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

// GitHub issues — internal/api/github_issues.go, keyed by registered repo id
// (not owner/name — see `GithubIssue` doc comment). `api` and `web` (both
// project "billing" repos) carry a few open issues each with labels and
// varying `updated_at`; `infra` is used by tests to exercise the
// `not_a_github_repo` branch (see mocks/handlers.ts GET /v1/github/issues).
export const githubIssues: Record<string, GithubIssue[]> = {
  api: [
    {
      number: 241,
      title: 'Rate limit billing webhooks',
      body: 'Webhook retries currently have no backoff — a flaky downstream can hammer us.',
      html_url: 'https://github.com/acme/api/issues/241',
      state: 'open',
      updated_at: new Date((NOW - 2 * DAY) * 1000).toISOString(),
      labels: ['bug', 'billing'],
    },
    {
      number: 238,
      title: 'Add prorated refund support',
      body: 'Finance wants mid-cycle downgrades to prorate a refund, not just credit next cycle.',
      html_url: 'https://github.com/acme/api/issues/238',
      state: 'open',
      updated_at: new Date((NOW - 5 * DAY) * 1000).toISOString(),
      labels: ['enhancement'],
    },
    {
      number: 190,
      title: 'Flaky billing_test.go',
      body: '',
      html_url: 'https://github.com/acme/api/issues/190',
      state: 'open',
      updated_at: new Date((NOW - 30 * DAY) * 1000).toISOString(),
      labels: [],
    },
  ],
  web: [
    {
      number: 55,
      title: 'Billing settings UI polish',
      body: 'Spacing is off on the plan-change modal at 1280px.',
      html_url: 'https://github.com/acme/web/issues/55',
      state: 'open',
      updated_at: new Date((NOW - 1 * DAY) * 1000).toISOString(),
      labels: ['design'],
    },
  ],
}

// Settings — internal/api/settings.go. No GitHub token configured by
// default (`github_token: ""`), so the GitHub tab in the New Project wizard
// shows the "Connect GitHub" placeholder (GET /v1/github/repos 400 no_token)
// unless a test explicitly sets one via PUT /v1/settings.
export const settings: Settings = { github_token: '' }

// ---------------------------------------------------------------------------
// Agents (docs/10-agents.md): agent "sre" of project billing — enabled, its
// tmux session up, two unread messages and a thread it is waiting on you for.
// Agent "triage" is the disabled/dead-session counterpart.
// ---------------------------------------------------------------------------

export const agents: Agent[] = [
  {
    id: 'sre',
    description: 'Platform SRE: triages platform issues, escalates what it cannot take.',
    project: 'billing',
    dir: '/home/dev/agents/sre',
    command: 'claude',
    enabled: true,
    session_alive: true,
    unread: 2,
    open_questions: 1,
    awaiting_user: 1,
    created_at: NOW - 5 * DAY,
    updated_at: NOW - 4 * MIN,
  },
  {
    id: 'triage',
    description: '',
    project: 'billing',
    dir: '',
    command: '',
    enabled: false,
    session_alive: false,
    unread: 0,
    open_questions: 0,
    awaiting_user: 0,
    created_at: NOW - 2 * DAY,
    updated_at: NOW - 2 * DAY,
  },
  // Registered with no project: invisible in the project-scoped list, which is
  // exactly what the global `/agents` view exists for.
  {
    id: 'librarian',
    description: 'Keeps the docs honest across every project.',
    project: '',
    dir: '/home/dev/agents/librarian',
    command: 'claude',
    enabled: true,
    session_alive: false,
    unread: 0,
    open_questions: 0,
    awaiting_user: 0,
    created_at: NOW - 3 * DAY,
    updated_at: NOW - 1 * DAY,
  },
]

export const agentInbox: AgentInboxMessage[] = [
  {
    id: 1,
    from: 'billing-v2-orch',
    body: 'blocked by the platform migration',
    status: 'unread',
    created_at: NOW - 6 * MIN,
  },
  {
    id: 2,
    from: 'ivan',
    body: 'please look at acme/platform#42',
    status: 'unread',
    created_at: NOW - 4 * MIN,
  },
  {
    id: 3,
    from: 'billing-v2-w1',
    body: 'migration is done, thanks',
    status: 'read',
    created_at: NOW - 2 * HOUR,
    read_at: NOW - 100 * MIN,
  },
]

export const agentQuestions: AgentQuestion[] = [
  {
    id: 91,
    role_id: 'sre',
    ordinal: 1,
    local_ref: 'sre/Q1',
    type: 'decision',
    asked_by: 'sre',
    title: 'Close acme/platform#42 now?',
    body:
      'Should I close acme/platform#42 now?\n\n---\n\n' +
      'The task is in review and the team has not confirmed yet.',
    status: 'open',
    participants: ['human', 'sre'],
    waiting_on: ['human'],
    your_turn: true,
    whose_turn: 'user',
    asked_at: NOW - 10 * MIN,
    messages: [],
  },
  {
    id: 90,
    role_id: 'sre',
    ordinal: 2,
    local_ref: 'sre/Q2',
    type: 'decision',
    asked_by: '',
    title: 'What is blocking acme/platform#43?',
    body: 'What is blocking acme/platform#43?',
    status: 'resolved',
    resolution: 'answered',
    participants: ['human', 'sre'],
    waiting_on: [],
    your_turn: false,
    asked_at: NOW - 2 * DAY,
    resolved_at: NOW - 2 * DAY + HOUR,
    messages: [
      { id: 1, author: 'sre', kind: 'reply', body: 'the DB migration', created_at: NOW - 2 * DAY + MIN },
      { id: 2, author: '', kind: 'answer', body: 'thanks, keep it deferred', created_at: NOW - 2 * DAY + HOUR },
    ],
  },
]
