import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ProjectsScreen } from './screens/projects/ProjectsScreen'

function NewProjectPage() {
  return <div>New project</div>
}

function KanbanPage() {
  return <div>Kanban</div>
}

function TaskPage() {
  return <div>Task</div>
}

function SystemPage() {
  return <div>System</div>
}

function SettingsPage() {
  return <div>Settings</div>
}

export const router = createBrowserRouter([
  {
    element: <AppShell />,
    children: [
      { path: '/', element: <ProjectsScreen /> },
      { path: '/projects/new', element: <NewProjectPage /> },
      { path: '/p/:projectId', element: <KanbanPage /> },
      { path: '/p/:projectId/tasks/:taskId', element: <TaskPage /> },
      { path: '/system', element: <SystemPage /> },
      { path: '/settings', element: <SettingsPage /> },
    ],
  },
])
