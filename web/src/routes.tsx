import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { KanbanScreen } from './screens/kanban/KanbanScreen'
import { ProjectsScreen } from './screens/projects/ProjectsScreen'
import { SettingsScreen } from './screens/settings/SettingsScreen'
import { SystemScreen } from './screens/system/SystemScreen'
import { WizardScreen } from './screens/newproject/WizardScreen'

function TaskPage() {
  return <div>Task</div>
}

export const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <ProjectsScreen /> },
      { path: '/projects/new', element: <WizardScreen /> },
      { path: '/p/:projectId', element: <KanbanScreen /> },
      { path: '/p/:projectId/tasks/:taskId', element: <TaskPage /> },
      { path: '/system', element: <SystemScreen /> },
      { path: '/settings', element: <SettingsScreen /> },
    ],
  },
])
