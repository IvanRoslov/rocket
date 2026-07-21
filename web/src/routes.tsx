import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ChatScreen } from './screens/chat/ChatScreen'
import { KanbanScreen } from './screens/kanban/KanbanScreen'
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
      { path: '/questions', element: <QuestionsScreen /> },
      { path: '/system', element: <SystemScreen /> },
      { path: '/settings', element: <SettingsScreen /> },
    ],
  },
])
