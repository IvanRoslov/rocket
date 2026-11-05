import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, expect, it } from "vitest";
import { ReviewActions } from "./ReviewActions";
import type { ShownVersion, Work } from "./types";
const requests: unknown[] = [];
const server = setupServer(
  http.post("/v1/creative/runs/7/stages/4/actions", async ({ request }) => {
    requests.push(await request.json());
    return HttpResponse.json({ id: 7 });
  }),
);
beforeEach(() => { requests.length = 0; });
afterEach(() => server.resetHandlers());
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
const shown: ShownVersion = {
  artifact: {
    id: 2,
    run_id: 7,
    stage_id: 4,
    name: "Opening scene",
    kind: "result",
    current_version_id: 12,
  },
  version: {
    id: 11,
    artifact_id: 2,
    version_number: 1,
    filename: "opening.mp4",
    mime: "video/mp4",
    size: 100,
    content_url: "/v1/creative/artifact-versions/11/content",
    download_url: "/v1/creative/artifact-versions/11/content?download=1",
  },
};
const work: Work = {
  id: 4,
  name: "Scenes",
  description: "",
  expected_artifacts: "",
  status: "awaiting_approval",
  progress: 100,
  progress_note: "",
  review_revision: 3,
  artifact_version_ids: [11],
};
it("approves the displayed submitted version even when the current version is newer", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <ReviewActions
        scope={{ run_id: 7, stage_id: 4 }}
        work={work}
        canProgress
        shown={[shown]}
        ready
        onShowReview={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(screen.getByText(/Opening scene · version 1/)).toBeInTheDocument();
  await userEvent.click(
    screen.getByRole("button", { name: "Approve shown results" }),
  );
  await waitFor(() =>
    expect(requests).toEqual([{ action: "approve", review_revision: 3 }]),
  );
});
it("requires returning to submitted versions before approval when browsing other history", () => {
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <ReviewActions
        scope={{ run_id: 7, stage_id: 4 }}
        work={work}
        canProgress
        shown={[
          {
            ...shown,
            version: { ...shown.version, id: 12, version_number: 2 },
          },
        ]}
        ready
        onShowReview={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("button", { name: "Approve shown results" }),
  ).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Show submitted versions" }),
  ).toBeEnabled();
});

function renderActions(overrides: Partial<React.ComponentProps<typeof ReviewActions>> = {}) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const props = { scope: { run_id: 7, stage_id: 4 }, work: { ...work, status: "in_progress" as const }, canProgress: true, shown: [shown], ready: true, onShowReview: () => {}, ...overrides };
  const view = render(<QueryClientProvider client={client}><ReviewActions {...props} /></QueryClientProvider>);
  return { ...view, client, update: (next: typeof overrides) => view.rerender(<QueryClientProvider client={client}><ReviewActions {...props} {...next} /></QueryClientProvider>) };
}

it("confirms the named work and exact older shown result version before sending", async () => {
  renderActions();
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  expect(requests).toEqual([]);
  const dialog = screen.getByRole("dialog", { name: /Confirm submission/ });
  expect(dialog).toHaveTextContent("Scenes");
  expect(dialog).toHaveTextContent("Opening scene · version 1 · ID 11");
  expect(dialog).not.toHaveTextContent("version 2");
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  await waitFor(() => expect(requests).toEqual([{ action: "submit", artifact_version_ids: [11] }]));
});

it("cancel sends nothing and an empty result set is explicit", async () => {
  renderActions({ shown: [] });
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("No result versions");
  await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(requests).toEqual([]);
});

it.each([
  { shown: [{ ...shown, version: { ...shown.version, id: 12, version_number: 2 } }] },
  { scope: { run_id: 7, stage_id: 4, item_id: 9 }, work: { ...work, status: "in_progress" as const, name: "Other item" } },
  { work: { ...work, status: "in_progress" as const, review_revision: 4 } },
  { work },
  { ready: false },
])("invalidates confirmation when context, state or versions change: %j", async (change) => {
  const view = renderActions();
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  expect(screen.getByRole("dialog")).toBeInTheDocument();
  view.update(change);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(requests).toEqual([]);
});

it("a double click cannot accept the newly displayed confirmation", async () => {
  renderActions();
  await userEvent.dblClick(screen.getByRole("button", { name: "Submit shown results" }));
  fireEvent.click(screen.getByRole("button", { name: "Confirm submission" }), { detail: 2 });
  expect(requests).toEqual([]);
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  await waitFor(() => expect(requests).toHaveLength(1));
});

it("a repeated click on the replacement submit button cannot follow request changes", async () => {
  const view = renderActions({ work });
  await userEvent.click(screen.getByRole("button", { name: "Request changes" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  view.update({ work: { ...work, status: "in_progress" } });
  fireEvent.click(screen.getByRole("button", { name: "Submit shown results" }), { detail: 2 });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  await waitFor(() => expect(requests).toEqual([{ action: "reject", review_revision: 3 }, { action: "submit", artifact_version_ids: [11] }]));
});

it("synchronously suppresses duplicate commands until invalidation finishes", async () => {
  let release!: () => void;
  const view = renderActions({ work });
  view.client.invalidateQueries = () => new Promise<void>((resolve) => { release = resolve; });
  const approve = screen.getByRole("button", { name: "Approve shown results" });
  act(() => { fireEvent.click(approve); fireEvent.click(approve); });
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(approve).toBeDisabled();
  await act(async () => { release(); });
});


it("lists and submits the whole exact result set while excluding sources", async () => {
  renderActions({ shown: [shown, { ...shown, artifact: { ...shown.artifact, id: 3, name: "Ending" }, version: { ...shown.version, id: 21, artifact_id: 3, version_number: 4 } }, { ...shown, artifact: { ...shown.artifact, id: 5, kind: "source", name: "Reference" }, version: { ...shown.version, id: 31, artifact_id: 5 } }] });
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("Ending · version 4 · ID 21");
  expect(screen.getByRole("dialog")).not.toHaveTextContent("Reference");
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  await waitFor(() => expect(requests).toEqual([{ action: "submit", artifact_version_ids: [11, 21] }]));
});

it("requires a new confirmation after a failed submission and allows intentional retry", async () => {
  server.use(http.post("/v1/creative/runs/7/stages/4/actions", async ({ request }) => {
    requests.push(await request.json());
    return HttpResponse.json({ error: { code: "creative_conflict", message: "Work changed" } }, { status: 409 });
  }));
  renderActions();
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  expect(await screen.findByText("Work changed")).toBeInTheDocument();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  server.resetHandlers();
  await userEvent.click(screen.getByRole("button", { name: "Submit shown results" }));
  await userEvent.click(screen.getByRole("button", { name: "Confirm submission" }));
  await waitFor(() => expect(requests).toEqual([{ action: "submit", artifact_version_ids: [11] }, { action: "submit", artifact_version_ids: [11] }]));
});

it("confirms an item name and supports keyboard confirmation of an empty set", async () => {
  server.use(http.post("/v1/creative/runs/7/stages/4/items/9/actions", async ({ request }) => {
    requests.push(await request.json());
    return HttpResponse.json({ id: 7 });
  }));
  renderActions({ scope: { run_id: 7, stage_id: 4, item_id: 9 }, work: { ...work, id: 9, name: "Scene nine", status: "in_progress" }, shown: [] });
  screen.getByRole("button", { name: "Submit shown results" }).focus();
  await userEvent.keyboard("{Enter}");
  expect(screen.getByRole("dialog")).toHaveTextContent("Submit item: Scene nine?");
  screen.getByRole("button", { name: "Confirm submission" }).focus();
  await userEvent.keyboard("{Enter}");
  await waitFor(() => expect(requests).toEqual([{ action: "submit", artifact_version_ids: [] }]));
});
