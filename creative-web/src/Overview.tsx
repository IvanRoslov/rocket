import type { UseQueryResult } from "@tanstack/react-query";
import { Button } from "../../web/src/components/Button";
import { Tabs } from "../../web/src/components/Tabs";
import { useUnread } from "./feed";
import { statusLabel } from "./workflow";
import type { Run } from "./types";
import { Empty, ErrorNotice } from "./ui";

export function Overview({ runs, projects, projectsLoaded, completed, onView, onOpen }: {
  runs: UseQueryResult<Run[], Error>;
  projects: { id: string; name: string }[];
  projectsLoaded: boolean;
  completed: boolean;
  onView: (completed: boolean) => void;
  onOpen: (run: Run) => void;
}) {
  const visible = runs.data?.filter(r => (r.closure_state === "accepted") === completed);
  return <main className="welcome overview">
    <span className="eyebrow">Creative · All projects</span>
    <h1>{completed ? "Completed works" : "Active works"}</h1>
    <p>{completed ? "Finally accepted work. Open an archive to revisit its history and results." : "Every project, one place to pick up your creative work."}</p>
    <Tabs label="Works" activeId={completed ? "completed" : "active"}
      items={[{ id: "active", label: "Active" }, { id: "completed", label: "Completed" }]}
      onChange={id => onView(id === "completed")} />
    <ErrorNotice error={runs.error} />
    {runs.isPending ? <Empty>Loading works…</Empty> : <>
      <div className="run-grid">
        {visible?.map(run => <WorkCard key={run.id} run={run}
          project={projects.find(p => p.id === run.project_id)?.name ?? run.project_id} onOpen={() => onOpen(run)} />)}
      </div>
      {!runs.isError && visible?.length === 0 && <Empty>{completed ? "No accepted work yet." : "No active work."}</Empty>}
    </>}
    {projectsLoaded && projects.length === 0 && <p>Create a Rocket project to begin. <a href="/projects/new">Create project</a></p>}
  </main>;
}
function WorkCard({ run, project, onOpen }: { run: Run; project: string; onOpen: () => void }) {
  const unread = useUnread(run.id);
  const stage = run.stages.find(s => s.status !== "completed");
  const state = run.closure_state === "accepted" ? "Accepted · read-only archive"
    : run.closure_state === "closing" ? "Closure incomplete · retry available"
    : stage ? `${stage.name} · ${statusLabel[stage.status]}` : statusLabel[run.status];
  return <Button variant="secondary" size="sm" className="run-card" onClick={onOpen}>
    <strong>{run.name}</strong>
    <span className="run-project">{project}</span>
    <small>{state}</small>
    {unread.isError ? <span className="unread-state" title={unread.error.message}>Unread unavailable</span>
      : unread.isPending ? <span className="unread-state">Loading unread…</span>
      : unread.data.total > 0 ? <span className="feed-unread" aria-label={`${unread.data.total} unread messages`}>{unread.data.total}</span> : null}
  </Button>;
}
