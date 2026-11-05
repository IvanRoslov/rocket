import { Button } from "../../web/src/components/Button";
import { useState, type Dispatch, type SetStateAction } from "react";
import { QuestionThreadView } from "../../web/src/components/QuestionThreadView";
import { Markdown } from "../../web/src/components/Markdown";
import { isHuman } from "../../web/src/lib/participants";
import { questionTitle } from "../../web/src/lib/thread";
import { timeAgo } from "../../web/src/lib/format";
import "../../web/src/screens/task/QuestionsTab.css";
import "../../web/src/screens/chat/ChatScreen.css";
import { useQueries, useQuery } from "@tanstack/react-query";
import { api, base, contextPath, useCommand } from "./api";
import type {
  ArtifactDetail,
  ArtifactVersion,
  Context,
  CreativeMessage,
  CreativeQuestion,
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
  const [body, setBody] = useState(""),
    [mode, setMode] = useState("message");
  const [draftContext, setDraftContext] = useState<Context>();
  const [replies, setReplies] = useState<Record<number, string>>({});
  const command = useCommand();
  const target = draftContext ?? context;
  const changed = JSON.stringify(target) !== JSON.stringify(context);
  function reset() {
    setBody("");
    setDraftContext(undefined);
  }
  const sorted = [...(questions.data ?? [])].sort(
    (a, b) => (b.asked_at ?? b.id) - (a.asked_at ?? a.id) || b.id - a.id,
  );
  const open = sorted.filter((q) => q.status === "open");
  const resolved = sorted.filter((q) => q.status !== "open");
  const draft = (id: number) => ({
    body: replies[id] ?? "",
    setBody: ((value) => setReplies((prev) => ({
      ...prev,
      [id]: typeof value === "function" ? value(prev[id] ?? "") : value,
    }))) as Dispatch<SetStateAction<string>>,
  });
  return (
    <section className="discussion">
      <div className="heading">
        <div>
          <span className="eyebrow">Creative orchestrator</span>
          <h2>Discussion</h2>
        </div>
      </div>
      <p className="context-label">{context.label}</p>
      <ErrorNotice error={messages.error ?? questions.error} />
      <div className="discussion-feed">
        <h3>Questions & decisions</h3>
        {questions.isPending && <p>Loading questions…</p>}
        {questions.data?.length === 0 && (
          <Empty>No questions in this context.</Empty>
        )}
        <div className="questions-tab">
          {open.map((q) => <Question key={q.id} question={q} draft={draft(q.id)} />)}
          {resolved.length > 0 && <div className="questions-tab__resolved-label">Resolved</div>}
          {resolved.map((q) => <ResolvedQuestion key={q.id} question={q} />)}
        </div>
        <h3>Messages</h3>
        {messages.isPending && <p>Loading messages…</p>}
        {messages.data?.length === 0 && (
          <Empty>Discuss the selected work and shown versions here.</Empty>
        )}
        {messages.data?.map((m) => (
          <article className={`chat-screen__row ${isHuman(m.from) ? "chat-screen__row--own" : ""}`} key={m.id}>
            <div className={`chat-screen__bubble ${isHuman(m.from) ? "chat-screen__bubble--own" : ""}`}>
              <strong className="question-thread__message-author">
                {isHuman(m.from) ? "You" : m.from === orchestrator ? "Orchestrator" : m.from}
              </strong>
              <div className="chat-screen__body"><Markdown compact>{m.body}</Markdown></div>
              <div className="chat-screen__when">{timeAgo(m.created_at)} · {m.status}</div>
              <BoundVersions ids={m.artifact_version_ids ?? []} />
              {m.reason && <p className="error">{m.reason}</p>}
            </div>
          </article>
        ))}
      </div>
      <form
        className="composer question-thread__form"
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
          <button
            className="question-thread__answer"
            type="submit"
            disabled={
              !body.trim() ||
              command.isPending ||
              (!orchestrator && mode === "message")
            }
          >
            {mode === "question" ? "Open question" : "Send message"}
          </button>
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
    </section>
  );
}
function Question({ question: q, draft, readOnly = false }: {
  question: CreativeQuestion;
  draft?: { body: string; setBody: Dispatch<SetStateAction<string>> };
  readOnly?: boolean;
}) {
  const command = useCommand();
  function send(action: "reply" | "answer", payload: unknown) {
    const submittedDraft = draft?.body;
    // Per-call mutate callbacks are skipped after a scope change unmounts the
    // thread. Promise completion still clears its owned draft after delivery.
    void command.mutateAsync(() => api.post(`/v1/questions/${q.id}/${action}`, payload)).then(
      () => draft?.setBody((current) => current === submittedDraft ? "" : current),
      () => { /* The mutation renders the error and keeps the draft for retry. */ },
    );
  }
  return (
    <div>
      <QuestionThreadView
        ordinal={q.ordinal ?? q.id}
        localRef={q.local_ref || `creative/Q${q.id}`}
        title={q.title}
        body={q.body}
        brief={q.brief}
        messages={q.messages ?? []}
        turnLabel={readOnly ? "Resolved" : q.your_turn ? "Your reply needed" : "Open"}
        turnWarn={!readOnly && q.your_turn}
        askerLabel={isHuman(q.asked_by) ? "you asked" : `${q.asked_by} asked`}
        options={q.options}
        stale={q.stale}
        busy={command.isPending}
        draft={draft}
        readOnly={readOnly}
        onClarify={(body) => send("reply", { body })}
        onAnswer={(body) => send("answer", { body })}
        onChoose={(choose) => send("answer", { choose })}
        onDismiss={() => send("answer", { dismiss: true })}
      >
        <BoundVersions ids={q.artifact_version_ids ?? []} />
      </QuestionThreadView>
      <ErrorNotice error={command.error} />
    </div>
  );
}
function ResolvedQuestion({ question: q }: { question: CreativeQuestion }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="questions-tab__resolved">
      <button type="button" className="questions-tab__resolved-row" aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}>
        <span className="questions-tab__resolved-tag">{q.local_ref || `creative/Q${q.id}`}</span>
        <span className="questions-tab__resolved-badge">{q.resolution || "resolved"}</span>
        <span className="questions-tab__resolved-text">{questionTitle(q)}</span>
        <span className="questions-tab__resolved-chevron">{expanded ? "▴" : "▾"}</span>
      </button>
      {expanded && <Question question={q} readOnly />}
    </div>
  );
}
