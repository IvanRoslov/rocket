import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "../../web/src/components/Button";
import { api, actionPath, base, useCommand } from "./api";
import type { ArtifactDetail, ArtifactVersion, Scope, Work } from "./types";
import { actionsFor } from "./workflow";
import { ErrorNotice } from "./ui";
import { Media } from "./Artifacts";

type Props = {
  scope: Scope;
  work: Work;
  canProgress: boolean;
  itemContexts?: { id: number; name: string }[];
};

// A new scope or submitted revision must never inherit a return draft or outcome.
export function ReviewActions(props: Props) {
  const { scope, work } = props;
  return <ReviewCard key={`${scope.run_id}:${scope.stage_id}:${scope.item_id}:${work.review_revision}`} {...props} />;
}

function ReviewCard({ scope, work, canProgress, itemContexts = [] }: Props) {
  const command = useCommand();
  const inFlight = useRef(false);
  const [returning, setReturning] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [outcome, setOutcome] = useState<{ action: string; reason?: string }>();
  const reviewing = work.status === "awaiting_approval" && !outcome;
  const kind = scope.item_id ? "item" : "stage";
  async function execute(action: string) {
    if (inFlight.current || (outcome && ["approve", "reject"].includes(action)) || (action === "reject" && !feedback.trim())) return;
    inFlight.current = true;
    const reason = action === "reject" ? feedback.trim() : undefined;
    try {
      await command.mutateAsync(async () => {
        await api.post(actionPath(scope), {
          action,
          ...(["approve", "reject"].includes(action) ? { review_revision: work.review_revision } : {}),
          ...(reason ? { reason } : {}),
        });
        // Record the committed response before query refresh; a stale cached
        // snapshot must not make the same decision available again.
        if (action === "approve" || action === "reject") setOutcome({ action, reason });
        else setOutcome(undefined);
      });
    } catch {
      // The mutation exposes errors; keep the draft for an intentional retry.
    } finally {
      inFlight.current = false;
    }
  }
  const labels: Record<string, string> = {
    start: "Start work", approve: `Approve ${kind}`, reject: "Return for changes", reopen: "Reopen for rework",
  };
  return (
    <section className="review-actions" aria-label={`Review ${work.name}`}>
      <h2>{reviewing ? `Review ${kind}: ${work.name}` : work.name}</h2>
      {work.review_revision > 0 && <p>Revision {work.review_revision}</p>}
      {outcome ? (
        <div role="status">
          <p>Revision {work.review_revision} {outcome.action === "approve" ? "approved." : "returned for changes."}</p>
          {outcome.reason && <p className="prose">{outcome.reason}</p>}
        </div>
      ) : !reviewing && (
        <p>{work.status === "completed" ? "Completed. Reopen for rework to revise these results." : work.status === "in_progress" ? "Work is in progress. No review is awaiting a decision." : "Work has not started."}</p>
      )}
      {reviewing && (
        <>
          <p>Review these submitted results:</p>
          {work.artifact_version_ids.length === 0 ? <p>No artifacts were submitted with this review.</p> : (
            <ul>{work.artifact_version_ids.map((id) => <SubmittedArtifact key={id} id={id} itemContexts={itemContexts} />)}</ul>
          )}
        </>
      )}
      <div className="row">
        {actionsFor(work.status, canProgress).filter((action) => !outcome || !["approve", "reject"].includes(action)).map((action) => (
          <Button key={action} variant={action === "approve" ? "primary" : "secondary"} size="sm"
            disabled={command.isPending}
            onClick={(event) => {
              if (event.detail > 1 || inFlight.current) return;
              if (action === "reject") setReturning(true);
              else void execute(action);
            }}>
            {labels[action]}
          </Button>
        ))}
      </div>
      {reviewing && returning && (
        <form className="return-feedback" onSubmit={(event) => { event.preventDefault(); void execute("reject"); }}>
          <label>What to fix<textarea required rows={4} value={feedback} disabled={command.isPending} onChange={(event) => setFeedback(event.target.value)} /></label>
          <div className="row">
            <Button type="submit" variant="primary" size="sm" disabled={command.isPending || !feedback.trim()}>Send feedback and return</Button>
            <Button type="button" variant="secondary" size="sm" disabled={command.isPending} onClick={() => setReturning(false)}>Cancel</Button>
          </div>
        </form>
      )}
      <ErrorNotice error={command.error} />
    </section>
  );
}

function SubmittedArtifact({ id, itemContexts }: { id: number; itemContexts: { id: number; name: string }[] }) {
  const [open, setOpen] = useState(false);
  const version = useQuery({ queryKey: ["creative", "artifact-version", id], queryFn: () => api.get<ArtifactVersion>(`${base}/artifact-versions/${id}`) });
  const detail = useQuery({ queryKey: ["creative", "artifact", version.data?.artifact_id], enabled: !!version.data, queryFn: () => api.get<ArtifactDetail>(`${base}/artifacts/${version.data!.artifact_id}`) });
  const v = version.data, artifact = detail.data?.artifact;
  const item = itemContexts.find((item) => item.id === artifact?.item_id);
  const html = v && /^text\/html(;|$)/.test(v.mime);
  return (
    <li>
      {v && artifact ? <>
        <a href={html ? `${base}/artifact-versions/${id}/view` : v.content_url}
          target={html ? "_blank" : undefined} rel={html ? "noopener noreferrer" : undefined}
          onClick={html ? undefined : (event) => { event.preventDefault(); setOpen(!open); }}
          aria-expanded={html ? undefined : open}>
          {item ? `${item.name} / ` : ""}{artifact.name} · version {v.version_number}
        </a>
        {open && <Media key={id} version={v} />}
      </> : <span>Loading submitted artifact {id}…</span>}
      <ErrorNotice error={version.error ?? detail.error} />
    </li>
  );
}
