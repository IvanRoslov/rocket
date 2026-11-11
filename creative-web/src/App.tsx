import { Badge } from "../../web/src/components/Badge";
import { RocketBrand } from "../../web/src/components/RocketBrand";
import { Tabs } from "../../web/src/components/Tabs";
import { Button } from "../../web/src/components/Button";
import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, base, useCommand } from "./api";
import type { Pipeline, Run, Scope, ShownVersion } from "./types";
import { Pipelines } from "./Pipelines";
import { ProcessRail, WorkDescription, ItemEditor } from "./RunWorkspace";
import { Artifacts } from "./Artifacts";
import { ReviewActions } from "./ReviewActions";
import { Empty, ErrorNotice } from "./ui";
import { Discussion } from "./Discussion";
import { CreativeSessionRail } from "./SessionRail";
import { FinalAcceptance } from "./FinalAcceptance";
import { Overview } from "./Overview";
type View = "work" | "active" | "completed" | "pipelines" | "new";
interface Selection {
  view?: View;
  project?: string;
  run?: number;
  stage?: number;
  item?: number;
}
function readSelection(): Selection {
  const p = new URLSearchParams(window.location.search);
  const number = (key: string) => {
    const n = Number(p.get(key));
    return Number.isSafeInteger(n) && n > 0 ? n : undefined;
  };
  const deep = window.location.pathname.match(/^\/creative\/runs\/(\d+)\/?$/);
  const view = p.get("view");
  return {
    view: ["active", "completed", "pipelines", "new"].includes(view ?? "") ? view as View : undefined,
    project: p.get("project") || undefined,
    run: number("run") ?? (deep ? Number(deep[1]) : undefined),
    stage: number("stage"),
    item: number("item"),
  };
}
export function App() {
  const [selection, setSelection] = useState(readSelection);
  const view = selection.view ?? (selection.run ? "work" : "active");
  useEffect(() => {
    const pop = () => setSelection(readSelection());
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, []);
  function select(next: Selection) {
    const p = new URLSearchParams();
    Object.entries(next).forEach(([k, v]) => {
      if (v && !(k === "view" && (v === "work" || v === "active"))) p.set(k, String(v));
    });
    window.history.pushState({}, "", `/creative/${p.size ? `?${p}` : ""}`);
    setSelection(next);
  }
  const projects = useQuery({
    queryKey: ["creative", "projects"],
    queryFn: () => api.get<{ id: string; name: string }[]>("/v1/projects"),
  });
  const run = useQuery({
    queryKey: ["creative", "run", selection.run],
    queryFn: () => api.get<Run>(`${base}/runs/${selection.run}`),
    enabled: !!selection.run,
  });
  const pipelines = useQuery({
    queryKey: ["creative", "pipelines"],
    queryFn: () => api.get<Pipeline[]>(`${base}/pipelines`),
  });
  const overview = view === "active" || view === "completed";
  const runs = useQuery({
    queryKey: ["creative", "runs"],
    queryFn: () => api.get<Run[]>(`${base}/runs`),
    enabled: overview,
    refetchInterval: overview ? 2000 : false,
  });
  const selectedRun = run.data;
  const projectName = (id: string) => projects.data?.find(p => p.id === id)?.name ?? id;
  const allWorks = () => select({});
  const returnToWork = () => select(selection.run ? { project: selectedRun?.project_id, run: selection.run, stage: selection.stage, item: selection.item } : {});
  return (
    <div className="creative-app">
      <header className="topbar">
        <a className="brand" href="/creative/"><RocketBrand /></a>
        <Badge tone="indigo">Creative</Badge>
        {!overview && <Button variant="secondary" size="sm" onClick={allWorks}>All works</Button>}
        {selectedRun && !overview && <div className="current-work" aria-label="Current work">
          <span>{projectName(selectedRun.project_id)}</span><strong>{selectedRun.name}</strong>
        </div>}
        <div className="top-actions">
          <Button variant="primary" size="sm" onClick={() => select({
            view: "new", ...(view === "work" && selectedRun ? { project: selectedRun.project_id, run: selectedRun.id, stage: selection.stage, item: selection.item } : {}),
          })}>New work</Button>
          <Button variant="secondary" size="sm" aria-pressed={view === "pipelines"}
            onClick={() => select({ ...selection, view: "pipelines" })}>Pipelines</Button>
          <a href="/" target="_blank" rel="noopener noreferrer">Development ↗</a>
        </div>
      </header>
      <ErrorNotice error={projects.error ?? pipelines.error ?? (selection.run ? run.error : null)} />
      {view === "pipelines" ? (
        <Pipelines pipelines={pipelines.data ?? []} onClose={returnToWork} />
      ) : overview ? (
        <Overview runs={runs} projects={projects.data ?? []} projectsLoaded={projects.isSuccess} completed={view === "completed"}
          onView={(completed) => select(completed ? { view: "completed" } : {})}
          onOpen={(r) => select({ project: r.project_id, run: r.id })} />
      ) : view === "new" ? (
        projects.isPending || (!!selection.run && run.isPending) ? <main><Empty>Loading projects…</Empty></main> :
        projects.data?.length === 0 ? <main><h1>Your creative workspace</h1><p>Create a Rocket project to begin.</p><a href="/projects/new">Create project</a></main> :
        <NewWork key={`${selection.run ?? "overview"}:${selectedRun?.project_id ?? ""}`}
          project={selection.run ? selectedRun?.project_id ?? "" : ""}
          projects={projects.data ?? []} pipelines={pipelines.data ?? []}
          onCancel={returnToWork}
          onCreated={(r) => select({ project: r.project_id, run: r.id })}
          onPipelines={() => select({ ...selection, view: "pipelines" })} />
      ) : selectedRun ? (
        <Workspace key={selectedRun.id} run={selectedRun} selection={selection}
          onSelect={(stage, item) => select({ project: selectedRun.project_id, run: selectedRun.id, stage, item })} />
      ) : <main>{run.isPending && <Empty>Loading work…</Empty>}</main>}
    </div>
  );
}
function NewWork({
  project: initialProject,
  projects,
  pipelines,
  onCancel,
  onCreated,
  onPipelines,
}: {
  project: string;
  projects: { id: string; name: string }[];
  pipelines: Pipeline[];
  onCancel: () => void;
  onCreated: (run: Run) => void;
  onPipelines: () => void;
}) {
  const [project, setProject] = useState(initialProject);
  const [name, setName] = useState(""),
    [brief, setBrief] = useState(""),
    [pipeline, setPipeline] = useState("");
  const command = useCommand();
  return (
    <section className="page-panel narrow">
      <span className="eyebrow">Start with an idea</span>
      <h1>New creative work</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!project || !pipeline) return;
          command.mutate(() =>
            api
              .post<Run>(`${base}/runs`, {
                project_id: project,
                pipeline_id: Number(pipeline),
                name,
                brief,
              })
              .then(onCreated),
          );
        }}
      >
        <label>
          Project
          <select required value={project} onChange={(e) => setProject(e.target.value)}>
            <option value="" disabled>Choose project</option>
            {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </label>
        <label>
          Work name
          <input
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          Creative brief
          <textarea
            required
            rows={8}
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
          />
        </label>
        <label>
          Pipeline
          <select
            required
            value={pipeline}
            onChange={(e) => setPipeline(e.target.value)}
          >
            <option value="" disabled>
              Choose pipeline
            </option>
            {pipelines.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <p>
          This work keeps a copy of the chosen pipeline. Future template edits
          apply to new work.
        </p>
        {pipelines.length === 0 && (
          <Button
            variant="secondary"
            size="sm"
            type="button"
            onClick={onPipelines}
          >
            Create a pipeline
          </Button>
        )}
        <div className="row">
          <Button
            variant="primary"
            size="sm"
            type="submit"
            disabled={!project || !pipeline || command.isPending}
          >
            Create work
          </Button>
          <Button
            variant="secondary"
            size="sm"
            type="button"
            onClick={onCancel}
          >
            Cancel
          </Button>
        </div>
        <ErrorNotice error={command.error} />
      </form>
    </section>
  );
}
function Workspace({
  run,
  selection,
  onSelect,
}: {
  run: Run;
  selection: Selection;
  onSelect: (stage?: number, item?: number) => void;
}) {
  const readOnly = run.closure_state === "closing" || run.closure_state === "accepted";
  const stage = run.stages.find((s) => s.id === selection.stage),
    item = stage?.items.find((i) => i.id === selection.item);
  const invalid = (!!selection.stage && !stage) || (!!selection.item && !item);
  const scope: Scope = {
    run_id: run.id,
    ...(stage ? { stage_id: stage.id } : {}),
    ...(item ? { item_id: item.id } : {}),
  };
  const scopeKey = `${run.id}:${stage?.id ?? 0}:${item?.id ?? 0}`;
  const [artifactState, setArtifactState] = useState<{
    key: string;
    shown: ShownVersion[];
    ready: boolean;
  }>({ key: "", shown: [], ready: false });
  const [tab, setTab] = useState<"discussion" | "results">("discussion");
  const onShown = useCallback(
    (shown: ShownVersion[], ready: boolean) =>
      setArtifactState((prev) => {
        const next = { key: scopeKey, shown, ready };
        return JSON.stringify(next) === JSON.stringify(prev) ? prev : next;
      }),
    [scopeKey],
  );
  const shown = artifactState.key === scopeKey ? artifactState.shown : [],
    ready = artifactState.key === scopeKey && artifactState.ready;
  const work = item ?? stage;
  const first = run.stages.find((s) => s.status !== "completed");
  const canProgress =
    !!stage &&
    stage.id === first?.id &&
    (!item || stage.status === "in_progress") &&
    (!item ||
      stage.approval !== "item" ||
      stage.items.find((i) => i.status !== "completed")?.id === item.id);
  return (
    <div className="workspace">
      <ProcessRail run={run} scope={scope} onSelect={onSelect} />
      <main className="results">
        <FinalAcceptance key={run.id} run={run} />
        {invalid ? (
          <>
            <ErrorNotice error="This stage or item is not part of the selected work." />
            <Button
              variant="secondary"
              size="sm"
              onClick={() => onSelect()}>Back to work</Button
            >
          </>
        ) : (
          <>
            <WorkDescription stage={stage} item={item} />
            {!work && (
              <>
                <h1>{run.name}</h1>
                <details className="run-brief">
                  <summary>Creative brief</summary>
                  <p className="prose">{run.brief}</p>
                </details>
              </>
            )}
            {!readOnly && stage && <ItemEditor key={stage.id} scope={scope} stage={stage} />}
            <div className="view-tabs">
              <Tabs
                label="Workspace"
                items={[
                  { id: "discussion", label: "Conversation" },
                  { id: "results", label: "Artifacts" },
                ]}
                activeId={tab}
                onChange={(id) =>
                  setTab(id as "discussion" | "results")
                }
              />
            </div>
            <div className="conversation-panel" hidden={tab !== "discussion"}>
              <Discussion
                readOnly={readOnly}
                stageName={stage?.name}
                context={{
                  ...scope,
                  label: [
                    run.name,
                    stage?.name,
                    item?.name,
                  ]
                    .filter(Boolean)
                    .join(" / "),
                  artifact_version_ids: shown.map((s) => s.version.id),
                }}
                orchestrator={run.orchestrator_session_id}
                items={stage?.items}
                onNavigate={onSelect}
                ready={ready}
                active={tab === "discussion"}
              />
            </div>
            <div className="artifacts-panel" hidden={tab !== "results"}>
              <Artifacts
                readOnly={readOnly}
                key={scopeKey}
                scope={scope}
                itemContexts={stage && !item ? stage.items : []}
                reviewing={work?.status === "awaiting_approval"}
                reviewIDs={
                  work?.status === "awaiting_approval"
                    ? work.artifact_version_ids
                    : []
                }
                onShown={onShown}
              />
            </div>
          </>
        )}
      </main>
      <aside className="workspace-sidebar" aria-label="Review and sessions">
        {!readOnly && !invalid && work && <ReviewActions scope={scope} work={work} canProgress={!!canProgress}
          itemContexts={stage && !item ? stage.items : []} />}
        <CreativeSessionRail readOnly={readOnly} key={run.id} runID={run.id} orchestrator={run.orchestrator_session_id} allowedProfiles={run.allowed_profiles} />
      </aside>
    </div>
  );
}
