import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, beforeAll, expect, it } from "vitest";
import { ReviewActions } from "./ReviewActions";
import type { ShownVersion, Work } from "./types";
const requests: unknown[] = [];
const server = setupServer(
  http.post("/v1/creative/runs/7/stages/4/actions", async ({ request }) => {
    requests.push(await request.json());
    return HttpResponse.json({ id: 7 });
  }),
);
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
