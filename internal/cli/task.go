package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

// taskRow represents a task as returned by the API for ls rendering.
type taskRow struct {
	ID int64 `json:"id"`
	// ParentID is 0 for a root task — the one an orchestrator runs.
	ParentID    int64  `json:"parent_id,omitempty"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id,omitempty"`
	FeatureSlug string `json:"feature_slug,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Milestone   bool   `json:"milestone,omitempty"`
	// AssignedRole is the persistent agent holding a milestone, empty when
	// nobody has taken it.
	AssignedRole string `json:"assigned_role,omitempty"`
	PRNumber     int    `json:"pr_number,omitempty"`
	PRState      string `json:"pr_state,omitempty"`
	CIState      string `json:"ci_state,omitempty"`
	// WaitingTerminal is the API's derived flag: the task's session has been
	// sitting on interactive input long enough that nothing moves until
	// somebody types.
	WaitingTerminal bool `json:"waiting_terminal,omitempty"`
}

// waitingTerminalGlyph flags a task or session stalled waiting for a
// keystroke; the two marks are its spelled-out forms, used where there is room
// for one (the free-form task board line) rather than in a table cell.
//
// An orchestrator gets a blunter wording than a worker on purpose (task #1023
// §«Input stall»): a worker may legitimately be answering a question, whereas
// an orchestrator must never wait on a terminal prompt at all — for it the
// state is a fault, not a mode.
const (
	waitingTerminalGlyph = "⏳"
	waitingTerminalMark  = waitingTerminalGlyph + " ждёт ответа в терминале"
	waitingTerminalOrch  = waitingTerminalGlyph + " ждёт ввода в терминале — вероятно, застрял"
)

// waitingMarkFor picks the wording for a task line: the root task of a feature
// is the one an orchestrator runs, and only it gets the "probably stuck"
// reading.
func waitingMarkFor(t taskRow) string {
	if t.ParentID == 0 {
		return waitingTerminalOrch
	}
	return waitingTerminalMark
}

// taskDetailRow represents a full task detail as returned by GET /v1/tasks/{id}.
type taskDetailRow struct {
	ID            int64              `json:"id"`
	ParentID      int64              `json:"parent_id,omitempty"`
	Title         string             `json:"title"`
	Description   string             `json:"description,omitempty"`
	ProjectID     string             `json:"project_id"`
	RepoID        string             `json:"repo_id,omitempty"`
	Status        string             `json:"status"`
	FeatureSlug   string             `json:"feature_slug,omitempty"`
	SessionID     string             `json:"session_id,omitempty"`
	CreatedBy     string             `json:"created_by"`
	CreatedAt     int64              `json:"created_at"`
	UpdatedAt     int64              `json:"updated_at"`
	CompletedAt   int64              `json:"completed_at,omitempty"`
	Milestone     bool               `json:"milestone,omitempty"`
	AssignedRole  string             `json:"assigned_role,omitempty"`
	Subtasks      []taskRow          `json:"subtasks"`
	Session       *taskSessionDetail `json:"session,omitempty"`
	OpenQuestions int                `json:"open_questions"`
}

type taskSessionDetail struct {
	ID       string   `json:"id"`
	TmuxName string   `json:"tmux_name"`
	Attach   []string `json:"attach"`
}

// taskDocRow represents a task doc as returned by the API.
type taskDocRow struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Version   int64  `json:"version"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// taskLogRow represents a task log entry as returned by the API.
type taskLogRow struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// questionMessageRow is the JSON shape of a single entry in a question's
// thread, mirroring internal/api.questionMessageResponse.
type questionMessageRow struct {
	ID     int64  `json:"id"`
	Author string `json:"author,omitempty"`
	Kind   string `json:"kind"`
	Body   string `json:"body"`
	// AddressedTo narrows who is expected to respond. Empty means every
	// participant except the author.
	AddressedTo []string `json:"addressed_to,omitempty"`
	CreatedAt   int64    `json:"created_at"`
}

// questionRow is the JSON shape of a question and its thread, mirroring
// internal/api.questionResponse.
type questionRow struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	Ordinal    int    `json:"ordinal"`
	AskedBy    string `json:"asked_by"`
	Body       string `json:"body"`
	Context    string `json:"context,omitempty"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	// Participants is everyone taking part in the thread; WaitingOn is the
	// subset expected to speak next; YourTurn says whether this CLI's caller
	// is one of them. WhoseTurn is the pre-participant field the CLI no longer
	// prints but keeps on the wire for web and mobile — subtask #736 retires it.
	Participants []string `json:"participants,omitempty"`
	WaitingOn    []string `json:"waiting_on,omitempty"`
	YourTurn     bool     `json:"your_turn,omitempty"`
	WhoseTurn    string   `json:"whose_turn,omitempty"`
	// Attention is WaitingOn under its stored name (task #1023); Type is
	// decision|fyi; Options are the answer choices --choose picks from;
	// LocalRef is the one id printed and typed back, e.g. "1023/Q2".
	Attention  []string             `json:"attention,omitempty"`
	Type       string               `json:"type,omitempty"`
	Options    []string             `json:"options,omitempty"`
	LocalRef   string               `json:"local_ref,omitempty"`
	AskedAt    int64                `json:"asked_at"`
	ResolvedAt int64                `json:"resolved_at,omitempty"`
	Messages   []questionMessageRow `json:"messages"`
	// Echo is the target confirmation a write returns; DryRun marks a write
	// that was only rehearsed. Both are absent on a read.
	Echo   string `json:"echo,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// parseTo normalises --to values into participant ids. The flag is both
// comma-separated and repeatable, so "--to a,b --to c" and "--to a --to b,c"
// mean the same thing. Blank segments are dropped and duplicates collapse,
// preserving first-seen order so the request body is predictable.
func parseTo(vals []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

// setTo attaches the addressees to a thread-write request body. No --to means
// no "to" key at all rather than an empty array, which keeps the request
// byte-identical to what the CLI sent before the participant model existed.
// "to" decides who must RESPOND (waiting_on), never who gets notified: every
// participant is notified of every message regardless.
func setTo(reqBody map[string]any, to []string) {
	if len(to) > 0 {
		reqBody["to"] = to
	}
}

// toFlagUsage is the shared --to help string, so all six thread-writing
// commands describe the flag identically.
const toFlagUsage = "кому адресован вопрос — id участников через запятую (можно повторять)"

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Управление задачами",
	}
	cmd.AddCommand(newTaskAddCmd())
	cmd.AddCommand(newTaskLsCmd())
	cmd.AddCommand(newTaskShowCmd())
	cmd.AddCommand(newTaskMoveCmd())
	cmd.AddCommand(newTaskCancelCmd())
	cmd.AddCommand(newTaskStartCmd())
	cmd.AddCommand(newTaskTakeCmd())
	cmd.AddCommand(newTaskAssignCmd())
	cmd.AddCommand(newTaskDocCmd())
	cmd.AddCommand(newTaskLogCmd())
	cmd.AddCommand(newTaskAskCmd())
	cmd.AddCommand(newTaskAskOrchCmd())
	cmd.AddCommand(newTaskQuestionsCmd())
	cmd.AddCommand(newTaskReplyCmd())
	cmd.AddCommand(newTaskCloseCmd(false))
	cmd.AddCommand(newTaskAnswerCmd())
	return cmd
}

// taskStartResponse is the JSON shape of POST /v1/tasks/{id}/start.
type taskStartResponse struct {
	TaskID      int64  `json:"task_id"`
	FeatureSlug string `json:"feature_slug"`
	SessionID   string `json:"session_id"`
}

func newTaskStartCmd() *cobra.Command {
	var agentName string

	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Запустить оркестратора для задачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task start <id> [--agent <name>]"}
			}

			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var reqBody map[string]any
			if agentName != "" {
				reqBody = map[string]any{"agent": agentName}
			}

			path := apiPath("v1", "tasks", args[0], "start")
			var resp taskStartResponse
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("TASK=#%d\n", resp.TaskID)
			cmd.Printf("SLUG=%s\n", resp.FeatureSlug)
			cmd.Printf("SESSION=%s\n", resp.SessionID)
			cmd.Printf("attach: rocket attach %s\n", resp.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "имя агента (по умолчанию — из конфига)")
	return cmd
}

// needsProjectDefault reports whether `rocket task add` must resolve a
// default project from the daemon before creating the task. It's only
// needed when --project wasn't given AND there's no --parent: with a
// parent, the API inherits the project from the parent task, so sending an
// empty project is correct (and required, since --parent may target a task
// in a project other than the CLI's guessed default).
// A milestone belongs to no project at all, so it never resolves one.
func needsProjectDefault(projectID string, parentID int64, milestone bool) bool {
	return projectID == "" && parentID == 0 && !milestone
}

// taskAddRequestBody builds the POST /v1/tasks body. A milestone carries no
// project key at all: the server rejects the pair outright.
func taskAddRequestBody(title, description, projectID string, parentID int64, milestone bool) map[string]any {
	body := map[string]any{"title": title}
	if milestone {
		body["milestone"] = true
	} else {
		body["project"] = projectID
	}
	if description != "" {
		body["description"] = description
	}
	if parentID != 0 {
		body["parent_id"] = parentID
	}
	return body
}

func newTaskAddCmd() *cobra.Command {
	var projectID string
	var parentID int64
	var description string
	var descFile string
	var milestone bool

	cmd := &cobra.Command{
		Use:   "add \"<title>\"",
		Short: "Создать новую задачу",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task add \"<title>\" [--project <id> | --milestone] [--parent <id>] [--desc <md> | --desc-file <f>]"}
			}

			// Check that both --desc and --desc-file are not provided
			if description != "" && descFile != "" {
				return &usageError{message: "--desc and --desc-file are mutually exclusive"}
			}
			// A milestone stands above projects: it belongs to none.
			if milestone && projectID != "" {
				return &usageError{message: "--milestone and --project are mutually exclusive: a milestone belongs to no project"}
			}
			if milestone && parentID != 0 {
				return &usageError{message: "--milestone and --parent are mutually exclusive: a milestone is not a subtask"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			// If project not specified and a parent task is given, leave it
			// empty — the API inherits the project from the parent task.
			// Otherwise, try to resolve a default project.
			if needsProjectDefault(projectID, parentID, milestone) {
				var projects []map[string]any
				if err := c.Get("/v1/projects", nil, &projects); err != nil {
					return err
				}
				if len(projects) == 1 {
					projectID = projects[0]["id"].(string)
				} else if len(projects) == 0 {
					return &usageError{message: "no projects found; specify --project"}
				} else {
					return &usageError{message: "multiple projects found; specify --project <id>"}
				}
			}

			// Read description from file if provided
			if descFile != "" {
				data, err := os.ReadFile(descFile)
				if err != nil {
					return fmt.Errorf("failed to read description file: %w", err)
				}
				description = string(data)
			}

			reqBody := taskAddRequestBody(args[0], description, projectID, parentID, milestone)

			var resp taskRow
			if err := c.Post("/v1/tasks", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("task #%d created\n", resp.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "id проекта")
	cmd.Flags().Int64Var(&parentID, "parent", 0, "id родительской задачи")
	cmd.Flags().StringVar(&description, "desc", "", "описание задачи (MD)")
	cmd.Flags().StringVar(&descFile, "desc-file", "", "файл с описанием задачи (MD)")
	cmd.Flags().BoolVar(&milestone, "milestone", false,
		"создать майлстон: задачу вне проектов, которую берёт постоянный агент")
	return cmd
}

// taskLsQuery builds the GET /v1/tasks query for `task ls`. The board grouping
// is always on: the human view is a kanban.
func taskLsQuery(status, project string, milestones bool) string {
	q := url.Values{}
	q.Set("board", "true")
	if status != "" {
		q.Set("status", status)
	}
	if project != "" {
		q.Set("project", project)
	}
	if milestones {
		q.Set("milestones", "true")
	}
	return q.Encode()
}

func newTaskLsCmd() *cobra.Command {
	var status string
	var project string
	var milestones bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Список задач",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket task ls [--status <s>] [--project <id>] [--milestones]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := "/v1/tasks?" + taskLsQuery(status, project, milestones)

			var resp struct {
				Board map[string][]taskRow `json:"board"`
			}
			if err := c.Get(path, nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}

			statusFiltered := status != ""
			renderTaskBoard(resp.Board, cmd.OutOrStdout(), statusFiltered)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "фильтр по статусу")
	cmd.Flags().StringVar(&project, "project", "", "фильтр по id проекта")
	cmd.Flags().BoolVar(&milestones, "milestones", false, "показать только майлстоны")
	return cmd
}

// taskShowJSON is what `rocket task show --json` emits: the task's own
// fields inlined at the top level, plus the three collections the human
// card also shows.
//
// Inlining rather than nesting under a "task" key is deliberate — web and
// mobile already read id/title/subtasks from the top level, and --json may
// only gain keys, never move them. Before this type existed --json returned
// the bare task row and was strictly poorer than the text output it is
// supposed to mirror.
type taskShowJSON struct {
	taskDetailRow
	Docs      []taskDocRow  `json:"docs"`
	Log       []taskLogRow  `json:"log"`
	Questions []questionRow `json:"questions"`
}

func newTaskShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Показать задачу",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task show <id>"}
			}

			_, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := apiPath("v1", "tasks", args[0])
			var task taskDetailRow
			if err := c.Get(path, nil, &task); err != nil {
				return err
			}

			// Docs, log and questions are fetched for --json too: the human
			// card shows all three, so a --json that skipped them would be
			// strictly less informative than the text output.
			var docsResp struct {
				Docs []taskDocRow `json:"docs"`
			}
			if err := c.Get(path+"/docs", nil, &docsResp); err != nil {
				return err
			}

			var logResp struct {
				Log []taskLogRow `json:"log"`
			}
			if err := c.Get(path+"/log", nil, &logResp); err != nil {
				return err
			}

			questions, err := fetchQuestions(c, args[0], false)
			if err != nil {
				return err
			}

			// Machine consumers want an empty array, not a missing key or a
			// null: `jq '.log[]'` must work on a fresh task too.
			docs := docsResp.Docs
			if docs == nil {
				docs = []taskDocRow{}
			}
			logEntries := logResp.Log
			if logEntries == nil {
				logEntries = []taskLogRow{}
			}
			if questions == nil {
				questions = []questionRow{}
			}

			if flags.JSON {
				return printJSON(cmd, taskShowJSON{
					taskDetailRow: task,
					Docs:          docs,
					Log:           logEntries,
					Questions:     questions,
				})
			}

			renderTaskCard(task, docs, logEntries, questions, cmd.OutOrStdout(), time.Now())
			return nil
		},
	}
}

// patchTaskResponseRow mirrors internal/api.patchTaskResponse: taskRow plus
// an optional cleaned_up flag, set only when a root task was moved to done.
type patchTaskResponseRow struct {
	taskRow
	CleanedUp *bool `json:"cleaned_up,omitempty"`
}

// renderMoveError turns a review-blocked 409 into a friendlier message that
// surfaces the --force hint; other errors pass through unchanged.
func renderMoveError(err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "review_blocked" {
		return fmt.Errorf("cannot move to review: %s\nhint: rerun with --force to override", apiErr.Message)
	}
	return err
}

func newTaskMoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "move <id> <status>",
		Short: "Переместить задачу в другой статус",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket task move <id> <status>"}
			}

			_, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			if args[1] == "cancelled" {
				return &usageError{message: "use `rocket task cancel <id>` to cancel — it also stops sessions"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"status": args[1],
			}

			path := apiPath("v1", "tasks", args[0])
			if force {
				path += "?force=true"
			}
			var resp patchTaskResponseRow
			if err := c.Patch(path, reqBody, &resp); err != nil {
				return renderMoveError(err)
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("task #%d moved to %s\n", resp.ID, resp.Status)
			if resp.CleanedUp != nil && *resp.CleanedUp {
				cmd.Println("orchestrator cleaned up")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "переместить в review, несмотря на открытые подзадачи/живых воркеров")
	return cmd
}

func newTaskCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Отменить задачу",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task cancel <id>"}
			}

			_, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := apiPath("v1", "tasks", args[0], "cancel")
			var resp taskRow
			if err := c.Post(path, nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("task #%d cancelled\n", resp.ID)
			return nil
		},
	}
}

func newTaskDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Управление документами задачи",
	}
	cmd.AddCommand(newTaskDocPutCmd())
	return cmd
}

func newTaskDocPutCmd() *cobra.Command {
	var kind string
	var title string
	var file string

	cmd := &cobra.Command{
		Use:   "put <id>",
		Short: "Добавить документ к задаче",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task doc put <id> --kind <k> --title \"<t>\" --file <f.md>"}
			}

			if kind == "" || title == "" || file == "" {
				return &usageError{message: "usage: rocket task doc put <id> --kind <k> --title \"<t>\" --file <f.md>"}
			}

			taskID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			// Read file content
			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}

			reqBody := map[string]any{
				"kind":  kind,
				"title": title,
				"body":  string(data),
			}

			path := apiPath("v1", "tasks", strconv.FormatInt(taskID, 10), "docs")
			var resp taskDocRow
			if err := c.Put(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("doc added: %s v%d\n", resp.Kind, resp.Version)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "тип документа (spec|plan|report|doc)")
	cmd.Flags().StringVar(&title, "title", "", "название документа")
	cmd.Flags().StringVar(&file, "file", "", "файл с содержимым документа")
	return cmd
}

func newTaskLogCmd() *cobra.Command {
	var kind string
	var file string

	const usage = "usage: rocket task log <id> --kind <k> \"<text>\" | --file <path>"

	cmd := &cobra.Command{
		Use:   "log <id> [\"<text>\"]",
		Short: "Добавить запись в журнал задачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			if kind == "" {
				return &usageError{message: usage}
			}

			taskID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			text, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"kind": kind,
				"body": text,
			}

			path := apiPath("v1", "tasks", strconv.FormatInt(taskID, 10), "log")
			var resp taskLogRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("log entry added\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "тип записи (decision|problem|note|status)")
	cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")
	return cmd
}

// newTaskAskCmd builds "rocket task ask": opens a question thread FROM the
// task's own orchestrator TO the human. Called by the orchestrator agent
// (its session id is auto-attached via $ROCKET_SESSION_ID); the server
// rejects any other caller.
func newTaskAskCmd() *cobra.Command {
	var context string
	var to []string
	var file string
	var options []string
	var fyi bool

	const usage = "usage: rocket task ask <task-id> \"<вопрос>\" | --file <path> [--context <md>] [--to <id,...>] [--option <текст>]... [--fyi]"

	cmd := &cobra.Command{
		Use:   "ask <task-id> [\"<вопрос>\"]",
		Short: "Задать вопрос пользователю по задаче (от имени оркестратора этой задачи)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}
			if err := validateAskFlags(options, fyi, usage); err != nil {
				return err
			}

			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}

			if os.Getenv("ROCKET_SESSION_ID") == "" {
				return errors.New("rocket task ask is for orchestrators asking the human; to ask the orchestrator a question use: rocket task ask-orch")
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := askRequestBody(body, context, parseTo(to), options, fyi)

			path := apiPath("v1", "tasks", args[0], "questions")
			var resp questionRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("тред %s открыт\n", resp.ref())
			return nil
		},
	}
	cmd.Flags().StringVar(&context, "context", "", "дополнительный контекст (MD)")
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с вопросом ('-' — stdin)")
	cmd.Flags().StringArrayVar(&options, "option", nil, optionFlagUsage)
	cmd.Flags().BoolVar(&fyi, "fyi", false, fyiFlagUsage)
	return cmd
}

// newTaskAskOrchCmd builds "rocket task ask-orch": the reverse direction —
// the HUMAN opens a question thread addressed to the task's orchestrator.
// The question body is injected into the orchestrator's message queue, so
// it reaches the agent right away instead of waiting to be polled. The
// orchestrator replies in-thread (rocket task reply); only the human who
// opened it can resolve it (rocket task answer).
func newTaskAskOrchCmd() *cobra.Command {
	var context string
	var to []string
	var file string
	var options []string
	var fyi bool

	const usage = "usage: rocket task ask-orch <task-id> \"<вопрос>\" | --file <path> [--context <md>] [--to <id,...>] [--option <текст>]... [--fyi]"

	cmd := &cobra.Command{
		Use:   "ask-orch <task-id> [\"<вопрос>\"]",
		Short: "Задать вопрос оркестратору задачи (от пользователя)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}
			if err := validateAskFlags(options, fyi, usage); err != nil {
				return err
			}

			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := askRequestBody(body, context, parseTo(to), options, fyi)

			path := apiPath("v1", "tasks", args[0], "questions")
			var resp questionRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("тред %s открыт для оркестратора\n", resp.ref())
			return nil
		},
	}
	cmd.Flags().StringVar(&context, "context", "", "дополнительный контекст (MD)")
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с вопросом ('-' — stdin)")
	cmd.Flags().StringArrayVar(&options, "option", nil, optionFlagUsage)
	cmd.Flags().BoolVar(&fyi, "fyi", false, fyiFlagUsage)
	return cmd
}

// fetchQuestions retrieves the questions thread for a task, optionally
// filtered to open-only.
func fetchQuestions(c *client.Client, taskID string, openOnly bool) ([]questionRow, error) {
	q := url.Values{}
	if openOnly {
		q.Set("status", "open")
	}
	path := apiPath("v1", "tasks", taskID, "questions")
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp struct {
		Questions []questionRow `json:"questions"`
	}
	if err := c.Get(path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Questions, nil
}

func newTaskQuestionsCmd() *cobra.Command {
	var openOnly bool

	cmd := &cobra.Command{
		Use:   "questions [<task-id>]",
		Short: "Показать вопросы задачи (или всех корневых задач)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return &usageError{message: "usage: rocket task questions [<task-id>] [--open]"}
			}
			if len(args) == 1 {
				if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
					return &usageError{message: "invalid task id"}
				}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				taskID, _ := strconv.ParseInt(args[0], 10, 64)
				qs, err := fetchQuestions(c, args[0], openOnly)
				if err != nil {
					return err
				}

				if flags.JSON {
					return printJSON(cmd, qs)
				}
				cmd.Print(renderQuestions(taskID, qs))
				return nil
			}

			// No task id given: iterate root tasks.
			var listResp struct {
				Tasks []taskRow `json:"tasks"`
			}
			if err := c.Get("/v1/tasks", nil, &listResp); err != nil {
				return err
			}

			if flags.JSON {
				out := map[string][]questionRow{}
				for _, t := range listResp.Tasks {
					qs, err := fetchQuestions(c, strconv.FormatInt(t.ID, 10), openOnly)
					if err != nil {
						return err
					}
					if len(qs) > 0 {
						out[strconv.FormatInt(t.ID, 10)] = qs
					}
				}
				return printJSON(cmd, out)
			}

			var sb strings.Builder
			for _, t := range listResp.Tasks {
				qs, err := fetchQuestions(c, strconv.FormatInt(t.ID, 10), openOnly)
				if err != nil {
					return err
				}
				if len(qs) == 0 {
					continue
				}
				sb.WriteString(renderQuestions(t.ID, qs))
			}
			cmd.Print(sb.String())
			return nil
		},
	}
	cmd.Flags().BoolVar(&openOnly, "open", false, "показать только открытые вопросы")
	return cmd
}

func newTaskReplyCmd() *cobra.Command {
	var opts threadReplyOptions
	var taskFlag int64
	var file string

	const usage = "usage: rocket task reply <task-id>/Q<n>|<question-id> \"<текст>\" | --file <path> " +
		"[--task <task-id>] [--to <id,...>] [--dry-run] [--join]"

	cmd := &cobra.Command{
		Use:   "reply <task-id>/Q<n>|<question-id> [\"<текст>\"]",
		Short: "Ответить в тред вопроса",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			id, err := resolveQuestionRef(args[0], taskFlag)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			opts.body = body
			opts.to = parseTo(opts.to)
			var resp questionRow
			if err := c.Post(apiPath("v1", "questions", id, "reply"), opts.requestBody(), &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderWriteResult("реплика добавлена в", resp))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&opts.to, "to", nil, toFlagUsage)
	cmd.Flags().Int64Var(&taskFlag, "task", 0, taskFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, dryRunFlagUsage)
	cmd.Flags().BoolVar(&opts.join, "join", false, joinFlagUsage)
	return cmd
}

// newTaskCloseCmd builds "rocket task close": the one verb that ends a thread,
// with an answer, with a choice among its options, or as no longer relevant.
// It replaced "answer" plus its --dismiss flag, which spelled one act two ways.
func newTaskCloseCmd(hidden bool) *cobra.Command {
	var opts threadCloseOptions
	var taskFlag int64
	var file string

	name := "close"
	if hidden {
		// The pre-#1023 name, kept working for the scripts and prompts that
		// still say it, but out of the help so only one verb is taught.
		name = "answer"
	}

	usage := "usage: rocket task " + name + " <task-id>/Q<n>|<question-id> \"<резолюция>\" | --file <path> | " +
		"--choose <n> | --dismiss [\"<почему>\"] (ровно одно) [--task <task-id>] [--to <id,...>] [--dry-run] [--join]"

	cmd := &cobra.Command{
		Use:    name + " <task-id>/Q<n>|<question-id> [\"<резолюция>\"]",
		Short:  "Закрыть тред: ответом, выбором варианта или как неактуальный",
		Hidden: hidden,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			// The text is optional here — --choose and a bare --dismiss carry
			// no body — so it is only read when one of its sources is present.
			hasBody := len(args) == 2
			if hasBody || file != "" {
				body, err := textBody(cmd, argAt(args, 1), hasBody, file, usage)
				if err != nil {
					return err
				}
				opts.body = body
			}
			if err := opts.validate(usage); err != nil {
				return err
			}

			id, err := resolveQuestionRef(args[0], taskFlag)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			opts.to = parseTo(opts.to)
			var resp questionRow
			if err := c.Post(apiPath("v1", "questions", id, "answer"), opts.requestBody(), &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderWriteResult(closeAction(opts), resp))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.dismiss, "dismiss", false, dismissFlagUsage)
	cmd.Flags().IntVar(&opts.choose, "choose", 0, chooseFlagUsage)
	cmd.Flags().StringSliceVar(&opts.to, "to", nil, toFlagUsage)
	cmd.Flags().Int64Var(&taskFlag, "task", 0, taskFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с резолюцией ('-' — stdin)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, dryRunFlagUsage)
	cmd.Flags().BoolVar(&opts.join, "join", false, joinFlagUsage)
	return cmd
}

// newTaskAnswerCmd is the pre-#1023 name of "task close", kept so the scripts
// and prompts that still spell it keep working. Same command, hidden from help.
func newTaskAnswerCmd() *cobra.Command { return newTaskCloseCmd(true) }

// closeAction names what a close did, so the result line reads as the act the
// user asked for rather than a generic "closed".
func closeAction(opts threadCloseOptions) string {
	if opts.dismiss {
		return "тред закрыт как неактуальный:"
	}
	return "тред закрыт:"
}

// threadTurnArrow renders the header suffix naming who is expected to speak
// next. It replaces the pre-participant whose_turn arrow: with several
// participants "the orchestrator" is no longer something a single word can
// name. Nobody awaited — a resolved thread, or a server that predates the
// participant model — renders nothing, as the old arrow did.
func threadTurnArrow(waiting []string, yourTurn bool) string {
	if len(waiting) == 0 {
		return ""
	}
	arrow := " → ждут: " + strings.Join(waiting, ", ")
	if yourTurn {
		arrow += " (ваш ход)"
	}
	return arrow
}

// threadAuthorLabel renders a message author for a thread line. The human is
// spelled "human" on the wire since subtask #736; older daemons and cached
// payloads still say "", so both render as "user", the word the CLI has
// always shown.
func threadAuthorLabel(author string) string {
	if author == "" || author == "human" {
		return "user"
	}
	return author
}

// threadRef renders the id a thread is printed and typed back under. The
// server sends it as local_ref; a daemon that predates task #1023 sends none,
// so it is rebuilt from the scope and the ordinal the CLI already has. Either
// way the global id no longer appears in human output — a header carrying two
// numbers at once is what got replies misdelivered.
func threadRef(localRef, scope string, ordinal int) string {
	if localRef != "" {
		return localRef
	}
	return fmt.Sprintf("%s/Q%d", scope, ordinal)
}

// threadStatusLabel renders the bracketed state of a thread. An fyi thread is
// born resolved, so spelling it "resolved" would hide the one thing that
// matters about it: nobody has to act on it.
func threadStatusLabel(status, threadType string) string {
	if threadType == "fyi" {
		return "fyi"
	}
	return status
}

// renderThreadOptions writes the answer choices, numbered exactly as
// "close --choose <n>" indexes them.
func renderThreadOptions(sb *strings.Builder, options []string) {
	if len(options) == 0 {
		return
	}
	parts := make([]string, len(options))
	for i, o := range options {
		parts[i] = fmt.Sprintf("%d) %s", i+1, o)
	}
	fmt.Fprintf(sb, "  варианты: %s\n", strings.Join(parts, "  "))
}

// renderParticipantsLine writes the thread's participant line, omitted when
// the server sent none.
func renderParticipantsLine(sb *strings.Builder, participants []string) {
	if len(participants) > 0 {
		fmt.Fprintf(sb, "  участники: %s\n", strings.Join(participants, ", "))
	}
}

// renderThreadMessage writes one thread line: "  [author] body", or
// "  [author → a, b] body" when the message is addressed at somebody in
// particular. Shared by task and role threads, which carry the same message
// shape.
func renderThreadMessage(sb *strings.Builder, m questionMessageRow) {
	frame := threadAuthorLabel(m.Author)
	if len(m.AddressedTo) > 0 {
		frame += " → " + strings.Join(m.AddressedTo, ", ")
	}
	fmt.Fprintf(sb, "  [%s] %s\n", frame, m.Body)
}

// renderQuestions renders a task's questions block: a "task #<id>" header
// followed by, per question, a header line "Q<ordinal> (#<id>) [status]
// <arrow>" (arrow names who is awaited, empty when nobody is), the indented
// question body, an optional indented context line, an optional participants
// line, and indented thread lines ("  [user] ..." / "  [<session>] ...").
// Returns "" if qs is empty (callers should skip empty tasks entirely).
func renderQuestions(taskID int64, qs []questionRow) string {
	if len(qs) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "task #%d\n", taskID)
	for _, q := range qs {
		fmt.Fprintf(&sb, "%s [%s]%s\n",
			threadRef(q.LocalRef, strconv.FormatInt(taskID, 10), q.Ordinal),
			threadStatusLabel(q.Status, q.Type),
			threadTurnArrow(q.WaitingOn, q.YourTurn))
		fmt.Fprintf(&sb, "  %s\n", q.Body)
		if q.Context != "" {
			fmt.Fprintf(&sb, "  context: %s\n", q.Context)
		}
		renderThreadOptions(&sb, q.Options)
		renderParticipantsLine(&sb, q.Participants)
		for _, m := range q.Messages {
			renderThreadMessage(&sb, m)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// renderTaskBoard writes a kanban board view to w, grouping tasks by status.
// Renders statuses in order: backlog, in_progress, review, done, cancelled.
// Skips empty groups; shows done/cancelled only if they have tasks.
// When statusFiltered is false, trims done/cancelled to last 5 tasks (highest ids).
func renderTaskBoard(board map[string][]taskRow, w io.Writer, statusFiltered bool) {
	// Define status order and labels
	statuses := []struct {
		key   string
		label string
	}{
		{"backlog", "BACKLOG"},
		{"in_progress", "IN PROGRESS"},
		{"review", "REVIEW"},
		{"done", "DONE"},
		{"cancelled", "CANCELLED"},
	}

	for _, s := range statuses {
		tasks, ok := board[s.key]
		if !ok || len(tasks) == 0 {
			continue
		}

		// Trim done/cancelled to last 5 if not status-filtered
		displayTasks := tasks
		trimmedCount := 0
		if !statusFiltered && (s.key == "done" || s.key == "cancelled") && len(tasks) > 5 {
			trimmedCount = len(tasks) - 5
			displayTasks = tasks[trimmedCount:]
		}

		fmt.Fprintf(w, "\n%s\n", s.label)
		fmt.Fprintf(w, "%s\n", "---")

		for _, t := range displayTasks {
			line := fmt.Sprintf("#%d %s", t.ID, t.Title)
			if t.FeatureSlug != "" {
				line += fmt.Sprintf(" [%s]", t.FeatureSlug)
			}
			if t.SessionID != "" {
				line += fmt.Sprintf(" [%s]", t.SessionID)
			}
			if t.WaitingTerminal {
				line += " " + waitingMarkFor(t)
			}
			fmt.Fprintf(w, "%s\n", line)
		}

		// Show trimmed message if tasks were cut
		if trimmedCount > 0 {
			fmt.Fprintf(w, "  … and %d more (use --status %s)\n", trimmedCount, s.key)
		}
	}
}

// renderTaskCard writes a detailed card view for a task to w.
func renderTaskCard(task taskDetailRow, docs []taskDocRow, logs []taskLogRow, questions []questionRow, w io.Writer, now time.Time) {
	// Header: #id title (status)
	fmt.Fprintf(w, "# #%d %s (%s)\n\n", task.ID, task.Title, task.Status)

	// Description
	if task.Description != "" {
		fmt.Fprintf(w, "## Description\n%s\n\n", task.Description)
	}

	// Basic info
	fmt.Fprintf(w, "## Info\n")
	if task.Milestone {
		renderMilestoneInfo(w, task.AssignedRole)
	} else {
		fmt.Fprintf(w, "Project: %s\n", task.ProjectID)
	}
	if task.RepoID != "" {
		fmt.Fprintf(w, "Repo: %s\n", task.RepoID)
	}
	if task.FeatureSlug != "" {
		fmt.Fprintf(w, "Feature: %s\n", task.FeatureSlug)
	}
	if task.Session != nil {
		fmt.Fprintf(w, "Session: %s (%s)\n", task.Session.ID, task.Session.TmuxName)
		if len(task.Session.Attach) > 0 {
			fmt.Fprintf(w, "Attach: %s\n", formatAttach(task.Session.Attach))
		}
	}
	fmt.Fprintf(w, "\n")

	// Subtasks
	if len(task.Subtasks) > 0 {
		fmt.Fprintf(w, "## Subtasks\n")
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tTITLE\tSTATUS\tREPO\tSESSION\tPR\tCI\n")
		for _, st := range task.Subtasks {
			repo := st.RepoID
			if st.RepoID == "" {
				repo = "-"
			}
			session := st.SessionID
			if session == "" {
				session = "-"
			}
			pr := "-"
			if st.PRNumber > 0 {
				pr = fmt.Sprintf("#%d (%s)", st.PRNumber, st.PRState)
			}
			ci := st.CIState
			if ci == "" {
				ci = "-"
			}
			fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\t%s\n", st.ID, st.Title, st.Status, repo, session, pr, ci)
		}
		tw.Flush()
		fmt.Fprintf(w, "\n")
	}

	// Docs
	if len(docs) > 0 {
		fmt.Fprintf(w, "## Docs\n")
		for _, d := range docs {
			fmt.Fprintf(w, "- %s \"%s\" v%d\n", d.Kind, d.Title, d.Version)
		}
		fmt.Fprintf(w, "\n")
	}

	// Log (tail of last 10)
	if len(logs) > 0 {
		fmt.Fprintf(w, "## Log\n")
		start := len(logs) - 10
		if start < 0 {
			start = 0
		}
		for i := start; i < len(logs); i++ {
			l := logs[i]
			author := l.Author
			if author == "" {
				author = "user"
			}
			ago := humanAge(l.CreatedAt, now)
			fmt.Fprintf(w, "[%s] %s (%s, %s ago)\n", l.Kind, l.Body, author, ago)
		}
		fmt.Fprintf(w, "\n")
	}

	// Questions: count line, followed by the inlined thread rendering.
	if task.OpenQuestions > 0 {
		fmt.Fprintf(w, "## Open Questions\n%d\n\n", task.OpenQuestions)
	}
	if len(questions) > 0 {
		fmt.Fprintf(w, "## Questions\n")
		fmt.Fprint(w, renderQuestions(task.ID, questions))
	}
}

func formatAttach(attach []string) string {
	if len(attach) == 0 {
		return ""
	}
	return attach[0]
}
