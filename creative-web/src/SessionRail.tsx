import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { SessionCard } from "../../web/src/components/SessionCard";
import { Badge } from "../../web/src/components/Badge";
import { Button } from "../../web/src/components/Button";
import "../../web/src/screens/task/SessionRail.css";
import { api, base, useCommand } from "./api";
import type { CreativeSession } from "./types";
import { ErrorNotice } from "./ui";

export function CreativeSessionRail({ runID, orchestrator }: { runID: number; orchestrator: string }) {
  const sessions = useQuery({
    queryKey: ["creative", "sessions", runID],
    queryFn: () =>
      api.get<CreativeSession[]>(`${base}/runs/${runID}/sessions`),
  });
  const profiles = useQuery({
    queryKey: ["creative", "profiles"],
    queryFn: () =>
      api.get<{
        profiles: { name: string; enabled: boolean; model: string }[];
      }>("/v1/model-profiles"),
  });
  const [profile, setProfile] = useState("");
  const command = useCommand();
  const session = sessions.data?.find((s) => s.id === orchestrator);
  const active = session && ["running", "spawning"].includes(session.state);
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
          <Button
            variant="secondary"
            size="sm"
            disabled={command.isPending}
            onClick={() =>
              command.mutate(() =>
                api.post(
                  `${base}/runs/${runID}/sessions`,
                  profile ? { profile } : {},
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
      <ErrorNotice error={sessions.error ?? profiles.error ?? command.error} />
      {sessions.data && (
        <section className="creative-sessions" aria-label="Run sessions">
          
          {session && <SessionCard key={session.id} session={session} role="orchestrator" />}
          <div className="session-rail__workers-heading">Workers</div>
          <div className="session-rail__workers">
            {sessions.data.filter((s) => s.kind === "worker").map((worker) => (
              <SessionCard key={worker.id} session={worker} role="worker" />
            ))}
            {!sessions.data.some((s) => s.kind === "worker") && <p className="session-rail__empty">No workers yet.</p>}
          </div>
        </section>
      )}
    </aside>
  );
}
