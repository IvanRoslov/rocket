import { Badge } from "../../web/src/components/Badge";
import { Button } from "../../web/src/components/Button";
import { useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { api, base, contextPath, useCommand } from "./api";
import type {
  ArtifactDetail,
  ArtifactVersion,
  Context,
  CreativeMessage,
  CreativeQuestion,
  CreativeSession,
} from "./types";
import { Empty, ErrorNotice } from "./ui";
function BoundVersions({ ids }: { ids: number[] }) {
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
export function Discussion({
  context,
  orchestrator,
  ready,
}: {
  context: Context;
  orchestrator: string;
  ready: boolean;
}) {
  const messages = useQuery({
    queryKey: [
      "creative",
      "messages",
      context.run_id,
      context.stage_id,
      context.item_id,
    ],
    queryFn: () => api.get<CreativeMessage[]>(contextPath(context, "messages")),
  });
  const questions = useQuery({
    queryKey: [
      "creative",
      "questions",
      context.run_id,
      context.stage_id,
      context.item_id,
    ],
    queryFn: () =>
      api.get<CreativeQuestion[]>(contextPath(context, "questions")),
  });
  const sessions = useQuery({
    queryKey: ["creative", "sessions", context.run_id],
    queryFn: () =>
      api.get<CreativeSession[]>(`${base}/runs/${context.run_id}/sessions`),
  });
  const profiles = useQuery({
    queryKey: ["creative", "profiles"],
    queryFn: () =>
      api.get<{
        profiles: { name: string; enabled: boolean; model: string }[];
      }>("/v1/model-profiles"),
  });
  const [profile, setProfile] = useState(""),
    [body, setBody] = useState(""),
    [mode, setMode] = useState("message");
  const [draftContext, setDraftContext] = useState<Context>();
  const command = useCommand();
  const session = sessions.data?.find((s) => s.id === orchestrator);
  const active = session && ["running", "spawning"].includes(session.state);
  const target = draftContext ?? context;
  const changed = JSON.stringify(target) !== JSON.stringify(context);
  function reset() {
    setBody("");
    setDraftContext(undefined);
  }
  return (
    <aside className="discussion">
      <div className="heading">
        <div>
          <span className="eyebrow">Creative orchestrator</span>
          <h2>Discussion</h2>
        </div>
        <Badge tone={active ? "ok" : "neutral"}>
          {active ? "Connected" : orchestrator ? "Offline" : "Not started"}
        </Badge>
      </div>
      <p className="context-label">{context.label}</p>
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
                  `${base}/runs/${context.run_id}/sessions`,
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
      <ErrorNotice
        error={
          sessions.error ?? profiles.error ?? messages.error ?? questions.error
        }
      />
      <div className="discussion-feed">
        <h3>Questions & decisions</h3>
        {questions.isPending && <p>Loading questions…</p>}
        {questions.data?.length === 0 && (
          <Empty>No questions in this context.</Empty>
        )}
        {questions.data?.map((q) => (
          <Question key={q.id} question={q} />
        ))}
        <h3>Messages</h3>
        {messages.isPending && <p>Loading messages…</p>}
        {messages.data?.length === 0 && (
          <Empty>Discuss the selected work and shown versions here.</Empty>
        )}
        {messages.data?.map((m) => (
          <article className="message" key={m.id}>
            <div className="row">
              <strong>
                {!m.from || m.from === "human"
                  ? "You"
                  : m.from === orchestrator
                    ? "Orchestrator"
                    : m.from}
              </strong>
              <small>{m.status}</small>
            </div>
            <p className="prose">{m.body}</p>
            <BoundVersions ids={m.artifact_version_ids ?? []} />
            {m.reason && <p className="error">{m.reason}</p>}
          </article>
        ))}
      </div>
      <form
        className="composer"
        onSubmit={(e) => {
          e.preventDefault();
          const { run_id, stage_id, item_id, artifact_version_ids } = target;
          const payload = {
            ...(stage_id ? { stage_id } : {}),
            ...(item_id ? { item_id } : {}),
            artifact_version_ids,
            body,
          };
          command.mutate(
            () =>
              api.post(
                `${base}/runs/${run_id}/${mode === "question" ? "questions" : "messages"}`,
                payload,
              ),
            { onSuccess: reset },
          );
        }}
      >
        <label>
          Send as
          <select value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="message">Message</option>
            <option value="question">Question thread</option>
          </select>
        </label>
        <p className="draft-context">
          {changed
            ? `Draft stays with ${target.label}`
            : `About ${target.label}`}
        </p>
        <label>
          Message or question
          <textarea
            rows={4}
            value={body}
            onChange={(e) => {
              if (!draftContext)
                setDraftContext({
                  ...context,
                  artifact_version_ids: [...context.artifact_version_ids],
                });
              setBody(e.target.value);
            }}
            placeholder="Discuss these results; include timecodes in your message."
            disabled={command.isPending || (!ready && !draftContext)}
          />
        </label>
        <div className="row">
          <Button
            variant="primary"
            size="sm"
            type="submit"
            disabled={
              !body.trim() ||
              command.isPending ||
              (!orchestrator && mode === "message")
            }
          >
            {mode === "question" ? "Open question" : "Send message"}
          </Button>
          {body && (
            <Button
              variant="secondary"
              size="sm"
              type="button"
              onClick={reset}
            >
              Discard draft
            </Button>
          )}
        </div>
        <ErrorNotice error={command.error} />
      </form>
    </aside>
  );
}
function Question({ question: q }: { question: CreativeQuestion }) {
  const [reply, setReply] = useState("");
  const command = useCommand();
  const open = q.status === "open";
  function send(action: "reply" | "answer", payload: unknown) {
    command.mutate(() => api.post(`/v1/questions/${q.id}/${action}`, payload), {
      onSuccess: () => setReply(""),
    });
  }
  return (
    <article className={`question ${q.your_turn ? "your-turn" : ""}`}>
      <div className="heading">
        <strong>{q.title || "Question"}</strong>
        <Badge tone={open ? (q.your_turn ? "warn" : "neutral") : "ok"}>
          {open ? (q.your_turn ? "Your reply needed" : "Open") : "Resolved"}
        </Badge>
      </div>
      <p className="prose">{q.body}</p>
      <BoundVersions ids={q.artifact_version_ids ?? []} />
      {q.messages?.map((m) => (
        <div className="thread-reply" key={m.id}>
          <strong>
            {!m.author || m.author === "human" ? "You" : m.author}
          </strong>
          <p className="prose">{m.body}</p>
        </div>
      ))}
      {q.resolution && <p className="prose">Resolution: {q.resolution}</p>}
      {open && (
        <>
          <div className="row">
            {q.options?.map((option, i) => (
              <Button
                variant="secondary"
                size="sm"
                key={i}
                disabled={command.isPending}
                onClick={() => send("answer", { choose: i + 1 })}
              >
                {option}
              </Button>
            ))}
          </div>
          <label>
            Reply to {q.title || "question"}
            <textarea
              value={reply}
              onChange={(e) => setReply(e.target.value)}
            />
          </label>
          <div className="row">
            <Button
              variant="secondary"
              size="sm"
              disabled={!reply.trim() || command.isPending}
              onClick={() => send("reply", { body: reply })}
            >
              Reply
            </Button>
            <Button
              variant="secondary"
              size="sm"
              disabled={!reply.trim() || command.isPending}
              onClick={() => send("answer", { body: reply })}
            >
              Answer & close
            </Button>
          </div>
        </>
      )}
      <ErrorNotice error={command.error} />
    </article>
  );
}
