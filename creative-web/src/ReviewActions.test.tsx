import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, expect, it } from "vitest";
import { ReviewActions } from "./ReviewActions";
import type { Work } from "./types";
const requests: unknown[] = [];
const artifact = { id: 2, run_id: 7, stage_id: 4, item_id: 9, name: "Opening scene", kind: "result", current_version_id: 12 };
const version = { id: 11, artifact_id: 2, version_number: 1, filename: "opening.png", mime: "image/png", size: 100, content_url: "/content/11", download_url: "/download/11" };
const server = setupServer(
  http.get("/v1/creative/artifact-versions/11", () => HttpResponse.json(version)),
  http.get("/v1/creative/artifacts/2", () => HttpResponse.json({ artifact, versions: [version, { ...version, id: 12, version_number: 2 }] })),
  http.post("/v1/creative/runs/7/stages/4/actions", async ({ request }) => { requests.push(await request.json()); return HttpResponse.json({ id: 7 }); }),
);
beforeEach(() => { requests.length = 0; });
afterEach(() => server.resetHandlers());
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
const work: Work = { id: 4, name: "Scenes", description: "", expected_artifacts: "", status: "awaiting_approval", progress: 100, progress_note: "", review_revision: 3, artifact_version_ids: [11] };
function open(overrides: Partial<React.ComponentProps<typeof ReviewActions>> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const props = { scope: { run_id: 7, stage_id: 4 }, work, canProgress: true, itemContexts: [{ id: 9, name: "Opening item" }], ...overrides };
  const view = render(<QueryClientProvider client={client}><ReviewActions {...props} /></QueryClientProvider>);
  return { client, update: (next: typeof overrides) => view.rerender(<QueryClientProvider client={client}><ReviewActions {...props} {...next} /></QueryClientProvider>) };
}
it("shows a pending named revision and opens the exact descendant version despite a newer current version", async () => {
  open();
  expect(screen.getByRole("region", { name: "Review Scenes" })).toHaveTextContent("Revision 3");
  await userEvent.click(await screen.findByRole("link", { name: "Opening item / Opening scene · version 1" }));
  expect(screen.getByRole("img", { name: "opening.png" })).toHaveAttribute("src", "/content/11");
  expect(screen.getByRole("link", { name: "Download opening.png" })).toHaveAttribute("href", "/download/11");
  expect(requests).toEqual([]);
});
it("approves once and removes obsolete actions even if the server snapshot has not refreshed", async () => {
  open();
  await userEvent.click(await screen.findByRole("button", { name: "Approve stage" }));
  await waitFor(() => expect(requests).toEqual([{ action: "approve", review_revision: 3 }]));
  expect(await screen.findByText(/Revision 3 approved/)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Approve stage" })).not.toBeInTheDocument();
});
it("requires nonblank feedback, sends it with revision, and displays the returned outcome", async () => {
  open();
  await userEvent.click(screen.getByRole("button", { name: "Return for changes" }));
  const send = screen.getByRole("button", { name: "Send feedback and return" });
  expect(send).toBeDisabled();
  await userEvent.type(screen.getByLabelText("What to fix"), "   ");
  expect(send).toBeDisabled();
  await userEvent.type(screen.getByLabelText("What to fix"), "Fix the ending  ");
  await userEvent.click(send);
  await waitFor(() => expect(requests).toEqual([{ action: "reject", review_revision: 3, reason: "Fix the ending" }]));
  expect(await screen.findByText(/Revision 3 returned for changes/)).toBeVisible();
  expect(screen.getByText("Fix the ending")).toBeVisible();
});
it("preserves feedback on failure and permits deliberate retry", async () => {
  server.use(http.post("/v1/creative/runs/7/stages/4/actions", () => HttpResponse.json({ error: { code: "creative_conflict", message: "Work changed" } }, { status: 409 })));
  open();
  await userEvent.click(screen.getByRole("button", { name: "Return for changes" }));
  await userEvent.type(screen.getByLabelText("What to fix"), "Keep this draft");
  await userEvent.click(screen.getByRole("button", { name: "Send feedback and return" }));
  expect(await screen.findByText("Work changed")).toBeVisible();
  expect(screen.getByLabelText("What to fix")).toHaveValue("Keep this draft");
  server.resetHandlers();
  await userEvent.click(screen.getByRole("button", { name: "Send feedback and return" }));
  await waitFor(() => expect(requests).toHaveLength(1));
});
it.each([
  { work: { ...work, review_revision: 4 } },
  { scope: { run_id: 7, stage_id: 4, item_id: 9 }, work: { ...work, id: 9 } },
])("never carries a feedback draft into another review/context", async (next) => {
  const view = open();
  await userEvent.click(screen.getByRole("button", { name: "Return for changes" }));
  await userEvent.type(screen.getByLabelText("What to fix"), "Old review only");
  view.update(next);
  expect(screen.queryByLabelText("What to fix")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Return for changes" }));
  expect(screen.getByLabelText("What to fix")).toHaveValue("");
  expect(requests).toEqual([]);
});
it("synchronously prevents duplicate clicks while query refresh is pending", async () => {
  const view = open();
  let release!: () => void;
  view.client.invalidateQueries = () => new Promise<void>((resolve) => { release = resolve; });
  const approve = screen.getByRole("button", { name: "Approve stage" });
  act(() => { fireEvent.click(approve); fireEvent.click(approve); });
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(screen.queryByRole("button", { name: "Approve stage" })).not.toBeInTheDocument();
  await act(async () => release());
});
it("withdrawal removes decisions and a new submitted revision has fresh controls", async () => {
  const view = open();
  view.update({ work: { ...work, status: "in_progress" } });
  expect(screen.getByText(/No review is awaiting a decision/)).toBeVisible();
  expect(screen.queryByRole("button", { name: /Approve|Return|Submit/ })).not.toBeInTheDocument();
  view.update({ work: { ...work, review_revision: 4 } });
  await userEvent.click(screen.getByRole("button", { name: "Approve stage" }));
  await waitFor(() => expect(requests).toEqual([{ action: "approve", review_revision: 4 }]));
});
it.each(["pending", "in_progress", "completed"] as const)("retains lifecycle controls without human submit for %s", (status) => {
  open({ work: { ...work, status, review_revision: 0, artifact_version_ids: [] } });
  expect(screen.queryByRole("button", { name: /Approve|Return|Submit/ })).not.toBeInTheDocument();
  if (status === "pending") expect(screen.getByRole("button", { name: "Start work" })).toBeEnabled();
  if (status === "completed") expect(screen.getByRole("button", { name: "Reopen for rework" })).toBeEnabled();
});
it("uses isolated HTML viewing for the exact submitted version", async () => {
  server.use(http.get("/v1/creative/artifact-versions/11", () => HttpResponse.json({ ...version, mime: "text/html", filename: "page.html" })));
  open();
  const link = await screen.findByRole("link", { name: "Opening item / Opening scene · version 1" });
  expect(link).toHaveAttribute("href", "/v1/creative/artifact-versions/11/view");
  expect(link).toHaveAttribute("target", "_blank");
});

it("can reopen after approval without reusing the completed decision", async () => {
  const view = open();
  await userEvent.click(screen.getByRole("button", { name: "Approve stage" }));
  await screen.findByText(/Revision 3 approved/);
  view.update({ work: { ...work, status: "completed" } });
  await userEvent.click(screen.getByRole("button", { name: "Reopen for rework" }));
  await waitFor(() => expect(requests).toEqual([{ action: "approve", review_revision: 3 }, { action: "reopen" }]));
});

it("shows all submitted versions of one artifact and handles an empty review", async () => {
  server.use(http.get("/v1/creative/artifact-versions/12", () => HttpResponse.json({ ...version, id: 12, version_number: 2, content_url: "/content/12" })));
  const view = open({ work: { ...work, artifact_version_ids: [11, 12] } });
  expect(await screen.findByRole("link", { name: /Opening scene · version 1/ })).toBeVisible();
  expect(await screen.findByRole("link", { name: /Opening scene · version 2/ })).toBeVisible();
  view.update({ work: { ...work, review_revision: 4, artifact_version_ids: [] } });
  expect(screen.getByText("No artifacts were submitted with this review.")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "Approve stage" }));
  await waitFor(() => expect(requests).toEqual([{ action: "approve", review_revision: 4 }]));
});

it("shows current in-progress state if approved work is reopened elsewhere without a new revision", async () => {
  const view = open();
  await userEvent.click(screen.getByRole("button", { name: "Approve stage" }));
  await screen.findByText(/Revision 3 approved/);
  view.update({ work: { ...work, status: "completed" } });
  view.update({ work: { ...work, status: "in_progress" } });
  expect(screen.getByText(/Work is in progress. No review is awaiting a decision/)).toBeVisible();
  expect(screen.queryByText(/Revision 3 approved/)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Approve|Return|Submit/ })).not.toBeInTheDocument();
});
