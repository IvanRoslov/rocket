import { useQuery } from "@tanstack/react-query";
import { api, base } from "./api";
import type { PreviewAccess, Scope } from "./types";
import { Empty, ErrorNotice } from "./ui";
export function Preview({ scope }: { scope: Scope }) {
  const p = new URLSearchParams({ run_id: String(scope.run_id) });
  if (scope.stage_id) p.set("stage_id", String(scope.stage_id));
  if (scope.item_id) p.set("item_id", String(scope.item_id));
  const list = useQuery({
    queryKey: ["creative", "previews", scope],
    queryFn: () => api.get<PreviewAccess[]>(`${base}/previews?${p}`),
  });
  const setup = useQuery({
    queryKey: ["creative", "preview-setup"],
    queryFn: () =>
      api.get<{
        configured: boolean;
        message?: string;
        allowed_ports: number[];
      }>(`${base}/previews/setup`),
  });
  return (
    <section className="preview">
      <h2>Interactive landing preview</h2>
      <ErrorNotice error={list.error ?? setup.error} />
      {list.isPending && <p>Checking preview availability…</p>}
      {list.data?.length === 0 && (
        <Empty>
          {setup.data?.message ||
            "No preview is registered for this work. Ask the orchestrator to register and start a preview."}
        </Empty>
      )}
      {list.data?.map((access) => (
        <article key={access.id}>
          {access.status === "ready" && access.url ? (
            <>
              <div className="row">
                <span className="chip">Live preview</span>
                <a href={access.url} target="_blank" rel="noopener noreferrer">
                  Open preview in new tab
                </a>
              </div>
              <iframe
                title="Interactive landing preview"
                src={access.url}
                sandbox="allow-scripts allow-forms allow-same-origin"
              />
            </>
          ) : (
            <Empty>
              {access.message ||
                setup.data?.message ||
                "Preview is unavailable. Ask the orchestrator to restart and register the preview."}
            </Empty>
          )}
        </article>
      ))}
    </section>
  );
}
