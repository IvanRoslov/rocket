import { File as NativeFile } from "node:buffer";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { beforeAll, afterAll, afterEach, expect, it } from "vitest";
import { Artifacts, Media } from "./Artifacts";
const artifact = {
  id: 2,
  run_id: 7,
  stage_id: 4,
  name: "Opening scene",
  kind: "result" as const,
  current_version_id: 12,
};
const versions = [11, 12].map((id, i) => ({
  id,
  artifact_id: 2,
  version_number: i + 1,
  filename: "clip.mp4",
  mime: "video/mp4",
  size: 100,
  content_url: `/v1/creative/artifact-versions/${id}/content`,
  download_url: `/v1/creative/artifact-versions/${id}/content?download=1`,
}));
const server = setupServer(
  http.get("/v1/creative/runs/7/artifacts", () =>
    HttpResponse.json([artifact]),
  ),
  http.get("/v1/creative/artifacts/2", () =>
    HttpResponse.json({ artifact, versions }),
  ),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
it("opens the submitted immutable version and permits inspecting retained history", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Artifacts
        scope={{ run_id: 7, stage_id: 4 }}
        reviewIDs={[11]}
        onShown={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(
    await screen.findByRole("link", { name: "Download clip.mp4" }),
  ).toHaveAttribute(
    "href",
    "/v1/creative/artifact-versions/11/content?download=1",
  );
  await userEvent.selectOptions(
    screen.getByLabelText("Opening scene version"),
    "12",
  );
  expect(
    screen.getByRole("link", { name: "Download clip.mp4" }),
  ).toHaveAttribute(
    "href",
    "/v1/creative/artifact-versions/12/content?download=1",
  );
  expect(screen.getByText(/Current version/)).toBeInTheDocument();
});
it("keeps active document formats downloadable without embedding them", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Media
        version={{
          ...versions[0],
          mime: "image/svg+xml",
          filename: "unsafe.svg",
        }}
      />
    </QueryClientProvider>,
  );
  expect(
    screen.getByRole("link", { name: "Download unsafe.svg" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
});
it("aggregates item results into stage review without moving or republishing them", async () => {
  server.use(
    http.get("/v1/creative/runs/7/artifacts", ({ request }) =>
      HttpResponse.json(
        new URL(request.url).searchParams.get("item_id") === "9"
          ? [{ ...artifact, item_id: 9 }]
          : [],
      ),
    ),
    http.get("/v1/creative/artifacts/2", () =>
      HttpResponse.json({ artifact: { ...artifact, item_id: 9 }, versions }),
    ),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Artifacts
        scope={{ run_id: 7, stage_id: 4 }}
        itemContexts={[{ id: 9, name: "Scene one" }]}
        reviewIDs={[11]}
        reviewing
        onShown={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("Item: Scene one")).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "Download clip.mp4" }),
  ).toHaveAttribute(
    "href",
    "/v1/creative/artifact-versions/11/content?download=1",
  );
});
it("uploads source bytes with the selected item context and retains the chosen filename until upload", async () => {
  const uploads: { path: string; mime: string | null; bytes: string }[] = [];
  server.use(
    http.post("/v1/creative/runs/7/artifacts", async ({ request }) => {
      uploads.push({
        path: new URL(request.url).search,
        mime: request.headers.get("Content-Type"),
        bytes: await request.text(),
      });
      return HttpResponse.json(
        { artifact, version: versions[0] },
        { status: 201 },
      );
    }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <Artifacts
        scope={{ run_id: 7, stage_id: 4, item_id: 9 }}
        onShown={() => {}}
      />
    </QueryClientProvider>,
  );
  const user = userEvent.setup(),
    file = new NativeFile(["source text"], "reference.txt", {
      type: "text/plain",
    }) as unknown as File;
  await user.upload(screen.getByLabelText("Source file"), file);
  expect(
    (screen.getByLabelText("Source file") as HTMLInputElement).files?.[0]?.name,
  ).toBe("reference.txt");
  await user.type(screen.getByLabelText("Source name"), "Mood reference");
  await user.click(screen.getByRole("button", { name: "Upload source" }));
  await waitFor(() =>
    expect(uploads).toEqual([
      {
        path: "?stage_id=4&item_id=9&name=Mood+reference&kind=source&filename=reference.txt",
        mime: "text/plain",
        bytes: "source text",
      },
    ]),
  );
});

it("displays every submitted version when a review includes two versions of one artifact", async () => {
  const displayed: number[][] = [];
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Artifacts
        scope={{ run_id: 7, stage_id: 4 }}
        reviewIDs={[11, 12]}
        reviewing
        onShown={(shown, ready) => {
          if (ready) displayed.push(shown.map((s) => s.version.id));
        }}
      />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(
      screen.getAllByRole("link", { name: "Download clip.mp4" }),
    ).toHaveLength(2),
  );
  expect(
    screen
      .getAllByRole("link", { name: "Download clip.mp4" })
      .map((a) => a.getAttribute("href")),
  ).toEqual([
    "/v1/creative/artifact-versions/11/content?download=1",
    "/v1/creative/artifact-versions/12/content?download=1",
  ]);
  expect(displayed).toContainEqual([11, 12]);
});

it("opens the selected submitted HTML version in a separate isolated tab and retains download/history", async () => {
  const htmlVersions = versions.map((v) => ({
    ...v,
    filename: "page.html",
    mime: "text/html",
  }));
  server.use(
    http.get("/v1/creative/artifacts/2", () =>
      HttpResponse.json({ artifact, versions: htmlVersions }),
    ),
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Artifacts
        scope={{ run_id: 7, stage_id: 4 }}
        reviewIDs={[11]}
        reviewing
        onShown={() => {}}
      />
    </QueryClientProvider>,
  );
  const link = await screen.findByRole("link", { name: /Open HTML.*new tab/ });
  expect(link).toHaveAttribute(
    "href",
    "/v1/creative/artifact-versions/11/view",
  );
  expect(link).toHaveAttribute("target", "_blank");
  expect(link).toHaveAttribute("rel", "noopener noreferrer");
  expect(
    screen.getByRole("link", { name: "Download page.html" }),
  ).toHaveAttribute(
    "href",
    "/v1/creative/artifact-versions/11/content?download=1",
  );
  expect(screen.getByText(/self-contained/)).toBeInTheDocument();
  await userEvent.selectOptions(
    screen.getByLabelText("Opening scene version"),
    "12",
  );
  expect(
    screen.getByRole("link", { name: /Open HTML.*new tab/ }),
  ).toHaveAttribute("href", "/v1/creative/artifact-versions/12/view");
});

it("explains unsupported previews and keeps downloads", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Media
        version={{
          ...versions[0],
          mime: "application/zip",
          filename: "bundle.zip",
        }}
      />
    </QueryClientProvider>,
  );
  expect(
    screen.getByText(/Preview is unavailable for this file type/),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "Download bundle.zip" }),
  ).toBeInTheDocument();
});

it("retains image, video and audio previews and reports playback failures", () => {
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      {["image/png", "video/mp4", "audio/mpeg"].map((mime, i) => (
        <Media key={mime} version={{ ...versions[0], id: i + 1, mime }} />
      ))}
    </QueryClientProvider>,
  );
  expect(screen.getByRole("img")).toHaveAttribute(
    "src",
    versions[0].content_url,
  );
  expect(container.querySelector("video")).toHaveAttribute("controls");
  expect(container.querySelector("audio")).toHaveAttribute("controls");
  fireEvent.error(container.querySelector("video")!);
  expect(
    screen.getByText(/This browser could not display the file/),
  ).toBeInTheDocument();
  expect(
    screen.getAllByRole("link", { name: "Download clip.mp4" }),
  ).toHaveLength(3);
});

it("formats generic-MIME Markdown with selected filename/version and immutable download", async () => {
  const docs = versions.map((v) => ({ ...v, filename: "paid-generation-proposal.md", mime: "application/octet-stream" }));
  server.use(
    http.get("/v1/creative/artifacts/2", () => HttpResponse.json({ artifact, versions: docs })),
    http.get("/v1/creative/artifact-versions/:id/content", ({ params }) => HttpResponse.text(`# Proposal ${params.id}\n\n**Readable**\n\n| Cost | Count |\n| --- | --- |\n| 2 | 1 |`)),
  );
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <Artifacts scope={{ run_id: 7, stage_id: 4 }} reviewIDs={[11]} onShown={() => {}} />
  </QueryClientProvider>);
  expect(await screen.findByRole("heading", { name: "Proposal 11" })).toBeVisible();
  expect(screen.getByText("Readable").tagName).toBe("STRONG");
  expect(screen.getByRole("table")).toBeVisible();
  expect(screen.getByText("paid-generation-proposal.md · version 1")).toBeVisible();
  expect(screen.getByRole("heading", { name: "Opening scene" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Download paid-generation-proposal.md" })).toHaveAttribute("href", "/v1/creative/artifact-versions/11/content?download=1");
  await userEvent.selectOptions(screen.getByLabelText("Opening scene version"), "12");
  expect(await screen.findByRole("heading", { name: "Proposal 12" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Proposal 11" })).not.toBeInTheDocument();
  expect(screen.getByText("paid-generation-proposal.md · version 2")).toBeVisible();
  expect(screen.getByRole("link", { name: "Download paid-generation-proposal.md" })).toHaveAttribute("href", "/v1/creative/artifact-versions/12/content?download=1");
});

it("renders Markdown without active HTML, unsafe/relative links or automatic embedded resources", async () => {
  server.use(http.get(versions[0].content_url, () => HttpResponse.text(`# Safe document\n\n<script>alert(1)</script>\n\n<iframe src='/v1/private'></iframe>\n\n<img src='https://tracker.invalid/pixel'>\n\n![Remote](https://tracker.invalid/image)\n\n[Unsafe](javascript:alert%281%29) [API](/v1/private) [Protocol](//tracker.invalid) [External](https://example.com/document)`)));
  const { container } = render(<QueryClientProvider client={new QueryClient()}>
    <Media version={{ ...versions[0], filename: "document.md", mime: "text/markdown" }} />
  </QueryClientProvider>);
  await screen.findByRole("heading", { name: "Safe document" });
  expect(container.querySelector("script, iframe, img, object, embed")).toBeNull();
  for (const label of ["Unsafe", "API", "Protocol"]) expect(screen.queryByRole("link", { name: label })).not.toBeInTheDocument();
  expect(screen.getByText("Remote")).toBeVisible();
  expect(screen.getByRole("link", { name: "External" })).toHaveAttribute("rel", "noopener noreferrer");
});

it("explains Markdown load failures and size limits with download still available", async () => {
  server.use(http.get(versions[0].content_url, () => new HttpResponse(null, { status: 403 })));
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <Media version={{ ...versions[0], filename: "denied.md", mime: "application/octet-stream" }} />
    <Media version={{ ...versions[1], filename: "large.md", mime: "text/markdown", size: 1024 * 1024 + 1 }} />
  </QueryClientProvider>);
  expect(await screen.findByText(/Could not load text \(403\)/)).toBeVisible();
  expect(screen.queryByText("Loading text…")).not.toBeInTheDocument();
  expect(screen.getByText(/too large to preview/)).toBeVisible();
  expect(screen.getByRole("link", { name: "Download denied.md" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Download large.md" })).toBeVisible();
});

it("does not reinterpret HTML or media as Markdown based on filename", () => {
  const { container } = render(<QueryClientProvider client={new QueryClient()}>
    <Media version={{ ...versions[0], filename: "active.md", mime: "text/html" }} />
    <Media version={{ ...versions[1], filename: "image.md", mime: "image/png" }} />
  </QueryClientProvider>);
  expect(screen.getByRole("link", { name: /Open HTML/ })).toBeVisible();
  expect(container.querySelector("img")).toHaveAttribute("src", versions[1].content_url);
});
