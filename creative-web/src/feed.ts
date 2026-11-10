import { useQuery, type QueryClient } from "@tanstack/react-query";
import { api, contextPath } from "./api";
import type { FeedEntry, Scope } from "./types";

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
