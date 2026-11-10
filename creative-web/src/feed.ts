import { useQuery, type QueryClient } from "@tanstack/react-query";
import { api, contextPath } from "./api";
import { ApiError } from "../../web/src/lib/api";
import { onUnauthorized } from "../../web/src/lib/auth";
import type { FeedDecision, FeedEntry, Scope } from "./types";

export const feedKey = (scope: Scope) => ["creative", "feed", scope.run_id, scope.stage_id ?? 0, scope.item_id ?? 0] as const;
export function useFeed(scope: Scope) {
  return useQuery({
    queryKey: feedKey(scope),
    queryFn: () => api.get<FeedEntry[]>(contextPath(scope, "feed")),
    refetchInterval: 2000,
  });
}

// Update every cached view of this feed, including matching item filters.
export async function cacheFeedEntry(client: QueryClient, entry: FeedEntry) {
  const queryKey = ["creative", "feed", entry.run_id, entry.stage_id || 0];
  await client.cancelQueries({ queryKey });
  for (const [key, entries] of client.getQueriesData<FeedEntry[]>({ queryKey })) {
    if (key[4] && key[4] !== entry.item_id) continue;
    client.setQueryData(key, [entry, ...(entries ?? []).filter(old => old.id !== entry.id)].sort((a, b) => b.id - a.id));
  }
}

export const cardPath = (runID: number, entryID: number) => `/v1/creative/runs/${runID}/feed/cards/${entryID}`;

export class CardConflict extends ApiError {
  constructor(message: string, public entry: FeedEntry) {
    super(409, "creative_card_conflict", message);
  }
}

// The shared wrapper discards fields outside error. Preserve this contract's
// authoritative conflict entry locally, without changing other applications.
export async function decideCard(runID: number, entryID: number, decision: FeedDecision): Promise<FeedEntry> {
  const response = await fetch(`${cardPath(runID, entryID)}/decision`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision }),
  });
  if (response.status === 401) onUnauthorized();
  const result = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 409 && result?.entry) throw new CardConflict(result.error?.message ?? "Proposal state changed", result.entry);
    throw new ApiError(response.status, result?.error?.code ?? "unknown", result?.error?.message ?? response.statusText);
  }
  return result;
}

export async function refreshFeeds(client: QueryClient, runID: number) {
  await Promise.all([
    client.invalidateQueries({ queryKey: ["creative", "feed", runID] }),
    client.invalidateQueries({ queryKey: ["creative", "feed-unread", runID] }),
  ]);
}
