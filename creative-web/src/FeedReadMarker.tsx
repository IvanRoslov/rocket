import { useEffect, useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "../../web/src/components/Button";
import { api, base } from "./api";
import { unreadKey } from "./feed";
import type { Scope } from "./types";
import { ErrorNotice } from "./ui";

// Keyed by the displayed scope/filter. Mutation callbacks retain their original
// scope even when navigation or a newer feed response overtakes the request.
export function FeedReadMarker({ scope, active, loaded, through, updatedAt }: {
  scope: Scope; active: boolean; loaded: boolean; through: number; updatedAt: number;
}) {
  const client = useQueryClient();
  const highestRead = useRef<number | undefined>(undefined);
  const pending = useRef(new Set<number>());
  const read = useMutation({
    mutationFn: (through_entry_id: number) => api.post(`${base}/runs/${scope.run_id}/feed/read`, {
      ...(scope.stage_id ? { stage_id: scope.stage_id } : {}), through_entry_id,
    }),
    onSuccess: (_, id) => {
      highestRead.current = Math.max(highestRead.current ?? 0, id);
      void client.invalidateQueries({ queryKey: unreadKey(scope.run_id) });
    },
    onSettled: (_, __, id) => { pending.current.delete(id); },
  });
  const { mutate } = read;
  useEffect(() => {
    if (!active || !loaded || pending.current.has(through) || (highestRead.current !== undefined && through <= highestRead.current)) return;
    pending.current.add(through);
    mutate(through);
    // Failed reads retry on the next polled snapshot, never in a render loop.
  }, [active, loaded, through, updatedAt, mutate]);
  return <>
    <ErrorNotice error={read.error} />
    {read.isError && active && <Button variant="secondary" size="sm" disabled={read.isPending}
      onClick={() => mutate(through)}>Retry marking read</Button>}
  </>;
}
