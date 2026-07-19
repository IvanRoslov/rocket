import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { KanbanScreen } from './screens/kanban/KanbanScreen'
import { ProjectsScreen } from './screens/projects/ProjectsScreen'
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
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <ProjectsScreen /> },
      { path: '/projects/new', element: <WizardScreen /> },
      { path: '/p/:projectId', element: <KanbanScreen /> },
      { path: '/p/:projectId/tasks/:taskId', element: <TaskScreen /> },
      { path: '/system', element: <SystemScreen /> },
      { path: '/settings', element: <SettingsScreen /> },
    ],
  },
])
