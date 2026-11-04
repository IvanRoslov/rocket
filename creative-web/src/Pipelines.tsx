import { Button } from "../../web/src/components/Button";
import { useState } from "react";
import { api, base, useCommand } from "./api";
import type { Approval, Pipeline, StageDefinition } from "./types";
import { moveStage } from "./workflow";
import { ErrorNotice } from "./ui";
const blank = (): StageDefinition => ({
  name: "",
  description: "",
  expected_artifacts: "",
  repeated: false,
  approval: "stage",
});
export function Pipelines({
  project,
  pipelines,
  onClose,
}: {
  project: string;
  pipelines: Pipeline[];
  onClose: () => void;
}) {
  const [id, setID] = useState<number>();
  const [name, setName] = useState("");
  const [stages, setStages] = useState<StageDefinition[]>([blank()]);
  const save = useCommand();
  function choose(p?: Pipeline) {
    setID(p?.id);
    setName(p?.name ?? "");
    setStages(p ? p.stages.map((s) => ({ ...s })) : [blank()]);
    save.reset();
  }
  function edit(index: number, patch: Partial<StageDefinition>) {
    setStages(stages.map((s, i) => (i === index ? { ...s, ...patch } : s)));
  }
  return (
    <section className="page-panel">
      <div className="heading">
        <div>
          <span className="eyebrow">Process library</span>
          <h1>Pipelines</h1>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={onClose}>Back to workspace</Button
        >
      </div>
      <p>
        Templates define new work. Runs already underway keep their saved
        process.
      </p>
      <div className="pipeline-layout">
        <nav aria-label="Pipeline templates">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => choose()}>+ New pipeline</Button
          >
          {pipelines.map((p) => (
            <Button
              variant="secondary"
              size="sm"
              key={p.id}
              aria-pressed={id === p.id}
              onClick={() => choose(p)}
            >
              {p.name}
            </Button>
          ))}
        </nav>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const body = { project_id: project, name, stages };
            save.mutate(
              () =>
                id
                  ? api.put(`${base}/pipelines/${id}`, body)
                  : api.post(`${base}/pipelines`, body),
              { onSuccess: () => onClose() },
            );
          }}
        >
          <label>
            Pipeline name
            <input
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          {stages.map((stage, i) => (
            <fieldset key={i}>
              <legend>Stage {i + 1}</legend>
              <div className="row">
                <Button
                  variant="secondary"
                  size="sm"
                  type="button"
                  aria-label={`Move stage ${i + 1} up`}
                  disabled={i === 0}
                  onClick={() => setStages(moveStage(stages, i, -1))}
                >
                  ↑
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  type="button"
                  aria-label={`Move stage ${i + 1} down`}
                  disabled={i === stages.length - 1}
                  onClick={() => setStages(moveStage(stages, i, 1))}
                >
                  ↓
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  type="button"
                  disabled={stages.length === 1}
                  onClick={() => setStages(stages.filter((_, n) => n !== i))}
                >
                  Remove stage
                </Button>
              </div>
              <label>
                Stage {i + 1} name
                <input
                  required
                  value={stage.name}
                  onChange={(e) => edit(i, { name: e.target.value })}
                />
              </label>
              <label>
                Stage {i + 1} work
                <textarea
                  value={stage.description}
                  onChange={(e) => edit(i, { description: e.target.value })}
                />
              </label>
              <label>
                Stage {i + 1} expected artifacts
                <textarea
                  value={stage.expected_artifacts}
                  onChange={(e) =>
                    edit(i, { expected_artifacts: e.target.value })
                  }
                />
              </label>
              <label className="check">
                <input
                  type="checkbox"
                  checked={stage.repeated}
                  onChange={(e) =>
                    edit(i, {
                      repeated: e.target.checked,
                      ...(!e.target.checked && stage.approval === "item"
                        ? { approval: "stage" as const }
                        : {}),
                    })
                  }
                />
                Stage {i + 1} repeated items
              </label>
              <label>
                Stage {i + 1} approval
                <select
                  value={stage.approval}
                  onChange={(e) =>
                    edit(i, { approval: e.target.value as Approval })
                  }
                >
                  <option value="auto">Automatic progression</option>
                  <option value="stage">Human approval — whole stage</option>
                  {stage.repeated && (
                    <option value="item">Human approval — each item</option>
                  )}
                </select>
              </label>
            </fieldset>
          ))}
          <div className="row">
            <Button
              variant="secondary"
              size="sm"
              type="button"
              onClick={() => setStages([...stages, blank()])}
            >
              Add stage
            </Button>
            <Button
              variant="primary"
              size="sm"
              disabled={save.isPending}
              type="submit"
            >
              {save.isPending ? "Saving…" : "Save pipeline"}
            </Button>
          </div>
          <ErrorNotice error={save.error} />
        </form>
      </div>
    </section>
  );
}
