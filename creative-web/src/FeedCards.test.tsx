import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, expect, it, vi } from "vitest";
import { Discussion } from "./Discussion";
import type { FeedEntry } from "./types";
const proposal = (id = 40): FeedEntry => ({
  id, run_id: 7, stage_id: 4, item_id: 5, kind: "proposal", author: "orchestrator", author_session_id: "creative-7",
  body: `Proposal **${id}**: two images, model A, $0.20.`, artifact_version_ids: [], created_at: 1794340000,
  card: { status: "pending", decision: "", decided_at: 0, replaces_entry_id: 0, replaced_by_entry_id: 0 },
});
const server = setupServer(
  http.post("/v1/creative/runs/:run/feed/read", async ({ request }) => HttpResponse.json(await request.json())),
  http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([proposal()])),
  http.get("/v1/creative/runs/7/questions", () => HttpResponse.json([])),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
function open(onNavigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const tree = (stage_id = 4, item_id = 5) => <QueryClientProvider client={client}>
    <Discussion context={{ run_id: 7, stage_id, item_id, label: "Film / Scenes / Opening", artifact_version_ids: [] }} orchestrator="" ready onNavigate={onNavigate} />
  </QueryClientProvider>;
  return { ...render(tree()), tree, client, onNavigate };
}
it.each([["Go", "go"], ["Revise", "revise"], ["Deny", "deny"]])("records %s once and shows decision/time immediately when refreshing fails", async (label, decision) => {
  const writes: unknown[] = [];
  let saved = false;
  server.use(
    http.get("/v1/creative/runs/7/feed", () => saved ? HttpResponse.json({ error: { code: "offline", message: "Refresh offline" } }, { status: 503 }) : HttpResponse.json([proposal()])),
    http.post("/v1/creative/runs/7/feed/cards/40/decision", async ({ request }) => {
      writes.push(await request.json()); saved = true;
      return HttpResponse.json({ ...proposal(), card: { ...proposal().card, status: "decided", decision, decided_at: 1794340100 } });
    }),
  );
  open();
  const button = await screen.findByRole("button", { name: label });
  const card = button.closest("article")!;
  expect(card).toHaveClass("feed-entry--pending");
  expect(card.querySelector("strong")).toHaveTextContent("Orchestrator");
  expect(card.querySelector(".markdown strong")).toHaveTextContent("40");
  await userEvent.click(button);
  await waitFor(() => expect(within(card).queryByRole("button", { name: label })).not.toBeInTheDocument());
  expect(within(card).getByText(`Decision: ${label}`)).toBeVisible();
  expect(Array.from(card.querySelectorAll("time")).some(time => time.dateTime === new Date(1794340100 * 1000).toISOString())).toBe(true);
  expect(writes).toEqual([{ decision }]);
});
it("updates a stale card from a 409 envelope even when the refresh fails", async () => {
  let conflict = false;
  server.use(
    http.get("/v1/creative/runs/7/feed", () => conflict ? HttpResponse.json({ error: { code: "offline", message: "Refresh offline" } }, { status: 503 }) : HttpResponse.json([proposal()])),
    http.post("/v1/creative/runs/7/feed/cards/40/decision", () => {
      conflict = true;
      return HttpResponse.json({ error: { code: "creative_card_conflict", message: "Already replaced; read the current proposal" }, entry: {
        ...proposal(), card: { ...proposal().card, status: "replaced", replaced_by_entry_id: 50 },
      } }, { status: 409 });
    }),
  );
  open();
  await userEvent.click(await screen.findByRole("button", { name: "Go" }));
  expect(await screen.findByRole("link", { name: "Replaced → proposal #50" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Go" })).not.toBeInTheDocument();
  expect(await screen.findByText("Already replaced; read the current proposal")).toBeVisible();
});
it("renders decided cards without buttons and keeps a failed decision actionable", async () => {
  server.use(
    http.get("/v1/creative/runs/7/feed", () => HttpResponse.json([
      proposal(), { ...proposal(39), card: { ...proposal(39).card, status: "decided", decision: "deny", decided_at: 1794340100 } },
    ])),
    http.post("/v1/creative/runs/7/feed/cards/40/decision", () => HttpResponse.json({ error: { code: "offline", message: "Decision failed" } }, { status: 503 })),
  );
  open();
  expect(await screen.findByText("Decision: Deny")).toBeVisible();
  expect(within(screen.getByRole("article", { name: "Proposal 39" })).queryByRole("button")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Go" }));
  await screen.findByText("Decision failed");
  expect(screen.getByRole("button", { name: "Go" })).toBeEnabled();
});
it("resolves a replacement in another feed, clears item filtering and jumps to the successor without discarding a draft", async () => {
  const newer = { ...proposal(50), stage_id: 8, item_id: 6 };
  const cardReads: number[] = [];
  server.use(
    http.get("/v1/creative/runs/7/feed", ({ request }) => HttpResponse.json(new URL(request.url).searchParams.get("stage_id") === "8" ? [newer] : [
      { ...proposal(), card: { ...proposal().card, status: "replaced", replaced_by_entry_id: 50 } },
    ])),
    http.get("/v1/creative/runs/7/feed/cards/50", () => { cardReads.push(50); return HttpResponse.json(newer); }),
  );
  const view = open();
  await userEvent.click(screen.getByRole("checkbox", { name: "Only this item" }));
  await userEvent.type(screen.getByLabelText("Message"), "Keep original scope");
  const link = await screen.findByRole("link", { name: "Replaced → proposal #50" });
  expect(screen.queryByRole("button", { name: "Go" })).not.toBeInTheDocument();
  await userEvent.click(link);
  await waitFor(() => expect(view.onNavigate).toHaveBeenCalledWith(8, 6));
  expect(cardReads).toEqual([50]);
  view.rerender(view.tree(8, 6));
  const nextCard = await screen.findByRole("article", { name: "Proposal 50" });
  await waitFor(() => expect(nextCard).toHaveFocus());
  expect(screen.getByRole("checkbox", { name: "Only this item" })).not.toBeChecked();
  expect(screen.getByLabelText("Message")).toHaveValue("Keep original scope");
  expect(screen.getByText(/Draft stays with/)).toBeVisible();
});
