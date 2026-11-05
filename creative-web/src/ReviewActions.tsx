import { useEffect, useRef, useState } from "react";
import { Button } from "../../web/src/components/Button";
import { api, actionPath, useCommand } from "./api";
import type { Scope, ShownVersion, Work } from "./types";
import { actionsFor } from "./workflow";
import { ErrorNotice } from "./ui";
export function ReviewActions({
  scope,
  work,
  canProgress,
  shown,
  ready,
  onShowReview,
}: {
  scope: Scope;
  work: Work;
  canProgress: boolean;
  shown: ShownVersion[];
  ready: boolean;
  onShowReview: () => void;
}) {
  const command = useCommand();
  const inFlight = useRef(false);
  const [confirmation, setConfirmation] = useState<{ key: string; versions: ShownVersion[] } | null>(null);
  const resultIDs = shown
    .filter((s) => s.artifact.kind === "result")
    .map((s) => s.version.id);
  const review = work.artifact_version_ids ?? [];
  const shownIDs = shown.map((s) => s.version.id);
  const bound =
    ready &&
    review.every((id) => shownIDs.includes(id)) &&
    resultIDs.every((id) => review.includes(id));
  // The confirmation is valid only for the exact scope, state and displayed set.
  const confirmationKey = JSON.stringify([scope, work.id, work.name, work.status,
    work.review_revision, review, ready, canProgress, shown]);
  useEffect(() => { setConfirmation(null); }, [confirmationKey]);
  const pendingConfirmation = confirmation?.key === confirmationKey ? confirmation : null;
  async function execute(body: object) {
    if (inFlight.current) return;
    inFlight.current = true;
    setConfirmation(null);
    try {
      // mutateAsync includes useCommand's awaited query invalidation.
      await command.mutateAsync(() => api.post(actionPath(scope), body));
    } catch {
      // useCommand exposes the failure through ErrorNotice; never retry a transition.
    } finally {
      inFlight.current = false;
    }
  }
  const reviewing = work.status === "awaiting_approval";
  const labels: Record<string, string> = {
    start: "Start work",
    submit: "Submit shown results",
    approve: "Approve shown results",
    reject: "Request changes",
    reopen: "Reopen for rework",
  };
  return (
    <section className="review-actions">
      {reviewing && (
        <>
          <h2>Review submitted results</h2>
          <p>Approval applies to this submitted set:</p>
          {review.length === 0 ? (
            <p>No artifacts were submitted with this review.</p>
          ) : (
            <ul>
              {review.map((id) => {
                const entry = shown.find((s) => s.version.id === id);
                return (
                  <li key={id}>
                    {entry
                      ? `${entry.item_name ? `${entry.item_name} / ` : ""}${entry.artifact.name} · version ${entry.version.version_number}`
                      : "Submitted artifact — load its version to inspect"}
                  </li>
                );
              })}
            </ul>
          )}
          {!bound && (
            <>
              <p>
                Return to the submitted versions before approving or requesting
                changes.
              </p>
              <Button
                variant="secondary"
                size="sm"
                onClick={onShowReview}>Show submitted versions</Button
              >
            </>
          )}
        </>
      )}
      {work.status === "completed" && (
        <p>
          Reopen this {scope.item_id ? "item" : "stage"} to revise it. Later
          stages and their results are preserved. This work will need approval
          again according to its saved process.
        </p>
      )}
      <div className="row">
        {actionsFor(work.status, canProgress).map((action) => (
          <Button
            variant={action === "approve" ? "primary" : "secondary"}
            size="sm"
            key={action}
            disabled={
              command.isPending ||
              (["approve", "reject"].includes(action) && !bound) ||
              (action === "submit" && !ready)
            }
            onClick={(event) => {
              if (event.detail > 1 || inFlight.current) return;
              if (action === "submit") {
                setConfirmation({ key: confirmationKey, versions: shown.filter((s) => s.artifact.kind === "result") });
                return;
              }
              void execute(["approve", "reject"].includes(action)
                ? { action, review_revision: work.review_revision }
                : { action });
            }}
          >
            {labels[action]}
          </Button>
        ))}
      </div>
      {pendingConfirmation && (
        <div role="dialog" aria-modal="false" aria-label="Confirm submission">
          <h2>Submit {scope.item_id ? "item" : "stage"}: {work.name}?</h2>
          <p>Submit these exact shown result versions:</p>
          {pendingConfirmation.versions.length === 0 ? <p>No result versions are shown. Submit an empty result set?</p> : (
            <ul>{pendingConfirmation.versions.map((entry) => (
              <li key={entry.version.id}>
                {entry.item_name ? `${entry.item_name} / ` : ""}{entry.artifact.name} · version {entry.version.version_number} · ID {entry.version.id}
              </li>
            ))}</ul>
          )}
          <Button variant="primary" size="sm" disabled={command.isPending} onClick={(event) => {
            if (event.detail > 1 || inFlight.current || !ready) return;
            void execute({ action: "submit", artifact_version_ids: pendingConfirmation.versions.map((s) => s.version.id) });
          }}>Confirm submission</Button>
          <Button variant="secondary" size="sm" onClick={() => setConfirmation(null)}>Cancel</Button>
        </div>
      )}
      <ErrorNotice error={command.error} />
    </section>
  );
}
