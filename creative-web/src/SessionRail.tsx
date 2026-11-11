import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { SessionCard } from "../../web/src/components/SessionCard";
import { Badge } from "../../web/src/components/Badge";
import { Button } from "../../web/src/components/Button";
import "../../web/src/screens/task/SessionRail.css";
import { api, base, useCommand } from "./api";
import type { CreativeSession } from "./types";
import { ErrorNotice } from "./ui";
import { WorkerProfiles, type ModelProfile } from "./WorkerProfiles";

export function CreativeSessionRail({ runID, orchestrator, allowedProfiles = [], readOnly = false }: {
  readOnly?: boolean;
  runID: number;
  orchestrator: string;
  allowedProfiles?: string[];
}) {
  const sessions = useQuery({
    queryKey: ["creative", "sessions", runID],
    queryFn: () =>
      api.get<CreativeSession[]>(`${base}/runs/${runID}/sessions`),
  });
  const profiles = useQuery({
    queryKey: ["creative", "profiles"],
    queryFn: () =>
      api.get<{
        profiles: ModelProfile[];
      }>("/v1/model-profiles"),
  });
  const [profile, setProfile] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [editing, setEditing] = useState(false);
  const command = useCommand();
  const update = useCommand();
  const registry = profiles.data?.profiles ?? [];
  const enabledProfiles = registry.filter((p) => p.enabled);
  const editableProfiles = registry.filter((p) => p.enabled || allowedProfiles.includes(p.name));
  function startEdit() {
    setPicked(allowedProfiles.filter((name) => registry.some((p) => p.name === name)));
    update.reset();
    setEditing(true);
  }
  const session = sessions.data?.find((s) => s.id === orchestrator);
  const active = session && ["running", "spawning"].includes(session.state);
  // Keep recoverable errored workers, but omit closed sessions from the rail.
  // The full query remains available for orchestrator restore and history.
  const workers = sessions.data?.filter(
    (s) => s.kind === "worker" && s.state !== "killed" && s.state !== "done",
  ) ?? [];
  if (readOnly) return (
    <aside className="session-rail creative-session-rail" aria-label="Run sessions">
      <h2>Sessions</h2><p>Work closed for changes.</p>
      <ErrorNotice error={sessions.error} />
      <ul>{sessions.data?.map(s => <li key={s.id}>{s.id} · {s.state}</li>)}</ul>
    </aside>
  );
  return (
    <aside className="session-rail creative-session-rail" aria-label="Run sessions">
      <div className="heading">
        <h2>Sessions</h2>
        <Badge tone={active ? "ok" : "neutral"}>
          {active ? "Connected" : orchestrator ? "Offline" : "Not started"}
        </Badge>
      </div>
      {!orchestrator ? (
        <div className="session-controls">
          <label>
            Model profile
            <select
              value={profile}
              disabled={command.isPending || !profiles.isSuccess}
              onChange={(e) => setProfile(e.target.value)}
            >
              <option value="">Configured default</option>
              {profiles.data?.profiles
                .filter((p) => p.enabled)
                .map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name} · {p.model}
                  </option>
                ))}
            </select>
          </label>
          <WorkerProfiles profiles={enabledProfiles} picked={picked} onChange={setPicked}
            disabled={command.isPending || !profiles.isSuccess} />
          <Button
            variant="secondary"
            size="sm"
            disabled={command.isPending || profiles.isPending}
            onClick={() =>
              command.mutate(() =>
                api.post(
                  `${base}/runs/${runID}/sessions`,
                  { ...(profile ? { profile } : {}), allowed_profiles: picked },
                ),
              )
            }
          >
            Start creative orchestrator
          </Button>
        </div>
      ) : !active && !sessions.isPending ? (
        <Button
          variant="secondary"
          size="sm"
          disabled={command.isPending}
          onClick={() =>
            command.mutate(() =>
              api.post(
                `/v1/sessions/${encodeURIComponent(orchestrator)}/restore`,
              ),
            )
          }
        >
          Restore creative orchestrator
        </Button>
      ) : null}
      {orchestrator && (
        <section className="creative-worker-models" aria-label="Worker profiles">
          <h3>Worker profiles</h3>
          {allowedProfiles.length === 0 ? <p>all profiles</p> : (
            <ul>{allowedProfiles.map((name) => <li key={name}>{name}</li>)}</ul>
          )}
          {editing ? (
            <>
              <WorkerProfiles profiles={editableProfiles} picked={picked} onChange={setPicked}
                disabled={update.isPending} />
              <ErrorNotice error={update.error} />
              <div className="creative-worker-models__actions">
                <Button variant="secondary" size="sm" disabled={update.isPending}
                  onClick={() => update.mutate(
                    () => api.put(`${base}/runs/${runID}/models`, { allowed_profiles: picked }),
                    { onSuccess: () => setEditing(false) },
                  )}>Save worker profiles</Button>
                <Button variant="secondary" size="sm" disabled={update.isPending}
                  onClick={() => { setEditing(false); update.reset(); }}>Cancel</Button>
              </div>
            </>
          ) : (
            <Button variant="secondary" size="sm" disabled={!profiles.isSuccess}
              onClick={startEdit}>Edit worker profiles</Button>
          )}
        </section>
      )}
      <ErrorNotice error={sessions.error ?? profiles.error ?? command.error} />
      {sessions.data && (
        <section className="creative-sessions" aria-label="Run sessions">
          {session && <SessionCard key={session.id} session={session} role="orchestrator" />}
          <div className="session-rail__workers-heading">Workers</div>
          <div className="session-rail__workers">
            {workers.map((worker) => (
              <SessionCard key={worker.id} session={worker} role="worker" />
            ))}
            {workers.length === 0 && <p className="session-rail__empty">No workers yet.</p>}
          </div>
        </section>
      )}
    </aside>
  );
}
