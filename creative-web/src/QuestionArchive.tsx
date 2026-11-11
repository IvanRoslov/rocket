import { useState, type Dispatch, type SetStateAction } from "react";
import { useQuery } from "@tanstack/react-query";
import { QuestionThreadView } from "../../web/src/components/QuestionThreadView";
import { isHuman } from "../../web/src/lib/participants";
import { questionTitle } from "../../web/src/lib/thread";
import "../../web/src/screens/task/QuestionsTab.css";
import { api, contextPath, useCommand } from "./api";
import type { Context, CreativeQuestion } from "./types";
import { BoundVersions } from "./BoundVersions";
import { Empty, ErrorNotice } from "./ui";

export function QuestionArchive({ context, readOnly = false }: { context: Context; readOnly?: boolean }) {
  const questions = useQuery({
    queryKey: ["creative", "questions", context.run_id, context.stage_id, context.item_id],
    queryFn: () => api.get<CreativeQuestion[]>(contextPath(context, "questions")),
    refetchInterval: 2000,
  });
  const [replies, setReplies] = useState<Record<number, string>>({});
  const sorted = [...(questions.data ?? [])].sort(
    (a, b) => (b.asked_at ?? b.id) - (a.asked_at ?? a.id) || b.id - a.id,
  );
  const draft = (id: number) => ({
    body: replies[id] ?? "",
    setBody: ((value) => setReplies((prev) => ({
      ...prev, [id]: typeof value === "function" ? value(prev[id] ?? "") : value,
    }))) as Dispatch<SetStateAction<string>>,
  });
  return (
    <section className="question-archive" aria-label="Old questions archive">
      <ErrorNotice error={questions.error} />
      {questions.isPending && <p>Loading questions…</p>}
      {questions.data?.length === 0 && <Empty>No questions in this context.</Empty>}
      <div className="questions-tab">
        {sorted.filter(q => q.status === "open").map(q => <Question readOnly={readOnly} key={q.id} question={q} draft={draft(q.id)} />)}
        {sorted.some(q => q.status !== "open") && <div className="questions-tab__resolved-label">Resolved</div>}
        {sorted.filter(q => q.status !== "open").map(q => <ResolvedQuestion key={q.id} question={q} />)}
      </div>
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
        turnLabel={readOnly ? (q.status === "open" ? "Archived" : "Resolved") : q.your_turn ? "Your reply needed" : "Open"}
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
