import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "../../web/src/components/Button";
import { api, base } from "./api";
import type { Run } from "./types";
import { ErrorNotice } from "./ui";

export function FinalAcceptance({ run }: { run: Run }) {
  const client = useQueryClient();
  const accept = useMutation({
    mutationFn: () => api.post<Run>(`${base}/runs/${run.id}/accept`, {}),
    onSuccess: updated => client.setQueryData(["creative", "run", run.id], updated),
    // Failure can follow a committed freeze or partial stop: reload for retry.
    onSettled: () => client.invalidateQueries({ queryKey: ["creative"] }),
  });
  if (run.closure_state === "accepted") return (
    <section className="final-acceptance accepted" role="status">
      <strong>Work accepted · Permanent archive</strong>
      <p>Agents stopped. This work is read-only. History and published results remain available.</p>
    </section>
  );
  const closing = run.closure_state === "closing";
  if (!closing && (!run.stages.length || run.stages.some(s => s.status !== "completed"))) return null;
  return (
    <section className="final-acceptance" aria-label="Final acceptance">
      <h2>{closing ? "Closure incomplete" : "Ready for final acceptance"}</h2>
      <p>Accepting permanently closes this work and stops its agents. History and published results are retained. This cannot be undone.</p>
      {closing && <p>Work is frozen. Retry closure to stop remaining agents and save the archive.</p>}
      <ErrorNotice error={accept.error ?? run.closure_error} />
      <Button variant="primary" disabled={accept.isPending} onClick={() => accept.mutate()}>
        {accept.isPending ? "Closing work…" : closing ? "Retry closure" : "Accept all work"}
      </Button>
    </section>
  );
}
