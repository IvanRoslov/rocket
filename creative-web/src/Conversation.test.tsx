import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it } from "vitest";
import { Discussion } from "./Discussion";

const entry = (id: number, item_id = 0, author = "orchestrator") => ({
  id, run_id: 7, stage_id: 4, item_id, kind: "message", author,
  author_session_id: author === "human" ? "" : "creative-7", body: `Message ${id}`,
  artifact_version_ids: [], created_at: 1794340000 + id, card: null,
});
const server = setupServer(
  http.post("/v1/creative/runs/:run/feed/read", async ({ request }) => HttpResponse.json(await request.json())),
  http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([])),
  http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([])),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
function open(stage?: number, item?: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const tree = (stage_id = stage, item_id = item) => (
    <QueryClientProvider client={client}>
      <Discussion context={{ run_id: 7, stage_id, item_id, label: `Context ${stage_id ?? "run"}/${item_id ?? "all"}`, artifact_version_ids: [101] }}
        orchestrator="" ready items={[{ id: 5, name: "Opening scene" }, { id: 6, name: "Final scene" }]} />
    </QueryClientProvider>
  );
  return { ...render(tree()), tree, client };
}
it("puts chronological bubbles above the bottom composer and collapses human context", async () => {
  server.use(
    http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([
      entry(3), { ...entry(5, 5, "human"), artifact_version_ids: [101] }, entry(4, 6),
    ])),
    http.get("/v1/creative/artifact-versions/101", () => HttpResponse.json({ id: 101, artifact_id: 10, version_number: 2, filename: "scene.png", content_url: "/content/101" })),
    http.get("/v1/creative/artifacts/10", () => HttpResponse.json({ artifact: { name: "Storyboard" }, versions: [] })),
  );
  open(4);
  const list = await screen.findByRole("list", { name: "Conversation entries" });
  const cards = await within(list).findAllByRole("article");
  expect(cards.map((c) => within(c).getByText(/Message \d/).textContent)).toEqual(["Message 3", "Message 4", "Message 5"]);
  expect(screen.getByLabelText("Message").compareDocumentPosition(list) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
  expect(within(cards[2]).getByText("You")).toBeVisible();
  expect(within(cards[1]).getByText("Orchestrator")).toBeVisible();
  expect(within(cards[2]).getByText(/About: Opening scene/)).toBeVisible();
  expect(cards[2].querySelector("time")).toHaveAttribute("dateTime", new Date(1794340005 * 1000).toISOString());
  expect(screen.queryByRole("link", { name: "Storyboard · version 2" })).not.toBeInTheDocument();
  await userEvent.click(within(cards[2]).getByText(/About: Opening scene/));
  expect(await screen.findByRole("link", { name: "Storyboard · version 2" })).toHaveAttribute("href", "/content/101");
});
it("reads a run feed, a whole stage for an item, then the explicit item filter; archive keeps exact item scope", async () => {
  const reads: string[] = [], archives: string[] = [], writes: unknown[] = [];
  server.use(
    http.get("/v1/creative/runs/7/feed", ({ request }) => { reads.push(new URL(request.url).search); return HttpResponse.json([]); }),
    http.get("/v1/creative/runs/7/questions", ({ request }) => { archives.push(new URL(request.url).search); return HttpResponse.json([]); }),
    http.post("/v1/creative/runs/7/feed", async ({ request }) => { writes.push(await request.json()); return HttpResponse.json(entry(9, 5, "human"), { status: 201 }); }),
  );
  const view = open();
  await screen.findByText("No messages in this feed.");
  expect(reads).toContain("");
  view.rerender(view.tree(4, 5));
  await waitFor(() => expect(reads).toContain("?stage_id=4"));
  await waitFor(() => expect(archives).toContain("?stage_id=4&item_id=5"));
  await userEvent.click(screen.getByRole("checkbox", { name: "Only this item" }));
  await waitFor(() => expect(reads).toContain("?stage_id=4&item_id=5"));
  await userEvent.type(screen.getByLabelText("Message"), "Change the scene");
  await userEvent.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() => expect(writes).toEqual([{ stage_id: 4, item_id: 5, artifact_version_ids: [101], body: "Change the scene" }]));
  view.rerender(view.tree(4, 6));
  expect(screen.getByRole("checkbox", { name: "Only this item" })).not.toBeChecked();
});
it("keeps failed drafts and original item/version target, then discards to compose for the new selection", async () => {
  const writes: unknown[] = [];
  server.use(http.post("/v1/creative/runs/7/feed", async ({ request }) => {
    writes.push(await request.json());
    return HttpResponse.json({ error: { code: "offline", message: "Could not send" } }, { status: 503 });
  }));
  const view = open(4, 5);
  await userEvent.type(screen.getByLabelText("Message"), "Keep my draft");
  view.rerender(view.tree(8, 0));
  await userEvent.click(screen.getByRole("button", { name: "Send message" }));
  await screen.findByText("Could not send");
  expect(screen.getByLabelText("Message")).toHaveValue("Keep my draft");
  expect(writes).toEqual([{ stage_id: 4, item_id: 5, artifact_version_ids: [101], body: "Keep my draft" }]);
  await userEvent.click(screen.getByRole("button", { name: "Discard draft" }));
  expect(screen.getByLabelText("Message")).toHaveValue("");
  await userEvent.type(screen.getByLabelText("Message"), "New target");
  await userEvent.click(screen.getByRole("button", { name: "Send message" }));
  await waitFor(() => expect(writes[1]).toEqual({ stage_id: 8, artifact_version_ids: [101], body: "New target" }));
});
it("keeps old questions collapsed under the feed and never offers human question creation", async () => {
  server.use(http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([
    { id: 21, title: "Old decision", body: "Original proposal", status: "open", options: ["Go"], your_turn: true, asked_by: "creative-7", artifact_version_ids: [], messages: [] },
  ])));
  open(4);
  const summary = screen.getByText("Old questions");
  expect(summary.closest("details")).not.toHaveAttribute("open");
  await waitFor(() => expect(screen.queryByText("Loading questions…")).not.toBeInTheDocument());
  expect(screen.getByText("Original proposal")).not.toBeVisible();
  expect(screen.queryByRole("button", { name: "Open question" })).not.toBeInTheDocument();
  await userEvent.click(summary);
  expect(await screen.findByRole("button", { name: "Go" })).toBeVisible();
});

it("inserts a sent message immediately even if the following feed refresh fails", async () => {
  let sent = false;
  server.use(
    http.get("/v1/creative/runs/7/feed", () => sent ? HttpResponse.json({ error: { code: "offline", message: "Refresh unavailable" } }, { status: 503 }) : HttpResponse.json([])),
    http.post("/v1/creative/runs/7/feed", () => { sent = true; return HttpResponse.json({ ...entry(10, 0, "human"), body: "Saved while offline" }, { status: 201 }); }),
  );
  open(4);
  await screen.findByText("No messages in this feed.");
  await userEvent.type(screen.getByLabelText("Message"), "Saved while offline");
  await userEvent.click(screen.getByRole("button", { name: "Send message" }));
  expect(await screen.findByText("Saved while offline", { selector: ".markdown p" })).toBeVisible();
  await waitFor(() => expect(screen.getByLabelText("Message")).toHaveValue(""));
});

it("preserves scroll and draft on polling above the bottom and offers the new-message pill", async () => {
  server.use(http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([entry(3), entry(4)])));
  const view = open(4);
  await screen.findByRole("article", { name: "Message 4" });
  const scroll = screen.getByRole("region", { name: "Chat history" });
  Object.defineProperties(scroll, { scrollHeight: { configurable: true, value: 1000 }, clientHeight: { configurable: true, value: 200 } });
  scroll.scrollTop = 100;
  fireEvent.scroll(scroll);
  await userEvent.type(screen.getByLabelText("Message"), "A draft");
  act(() => view.client.setQueryData(["creative", "feed", 7, 4, 0], [entry(5), entry(4), entry(3)]));
  expect(scroll.scrollTop).toBe(100);
  expect(screen.getByLabelText("Message")).toHaveValue("A draft");
  await userEvent.click(await screen.findByRole("button", { name: "1 new ↓" }));
  expect(scroll.scrollTop).toBe(1000);
  expect(screen.queryByRole("button", { name: /new ↓/ })).not.toBeInTheDocument();
  act(() => view.client.setQueryData(["creative", "feed", 7, 4, 0], [entry(6), entry(5), entry(4), entry(3)]));
  await waitFor(() => expect(scroll.scrollTop).toBe(1000));
});
it.each(["Control", "Meta"])("sends with %s+Enter and retains the attachment target", async (modifier) => {
  const writes: unknown[] = [];
  server.use(http.post("/v1/creative/runs/7/feed", async ({ request }) => { writes.push(await request.json()); return HttpResponse.json(entry(10, 5, "human")); }));
  open(4, 5);
  await userEvent.type(screen.getByLabelText("Message"), "Send by keyboard");
  await userEvent.keyboard(`{${modifier}>}{Enter}{/${modifier}}`);
  await waitFor(() => expect(writes).toEqual([{ stage_id: 4, item_id: 5, artifact_version_ids: [101], body: "Send by keyboard" }]));
});
