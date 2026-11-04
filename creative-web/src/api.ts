import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../../web/src/lib/api";
import { onUnauthorized } from "../../web/src/lib/auth";
import type { Scope } from "./types";
export { api };
export const base = "/v1/creative";
export function scopeQuery(scope: Scope): string {
  const p = new URLSearchParams();
  if (scope.stage_id) p.set("stage_id", String(scope.stage_id));
  if (scope.item_id) p.set("item_id", String(scope.item_id));
  return p.toString() ? `?${p}` : "";
}
export function contextPath(scope: Scope, resource: string): string {
  return `${base}/runs/${scope.run_id}/${resource}${scopeQuery(scope)}`;
}
export function actionPath(scope: Scope): string {
  return `${base}/runs/${scope.run_id}/stages/${scope.stage_id}${scope.item_id ? `/items/${scope.item_id}` : ""}/actions`;
}
export async function uploadSource(
  scope: Scope,
  file: File,
  name: string,
): Promise<unknown> {
  const p = new URLSearchParams(scopeQuery(scope).slice(1));
  p.set("name", name);
  p.set("kind", "source");
  p.set("filename", file.name);
  const res = await fetch(`${base}/runs/${scope.run_id}/artifacts?${p}`, {
    method: "POST",
    headers: { "Content-Type": file.type || "application/octet-stream" },
    body: file,
  });
  if (!res.ok) {
    if (res.status === 401) onUnauthorized();
    const body = await res.json().catch(() => null);
    throw new ApiError(
      res.status,
      body?.error?.code ?? "upload_failed",
      body?.error?.message ?? `Upload failed (${res.status})`,
    );
  }
  return res.json();
}
export function useCommand() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (command: () => Promise<unknown>) => command(),
    onSuccess: () => client.invalidateQueries({ queryKey: ["creative"] }),
  });
}
