import { useState } from "react";
import { api, base, useCommand } from "./api";
import type { Item, Run, Scope, Stage } from "./types";
import { statusLabel } from "./workflow";
import { ErrorNotice } from "./ui";
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
      <button
        className="run-heading"
        aria-pressed={!scope.stage_id}
        onClick={() => onSelect()}
      >
        <strong>{run.name}</strong>
        <span>{statusLabel[run.status]}</span>
      </button>
      <details>
        <summary>Creative brief</summary>
        <p className="prose">{run.brief}</p>
      </details>
      <ol className="stages">
        {run.stages.map((stage, i) => (
          <li key={stage.id}>
            <button
              aria-pressed={scope.stage_id === stage.id && !scope.item_id}
              onClick={() => onSelect(stage.id)}
            >
              <span className={`status-dot ${stage.status}`} />
              <span>
                <strong>
                  {i + 1}. {stage.name}
                </strong>
                <small>{statusLabel[stage.status]}</small>
              </span>
            </button>
            {scope.stage_id === stage.id &&
              stage.items.map((item) => (
                <button
                  className="item-link"
                  key={item.id}
                  aria-pressed={scope.item_id === item.id}
                  onClick={() => onSelect(stage.id, item.id)}
                >
                  <span className={`status-dot ${item.status}`} />
                  <span>
                    {item.name}
                    <small>{statusLabel[item.status]}</small>
                  </span>
                </button>
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
      <h1>{work.name}</h1>
      <p className="prose">
        {work.description || "No work description provided."}
      </p>
      <details open>
        <summary>Expected artifacts</summary>
        <p className="prose">
          {work.expected_artifacts || "No expected artifacts specified."}
        </p>
      </details>
      <div className="progress">
        <progress max={100} value={work.progress} />
        <span>
          {work.progress}% · {statusLabel[work.status]}
        </span>
      </div>
      {work.progress_note && <p>{work.progress_note}</p>}
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
              <button
                onClick={() => {
                  setEditing(i);
                  setName(i.name);
                  setDescription(i.description);
                  setExpected(i.expected_artifacts);
                }}
              >
                Edit {i.name}
              </button>
              <button
                disabled={save.isPending}
                onClick={() => save.mutate(() => api.del(`${path}/${i.id}`))}
              >
                Remove {i.name}
              </button>
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
          <button type="submit" disabled={save.isPending}>
            {editing ? "Save item" : "Add item"}
          </button>
          {editing && (
            <button type="button" onClick={reset}>
              Cancel edit
            </button>
          )}
        </div>
        <ErrorNotice error={save.error} />
      </form>
    </details>
  );
}
