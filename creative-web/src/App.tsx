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
import { Preview } from "./Preview";
interface Selection {
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
  return {
    project: p.get("project") || undefined,
    run: number("run") ?? (deep ? Number(deep[1]) : undefined),
    stage: number("stage"),
    item: number("item"),
  };
}
export function App() {
  const [selection, setSelection] = useState(readSelection);
  const [view, setView] = useState<"work" | "pipelines" | "new">("work");
  useEffect(() => {
    const pop = () => {
      setSelection(readSelection());
      setView("work");
    };
    window.addEventListener("popstate", pop);
    return () => window.removeEventListener("popstate", pop);
  }, []);
  function select(next: Selection) {
    const p = new URLSearchParams();
    Object.entries(next).forEach(([k, v]) => {
      if (v) p.set(k, String(v));
    });
    window.history.pushState({}, "", `/creative/${p.size ? `?${p}` : ""}`);
    setSelection(next);
    if (view !== "pipelines") setView("work");
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
  const project =
    selection.project ?? run.data?.project_id ?? projects.data?.[0]?.id ?? "";
  const pipelines = useQuery({
    queryKey: ["creative", "pipelines"],
    queryFn: () => api.get<Pipeline[]>(`${base}/pipelines`),
  });
  const runs = useQuery({
    queryKey: ["creative", "runs", project],
    queryFn: () =>
      api.get<Run[]>(`${base}/runs?project_id=${encodeURIComponent(project)}`),
    enabled: !!project,
  });
  const selectedRun = run.data?.project_id === project ? run.data : undefined;
  return (
    <div className="creative-app">
      <header className="topbar">
        <a className="brand" href="/creative/">
          <RocketBrand />
        </a>
        <Badge tone="indigo">Creative</Badge>
        <label className="top-select">
          Project
          <select
            value={project}
            onChange={(e) => select({ project: e.target.value })}
          >
            <option value="" disabled>
              Select project
            </option>
            {projects.data?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label className="top-select">
          Creative work
          <select
            value={selection.run ?? ""}
            onChange={(e) =>
              select({ project, run: Number(e.target.value) || undefined })
            }
          >
            <option value="">Choose work</option>
            {selectedRun &&
              !runs.data?.some((r) => r.id === selectedRun.id) && (
                <option value={selectedRun.id}>{selectedRun.name}</option>
              )}
            {runs.data?.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
              </option>
            ))}
          </select>
        </label>
        <div className="top-actions">
          <Button
            variant="primary"
            size="sm"
            disabled={!project}
            onClick={() => setView("new")}
          >
            New work
          </Button>
          <Button
            variant="secondary"
            size="sm"
            aria-pressed={view === "pipelines"}
            onClick={() => setView("pipelines")}
          >
            Pipelines
          </Button>
          <a href="/" target="_blank" rel="noopener noreferrer">
            Development ↗
          </a>
        </div>
      </header>
      <ErrorNotice
        error={projects.error ?? pipelines.error ?? runs.error ?? run.error}
      />
      {view === "pipelines" ? (
        <Pipelines
          pipelines={pipelines.data ?? []}
          onClose={() => setView("work")}
        />
      ) : projects.isPending ? (
        <main>
          <Empty>Loading projects…</Empty>
        </main>
      ) : projects.data?.length === 0 ? (
        <main>
          <h1>Your creative workspace</h1>
          <p>Create a Rocket project to begin.</p>
          <a href="/projects/new">Create project</a>
        </main>
      ) : view === "new" ? (
        <NewWork
          key={project}
          project={project}
          pipelines={pipelines.data ?? []}
          onCancel={() => setView("work")}
          onCreated={(r) => select({ project, run: r.id })}
          onPipelines={() => setView("pipelines")}
        />
      ) : selectedRun ? (
        <Workspace
          run={selectedRun}
          selection={selection}
          onSelect={(stage, item) =>
            select({ project, run: selectedRun.id, stage, item })
          }
        />
      ) : (
        <main className="welcome">
          <span className="eyebrow">Creative work</span>
          <h1>Make room for the next idea.</h1>
          <p>Choose existing work or start with a brief and a pipeline.</p>
          {run.isPending && selection.run ? (
            <p>Loading work…</p>
          ) : (
            <div className="run-grid">
              {runs.data?.map((r) => (
                <Button
                  variant="secondary"
                  size="sm"
                  className="run-card"
                  key={r.id}
                  onClick={() => select({ project, run: r.id })}
                >
                  <strong>{r.name}</strong>
                  <span>{r.pipeline_name}</span>
                  <small>{r.status.replaceAll("_", " ")}</small>
                </Button>
              ))}
            </div>
          )}
          {!pipelines.isPending && pipelines.data?.length === 0 && (
            <p>
              No pipelines yet. Open Pipelines to define your first creative
              process.
            </p>
          )}
        </main>
      )}
    </div>
  );
}
function NewWork({
  project,
  pipelines,
  onCancel,
  onCreated,
  onPipelines,
}: {
  project: string;
  pipelines: Pipeline[];
  onCancel: () => void;
  onCreated: (run: Run) => void;
  onPipelines: () => void;
}) {
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
            disabled={!pipeline || command.isPending}
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
  const [reset, setReset] = useState(0);
  const [tab, setTab] = useState<"discussion" | "results" | "preview">("discussion");
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
                <span className="eyebrow">Creative brief</span>
                <h1>{run.name}</h1>
                <p className="prose">{run.brief}</p>
              </>
            )}

            <div className="view-tabs">
              <Tabs
                label="Workspace"
                items={[
                  { id: "discussion", label: "Questions & discussion" },
                  { id: "results", label: "Artifacts" },
                  { id: "preview", label: "Landing preview" },
                ]}
                activeId={tab}
                onChange={(id) => setTab(id as "discussion" | "results" | "preview")}
              />
            </div>
            <div hidden={tab !== "discussion"}>
              <Discussion
                context={{
                  ...scope,
                  label: [
                    run.name,
                    stage?.name,
                    item?.name,
                    ...shown.map(
                      (s) =>
                        `${s.item_name ? `${s.item_name} / ` : ""}${s.artifact.name} version ${s.version.version_number}`,
                    ),
                  ]
                    .filter(Boolean)
                    .join(" / "),
                  artifact_version_ids: shown.map((s) => s.version.id),
                }}
                orchestrator={run.orchestrator_session_id}
                ready={ready}
              />
            </div>
            <div hidden={tab !== "results"}>
            {work && (
              <ReviewActions
                scope={scope}
                work={work}
                canProgress={!!canProgress}
                shown={shown}
                ready={ready && tab === "results"}
                onShowReview={() => {
                  setTab("results");
                  setReset((n) => n + 1);
                }}
              />
            )}
              <Artifacts
                key={`${scopeKey}:${reset}`}
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
            {tab === "preview" && <Preview scope={scope} />}
            {stage && <ItemEditor key={stage.id} scope={scope} stage={stage} />}
          </>
        )}
      </main>
    </div>
  );
}
