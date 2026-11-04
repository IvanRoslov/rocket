import { Card } from "../../web/src/components/Card";
import { Badge } from "../../web/src/components/Badge";
import { Button } from "../../web/src/components/Button";
import { useLayoutEffect, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { api, base, contextPath, uploadSource, useCommand } from "./api";
import type {
  Artifact,
  ArtifactDetail,
  ArtifactVersion,
  Scope,
  ShownVersion,
} from "./types";
import { Empty, ErrorNotice } from "./ui";
export function Media({ version }: { version: ArtifactVersion }) {
  const [failed, setFailed] = useState(false);
  const textType = /^(text\/(plain|markdown|csv)|application\/json)(;|$)/.test(
    version.mime,
  );
  const text = useQuery({
    queryKey: ["creative", "text", version.id],
    enabled: textType && version.size <= 1024 * 1024,
    queryFn: async () => {
      const res = await fetch(version.content_url);
      if (!res.ok)
        throw new Error(
          `Could not load text (${res.status}). Download the file instead.`,
        );
      return res.text();
    },
    refetchInterval: false,
  });
  const image = /^image\/(png|jpeg|gif|webp|avif|bmp)(;|$)/.test(version.mime);
  return (
    <div className="media">
      {!failed &&
        (image ? (
          <img
            src={version.content_url}
            alt={version.filename}
            onError={() => setFailed(true)}
          />
        ) : version.mime.startsWith("video/") ? (
          <video
            controls
            preload="metadata"
            src={version.content_url}
            onError={() => setFailed(true)}
          />
        ) : version.mime.startsWith("audio/") ? (
          <audio
            controls
            preload="metadata"
            src={version.content_url}
            onError={() => setFailed(true)}
          />
        ) : textType && version.size <= 1024 * 1024 ? (
          <>
            <ErrorNotice error={text.error} />
            <pre>{text.data ?? "Loading text…"}</pre>
          </>
        ) : null)}
      {failed && (
        <p>This browser could not display the file. Download it to view it.</p>
      )}
      <a href={version.download_url} download>
        Download {version.filename}
      </a>
    </div>
  );
}
export function Artifacts({
  scope,
  reviewIDs = [],
  reviewing = false,
  itemContexts = [],
  onShown,
}: {
  scope: Scope;
  reviewIDs?: number[];
  reviewing?: boolean;
  itemContexts?: { id: number; name: string }[];
  onShown: (shown: ShownVersion[], ready: boolean) => void;
}) {
  const list = useQuery({
    queryKey: ["creative", "artifacts", scope],
    queryFn: () => api.get<Artifact[]>(contextPath(scope, "artifacts")),
  });
  const descendants = useQueries({
    queries: itemContexts.map((item) => ({
      queryKey: ["creative", "artifacts", { ...scope, item_id: item.id }],
      queryFn: () =>
        api.get<Artifact[]>(
          contextPath({ ...scope, item_id: item.id }, "artifacts"),
        ),
    })),
  });
  const artifacts = [
    ...(list.data ?? []),
    ...descendants.flatMap((q) => q.data ?? []),
  ];
  const details = useQueries({
    queries: artifacts.map((a) => ({
      queryKey: ["creative", "artifact", a.id],
      queryFn: () => api.get<ArtifactDetail>(`${base}/artifacts/${a.id}`),
    })),
  });
  const [selected, setSelected] = useState<Record<number, number>>({});
  const [browseAll, setBrowseAll] = useState(false);
  const [name, setName] = useState(""),
    [file, setFile] = useState<File>();
  const [uploadKey, setUploadKey] = useState(0);
  const command = useCommand();
  const visible = details.filter(
    (q) =>
      !reviewing ||
      browseAll ||
      !q.data ||
      q.data.artifact.kind === "source" ||
      q.data.versions.some((v) => reviewIDs.includes(v.id)),
  );
  const shown: ShownVersion[] = visible.flatMap((q) => {
    if (!q.data) return [];
    const { artifact, versions } = q.data;
    const submitted = versions.filter((v) => reviewIDs.includes(v.id));
    const multipleSubmitted = reviewing && !browseAll && submitted.length > 1;
    const id =
      selected[artifact.id] ?? submitted[0]?.id ?? artifact.current_version_id;
    const displayed = multipleSubmitted
      ? submitted
      : versions.filter((v) => v.id === id);
    return displayed.map((version) => ({
      artifact,
      version,
      item_name: itemContexts.find((i) => i.id === artifact.item_id)?.name,
    }));
  });
  const ready =
    list.isSuccess &&
    descendants.every((q) => q.isSuccess) &&
    details.every((q) => q.isSuccess) &&
    visible.every(
      (q) =>
        q.data &&
        shown.some((entry) => entry.artifact.id === q.data.artifact.id),
    );
  const signature = JSON.stringify({ shown, ready });
  useLayoutEffect(() => {
    onShown(shown, ready);
  }, [signature, onShown]);
  return (
    <section className="artifacts">
      <div className="heading">
        <h2>Sources & results</h2>
        <span>{artifacts.length} artifacts</span>
      </div>
      <ErrorNotice
        error={list.error ?? descendants.find((q) => q.error)?.error}
      />
      {reviewing && (
        <div className="row">
          <p className="muted">
            {browseAll
              ? "Browsing all published artifacts. Approval requires the submitted set."
              : "Showing submitted results and sources."}
          </p>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setBrowseAll(!browseAll);
              if (browseAll) setSelected({});
            }}
          >
            {browseAll ? "Return to submitted results" : "Browse all artifacts"}
          </Button>
        </div>
      )}
      {list.isPending && <p>Loading artifacts…</p>}
      {!list.isPending && artifacts.length === 0 && (
        <Empty>
          No artifacts here yet. Upload sources or ask the orchestrator to
          publish results.
        </Empty>
      )}
      {visible.map((q, index) => {
        if (q.error) return <ErrorNotice key={index} error={q.error} />;
        if (!q.data) return <p key={index}>Loading artifact history…</p>;
        const { artifact, versions } = q.data;
        const entries = shown.filter((s) => s.artifact.id === artifact.id);
        if (!entries.length)
          return <p key={artifact.id}>Artifact version is unavailable.</p>;
        return entries.map((entry) => {
          const version = entry.version,
            current = artifact.current_version_id === version.id;
          return (
            <Card
              role="article"
              className="artifact-card"
              key={`${artifact.id}:${version.id}`}
            >
              <div className="heading">
                <div>
                  <h3>{artifact.name}</h3>
                  {entry.item_name && <small>Item: {entry.item_name}</small>}
                </div>
                <Badge tone="neutral">
                  {artifact.kind === "source" ? "Source" : "Result"}
                </Badge>
              </div>
              {entries.length > 1 ? (
                <p>Submitted version {version.version_number}</p>
              ) : (
                <label>
                  {artifact.name} version
                  <select
                    value={version.id}
                    onChange={(e) =>
                      setSelected({
                        ...selected,
                        [artifact.id]: Number(e.target.value),
                      })
                    }
                  >
                    {[...versions].reverse().map((v) => (
                      <option value={v.id} key={v.id}>
                        Version {v.version_number}
                        {v.id === artifact.current_version_id
                          ? " · current"
                          : ""}
                        {reviewIDs.includes(v.id)
                          ? " · submitted for review"
                          : ""}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              <div className="row">
                <span className="muted">
                  {current ? "Current version" : "Retained version"} ·{" "}
                  {new Intl.NumberFormat("en", {
                    style: "unit",
                    unit: "megabyte",
                    maximumFractionDigits: 2,
                  }).format(version.size / 1e6)}
                </span>
                {!current && (
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={command.isPending}
                    onClick={() =>
                      command.mutate(() =>
                        api.put(`${base}/artifacts/${artifact.id}/current`, {
                          version_id: version.id,
                        }),
                      )
                    }
                  >
                    Make current
                  </Button>
                )}
              </div>
              <Media key={version.id} version={version} />
            </Card>
          );
        });
      })}
      <form
        className="upload"
        onSubmit={(e) => {
          e.preventDefault();
          if (file)
            command.mutate(() => uploadSource(scope, file, name || file.name), {
              onSuccess: () => {
                setFile(undefined);
                setName("");
                setUploadKey((n) => n + 1);
              },
            });
        }}
      >
        <h3>Upload source</h3>
        <label>
          Source name
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Optional — defaults to filename"
          />
        </label>
        <label>
          Source file
          <input
            type="file"
            key={uploadKey}
            onChange={(e) => setFile(e.target.files?.[0])}
          />
        </label>
        <p className="muted">
          Files up to 2 GiB. Uploads belong to the selected work.
        </p>
        <Button
          variant="secondary"
          size="sm"
          type="submit"
          disabled={!file || command.isPending}
        >
          {command.isPending ? "Uploading…" : "Upload source"}
        </Button>
      </form>
      <ErrorNotice error={command.error} />
    </section>
  );
}
