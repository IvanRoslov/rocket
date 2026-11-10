import { useQueries } from "@tanstack/react-query";
import { api, base } from "./api";
import type { ArtifactDetail, ArtifactVersion } from "./types";
export function BoundVersions({ ids }: { ids: number[] }) {
  const versions = useQueries({
    queries: ids.map((id) => ({
      queryKey: ["creative", "version", id],
      queryFn: () =>
        api.get<ArtifactVersion>(`${base}/artifact-versions/${id}`),
    })),
  });
  const artifacts = useQueries({
    queries: versions.map((v) => ({
      queryKey: ["creative", "artifact", v.data?.artifact_id],
      queryFn: () =>
        api.get<ArtifactDetail>(`${base}/artifacts/${v.data?.artifact_id}`),
      enabled: !!v.data,
    })),
  });
  if (!ids.length) return null;
  return (
    <ul className="bound-versions" aria-label="Referenced artifact versions">
      {versions.map((q, i) => (
        <li key={ids[i]}>
          {q.data ? (
            <a
              href={q.data.content_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              {artifacts[i].data?.artifact.name ?? q.data.filename} · version{" "}
              {q.data.version_number}
            </a>
          ) : q.error ? (
            "Referenced version is unavailable"
          ) : (
            "Loading referenced version…"
          )}
        </li>
      ))}
    </ul>
  );
}
