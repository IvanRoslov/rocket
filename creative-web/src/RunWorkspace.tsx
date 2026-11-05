import { Dot, type DotState } from "../../web/src/components/Dot";
import { Button } from "../../web/src/components/Button";
import { useState } from "react";
import { api, base, useCommand } from "./api";
import type { Item, Run, Scope, Stage, Status } from "./types";
import { statusLabel } from "./workflow";
import { ErrorNotice } from "./ui";
const dotState: Record<Status, DotState> = {
  pending: "idle",
  in_progress: "spawning",
  awaiting_approval: "waiting_input",
  completed: "ready",
};
export function ProcessRail({
  run,
  scope,
  onSelect,
}: {
  run: Run;
  scope: Scope;
  onSelect: (stage?: number, item?: number) => void;
}) {
  return (
    <aside className="process">
      <span className="eyebrow">Saved process · {run.pipeline_name}</span>
      <Button
        variant="secondary"
        size="sm"
        className="run-heading"
        aria-pressed={!scope.stage_id}
        onClick={() => onSelect()}
      >
        <strong>{run.name}</strong>
        <span>{statusLabel[run.status]}</span>
      </Button>
      <details>
        <summary>Creative brief</summary>
        <p className="prose">{run.brief}</p>
      </details>
      <ol className="stages">
        {run.stages.map((stage, i) => (
          <li key={stage.id}>
            <Button
              variant="secondary"
              size="sm"
              aria-pressed={scope.stage_id === stage.id && !scope.item_id}
              onClick={() => onSelect(stage.id)}
            >
              <Dot state={dotState[stage.status]} />
              <span>
                <strong>
                  {i + 1}. {stage.name}
                </strong>
                <small>{statusLabel[stage.status]}</small>
              </span>
            </Button>
            {scope.stage_id === stage.id &&
              stage.items.map((item) => (
                <Button
                  variant="secondary"
                  size="sm"
                  className="item-link"
                  key={item.id}
                  aria-pressed={scope.item_id === item.id}
                  onClick={() => onSelect(stage.id, item.id)}
                >
                  <Dot state={dotState[item.status]} />
                  <span>
                    {item.name}
                    <small>{statusLabel[item.status]}</small>
                  </span>
                </Button>
              ))}
          </li>
        ))}
      </ol>
    </aside>
  );
}
export function WorkDescription({
  stage,
  item,
}: {
  stage?: Stage;
  item?: Item;
}) {
  const work = item ?? stage;
  if (!work) return null;
  return (
    <div className="work-description">
      <span className="eyebrow">
        {item ? "Concrete item" : "Saved stage"}
        {stage
          ? ` · ${stage.approval === "auto" ? "Automatic progression" : stage.approval === "item" ? "Approval for each item" : "Approval for whole stage"}`
          : ""}
      </span>
      <div className="work-heading">
        <h1>{work.name}</h1>
        <div className="progress">
          <progress aria-label="Work progress" max={100} value={work.progress} />
          <span>{work.progress}% · {statusLabel[work.status]}</span>
        </div>
      </div>
      <details>
        <summary>Work details</summary>
        <p className="prose">
          {work.description || "No work description provided."}
        </p>
        <h3>Expected artifacts</h3>
        <p className="prose">
          {work.expected_artifacts || "No expected artifacts specified."}
        </p>
        {work.progress_note && <p className="prose">{work.progress_note}</p>}
      </details>
    </div>
  );
}
export function ItemEditor({ scope, stage }: { scope: Scope; stage: Stage }) {
  const [editing, setEditing] = useState<Item | null>(null);
  const [name, setName] = useState(""),
    [description, setDescription] = useState(""),
    [expected, setExpected] = useState("");
  const save = useCommand();
  function reset() {
    setEditing(null);
    setName("");
    setDescription("");
    setExpected("");
  }
  const path = `${base}/runs/${scope.run_id}/stages/${stage.id}/items`;
  if (!stage.repeated || !["pending", "in_progress"].includes(stage.status))
    return null;
  return (
    <details className="item-editor">
      <summary>Manage {stage.name} items</summary>
      <ul>
        {stage.items
          .filter((i) => i.status === "pending")
          .map((i) => (
            <li key={i.id}>
              {i.name}
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setEditing(i);
                  setName(i.name);
                  setDescription(i.description);
                  setExpected(i.expected_artifacts);
                }}
              >
                Edit {i.name}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={save.isPending}
                onClick={() => save.mutate(() => api.del(`${path}/${i.id}`))}
              >
                Remove {i.name}
              </Button>
            </li>
          ))}
      </ul>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const body = { name, description, expected_artifacts: expected };
          save.mutate(
            () =>
              editing
                ? api.patch(`${path}/${editing.id}`, body)
                : api.post(path, body),
            { onSuccess: reset },
          );
        }}
      >
        <h3>{editing ? "Edit pending item" : "Add item"}</h3>
        <label>
          Item name
          <input
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label>
          Item work
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>
        <label>
          Item expected artifacts
          <textarea
            value={expected}
            onChange={(e) => setExpected(e.target.value)}
          />
        </label>
        <div className="row">
          <Button
            variant="secondary"
            size="sm"
            type="submit"
            disabled={save.isPending}
          >
            {editing ? "Save item" : "Add item"}
          </Button>
          {editing && (
            <Button
              variant="secondary"
              size="sm"
              type="button"
              onClick={reset}
            >
              Cancel edit
            </Button>
          )}
        </div>
        <ErrorNotice error={save.error} />
      </form>
    </details>
  );
}
