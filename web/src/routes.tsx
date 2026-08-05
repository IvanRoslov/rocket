import { createBrowserRouter } from 'react-router-dom'
import { AgentScreen } from './screens/agents/AgentScreen'
import { AgentsScreen } from './screens/agents/AgentsScreen'
import { GlobalAgentsScreen } from './screens/agents/GlobalAgentsScreen'
import { AppShell } from './components/AppShell'
import { ChatScreen } from './screens/chat/ChatScreen'
import { KanbanScreen } from './screens/kanban/KanbanScreen'
import { MilestonesScreen } from './screens/milestones/MilestonesScreen'
import { ProjectsScreen } from './screens/projects/ProjectsScreen'
import { QuestionsScreen } from './screens/questions/QuestionsScreen'
import { SettingsScreen } from './screens/settings/SettingsScreen'
import { SystemScreen } from './screens/system/SystemScreen'
import { TaskScreen } from './screens/task/TaskScreen'
import { TermScreen } from './screens/term/TermScreen'
import { WizardScreen } from './screens/newproject/WizardScreen'

export const router = createBrowserRouter([
  // Dedicated full-window terminal page — deliberately OUTSIDE AppShell
  // (no sidebar/topbar chrome): it's opened in its own tab from the
  // «▣ term» buttons and the terminal fills the whole viewport.
  { path: '/term/:sessionId', element: <TermScreen /> },
  // Dedicated full-window chat page — same "own tab, outside AppShell"
  // convention as /term, opened from the «💬 chat» buttons.
  { path: '/chat/:sessionId', element: <ChatScreen /> },
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <ProjectsScreen /> },
      { path: '/projects/new', element: <WizardScreen /> },
      { path: '/p/:projectId', element: <KanbanScreen /> },
      { path: '/p/:projectId/tasks/:taskId', element: <TaskScreen /> },
      // Global agents list and card: the only way to reach an agent
      // registered without a project (docs/11-dashboard.md).
      { path: '/agents', element: <GlobalAgentsScreen /> },
      { path: '/agents/:roleId', element: <AgentScreen /> },
      { path: '/p/:projectId/agents', element: <AgentsScreen /> },
      { path: '/p/:projectId/agents/:roleId', element: <AgentScreen /> },
      // Milestones (task #1023, spec v2): outside every project, so the
      // routes carry no projectId — the detail view is the task screen.
      { path: '/milestones', element: <MilestonesScreen /> },
      { path: '/milestones/:taskId', element: <TaskScreen /> },
      { path: '/questions', element: <QuestionsScreen /> },
      { path: '/system', element: <SystemScreen /> },
      { path: '/settings', element: <SettingsScreen /> },
    ],
  },
])
