import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { Discussion } from "./Discussion";
import { ProcessRail } from "./RunWorkspace";
import type { FeedEntry, Run } from "./types";
const entry = (id: number, stage_id = 4, item_id = 0): FeedEntry => ({
  id, run_id: 7, stage_id, item_id, kind: "message", author: "orchestrator", author_session_id: "creative-7", body: `New ${id}`,
  artifact_version_ids: [], created_at: 1794340000 + id, card: null,
});
const work = { description: "", expected_artifacts: "", status: "in_progress" as const, progress: 0, progress_note: "", review_revision: 0, artifact_version_ids: [] };
const run: Run = { id: 7, name: "Film", project_id: "film", pipeline_id: 1, pipeline_name: "Film process", brief: "", status: "in_progress", orchestrator_session_id: "", stages: [4, 8].map(id => ({ ...work, id, run_id: 7, position: id, name: `Stage ${id}`, repeated: false, approval: "stage", items: [] })) };
const server = setupServer(
  http.get("/v1/creative/runs/:run/questions", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/feed/unread", () => HttpResponse.json({ run_id: 7, total: 0, run_feed: 0, stages: [{ stage_id: 4, unread: 0 }, { stage_id: 8, unread: 0 }] })),
  http.post("/v1/creative/runs/7/feed/read", async ({ request }) => HttpResponse.json(await request.json())),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => { server.resetHandlers(); });
function open(stage_id?: number, active = true, item_id?: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const tree = (stage = stage_id, visible = active, item = item_id, selectedRun = 7) => <QueryClientProvider client={client}>
    <ProcessRail run={{ ...run, id: selectedRun }} scope={{ run_id: selectedRun, stage_id: stage, item_id: item }} onSelect={() => {}} />
    <Discussion context={{ run_id: selectedRun, stage_id: stage, item_id: item, label: `Stage ${stage ?? "run"}`, artifact_version_ids: [] }} ready orchestrator="" active={visible} />
  </QueryClientProvider>;
  return { ...render(tree()), tree, client };
}
it("shows run total and per-stage badges, then clears only the opened stage through its highest displayed ID", async () => {
  const reads: unknown[] = [];
  let cleared = false;
  server.use(
    http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([entry(43), entry(41)])),
    http.get("/v1/creative/runs/7/feed/unread", () => HttpResponse.json({ run_id: 7, total: cleared ? 2 : 4, run_feed: 1, stages: [{ stage_id: 4, unread: cleared ? 0 : 2 }, { stage_id: 8, unread: 1 }] })),
    http.post("/v1/creative/runs/7/feed/read", async ({ request }) => { reads.push(await request.json()); cleared = true; return HttpResponse.json({ stage_id: 4, through_entry_id: 43 }); }),
  );
  const view = open(4, false);
  const rail = screen.getByRole("complementary");
  expect(await within(rail).findByLabelText("4 unread messages")).toBeVisible();
  expect(within(rail).getByRole("button", { name: /1. Stage 4/ })).toContainElement(screen.getByLabelText("2 unread messages"));
  expect(reads).toEqual([]);
  view.rerender(view.tree(4, true));
  await waitFor(() => expect(reads).toEqual([{ stage_id: 4, through_entry_id: 43 }]));
  await waitFor(() => expect(within(rail).getByRole("button", { name: /1. Stage 4/ })).not.toHaveTextContent("2"));
  expect(within(rail).getByRole("button", { name: /Film/ })).toContainElement(screen.getByLabelText("2 unread messages"));
  expect(within(rail).getByRole("button", { name: /2. Stage 8/ })).toContainElement(screen.getByLabelText("1 unread messages"));
});
it("marks filtered item view read in the containing stage through the displayed ID, and an empty run through zero", async () => {
  const reads: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7/feed", ({ request }) => {
      const p = new URL(request.url).searchParams;
      return HttpResponse.json(!p.has("stage_id") ? [] : p.has("item_id") ? [entry(41, 4, 5)] : [entry(43, 4, 6), entry(41, 4, 5)]);
    }),
    http.post("/v1/creative/runs/7/feed/read", async ({ request }) => { reads.push(await request.json()); return HttpResponse.json({}); }),
  );
  const view = open(4, true, 5);
  await waitFor(() => expect(reads).toContainEqual({ stage_id: 4, through_entry_id: 43 }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Only this item" }));
  await waitFor(() => expect(reads).toContainEqual({ stage_id: 4, through_entry_id: 41 }));
  expect(reads.every(value => !("item_id" in (value as object)))).toBe(true);
  view.rerender(view.tree(0, true, 0));
  await waitFor(() => expect(reads).toContainEqual({ through_entry_id: 0 }));
});
it("does not mark a delayed old scope response read as the new feed", async () => {
  const reads: unknown[] = [];
  let release!: () => void;
  server.use(
    http.get("/v1/creative/runs/7/feed", async ({ request }) => {
      const stage = Number(new URL(request.url).searchParams.get("stage_id"));
      if (stage === 4) await new Promise<void>(resolve => { release = resolve; });
      return HttpResponse.json([entry(stage === 4 ? 43 : 80, stage)]);
    }),
    http.post("/v1/creative/runs/7/feed/read", async ({ request }) => { reads.push(await request.json()); return HttpResponse.json({}); }),
  );
  const view = open(4);
  await waitFor(() => expect(release).toBeTypeOf("function"));
  view.rerender(view.tree(8));
  await waitFor(() => expect(reads).toEqual([{ stage_id: 8, through_entry_id: 80 }]));
  release();
  await view.client.invalidateQueries({ queryKey: ["creative", "feed", 7, 4] });
  expect(reads).toEqual([{ stage_id: 8, through_entry_id: 80 }]);
});
it("polls feed/unread at two seconds, advances read only to displayed entries, and recovers from read failure", async () => {
  let entries = [entry(43)], feedReads = 0, unreadReads = 0, fail = true;
  const marks: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7/feed", () => { feedReads++; return HttpResponse.json(entries); }),
    http.get("/v1/creative/runs/7/feed/unread", () => { unreadReads++; return HttpResponse.json({ run_id: 7, total: 1, run_feed: 0, stages: [{ stage_id: 4, unread: 1 }, { stage_id: 8, unread: 0 }] }); }),
    http.post("/v1/creative/runs/7/feed/read", async ({ request }) => { marks.push(await request.json()); return fail ? HttpResponse.json({ error: { code: "offline", message: "Read marker failed" } }, { status: 503 }) : HttpResponse.json({}); }),
  );
  open(4);
  await screen.findByText("Read marker failed");
  expect(marks).toHaveLength(1);
  entries = [entry(45), entry(43)]; fail = false;
  expect(await screen.findByText("New 45", {}, { timeout: 3500 })).toBeVisible();
  await waitFor(() => expect(marks).toContainEqual({ stage_id: 4, through_entry_id: 45 }));
  expect(feedReads).toBeGreaterThanOrEqual(2);
  expect(unreadReads).toBeGreaterThanOrEqual(2);
  await waitFor(() => expect(screen.queryByText("Read marker failed")).not.toBeInTheDocument());
});
it("does not mark hidden feeds read when polled entries arrive; opening marks the latest ID", async () => {
  const marks: unknown[] = [];
  let entries = [entry(43)];
  server.use(
    http.get("/v1/creative/runs/7/feed", () => HttpResponse.json(entries)),
    http.post("/v1/creative/runs/7/feed/read", async ({ request }) => { marks.push(await request.json()); return HttpResponse.json({}); }),
  );
  const view = open(4, false);
  await screen.findByText("New 43");
  entries = [entry(46), entry(43)];
  await act(() => view.client.invalidateQueries({ queryKey: ["creative", "feed", 7] }));
  expect(marks).toEqual([]);
  view.rerender(view.tree(4, true));
  await waitFor(() => expect(marks).toEqual([{ stage_id: 4, through_entry_id: 46 }]));
});
