import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ProjectsScreen } from './screens/projects/ProjectsScreen'
import { SettingsScreen } from './screens/settings/SettingsScreen'
import { SystemScreen } from './screens/system/SystemScreen'
import { WizardScreen } from './screens/newproject/WizardScreen'

function KanbanPage() {
  return <div>Kanban</div>
}

function TaskPage() {
  return <div>Task</div>
}

export const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <ProjectsScreen /> },
      { path: '/projects/new', element: <WizardScreen /> },
      { path: '/p/:projectId', element: <KanbanPage /> },
      { path: '/p/:projectId/tasks/:taskId', element: <TaskPage /> },
      { path: '/system', element: <SystemScreen /> },
      { path: '/settings', element: <SettingsScreen /> },
    ],
  },
])
