package cli

import (
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
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id,omitempty"`
	FeatureSlug string `json:"feature_slug,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
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
	ID        int64  `json:"id"`
	Author    string `json:"author,omitempty"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}

// questionRow is the JSON shape of a question and its thread, mirroring
// internal/api.questionResponse.
type questionRow struct {
	ID         int64                `json:"id"`
	TaskID     int64                `json:"task_id"`
	Ordinal    int                  `json:"ordinal"`
	AskedBy    string               `json:"asked_by"`
	Body       string               `json:"body"`
	Context    string               `json:"context,omitempty"`
	Status     string               `json:"status"`
	Resolution string               `json:"resolution,omitempty"`
	WhoseTurn  string               `json:"whose_turn,omitempty"`
	AskedAt    int64                `json:"asked_at"`
	ResolvedAt int64                `json:"resolved_at,omitempty"`
	Messages   []questionMessageRow `json:"messages"`
}

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
	cmd.AddCommand(newTaskDocCmd())
	cmd.AddCommand(newTaskLogCmd())
	cmd.AddCommand(newTaskAskCmd())
	cmd.AddCommand(newTaskQuestionsCmd())
	cmd.AddCommand(newTaskReplyCmd())
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

func newTaskAddCmd() *cobra.Command {
	var projectID string
	var parentID int64
	var description string
	var descFile string

	cmd := &cobra.Command{
		Use:   "add \"<title>\"",
		Short: "Создать новую задачу",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task add \"<title>\" [--project <id>] [--parent <id>] [--desc <md> | --desc-file <f>]"}
			}

			// Check that both --desc and --desc-file are not provided
			if description != "" && descFile != "" {
				return &usageError{message: "--desc and --desc-file are mutually exclusive"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			// If project not specified, try to get default
			if projectID == "" {
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

			reqBody := map[string]any{
				"title":   args[0],
				"project": projectID,
			}
			if description != "" {
				reqBody["description"] = description
			}
			if parentID != 0 {
				reqBody["parent_id"] = parentID
			}

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
	return cmd
}

func newTaskLsCmd() *cobra.Command {
	var status string
	var project string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Список задач",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket task ls [--status <s>] [--project <id>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			q := url.Values{}
			q.Set("board", "true")
			if status != "" {
				q.Set("status", status)
			}
			if project != "" {
				q.Set("project", project)
			}
			path := "/v1/tasks?" + q.Encode()

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
	return cmd
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

			if flags.JSON {
				return printJSON(cmd, task)
			}

			// Fetch docs and log
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

			renderTaskCard(task, docsResp.Docs, logResp.Log, cmd.OutOrStdout(), time.Now())
			return nil
		},
	}
}

func newTaskMoveCmd() *cobra.Command {
	return &cobra.Command{
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
			var resp taskRow
			if err := c.Patch(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("task #%d moved to %s\n", resp.ID, resp.Status)
			return nil
		},
	}
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

	cmd := &cobra.Command{
		Use:   "log <id> <text>",
		Short: "Добавить запись в журнал задачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return &usageError{message: "usage: rocket task log <id> --kind <k> \"<text>\""}
			}

			if kind == "" {
				return &usageError{message: "usage: rocket task log <id> --kind <k> \"<text>\""}
			}

			taskID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			text := args[1]

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
	return cmd
}

func newTaskAskCmd() *cobra.Command {
	var context string

	cmd := &cobra.Command{
		Use:   "ask <task-id> \"<вопрос>\"",
		Short: "Задать вопрос пользователю по задаче",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket task ask <task-id> \"<вопрос>\" [--context <md>]"}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"body": args[1]}
			if context != "" {
				reqBody["context"] = context
			}

			path := apiPath("v1", "tasks", args[0], "questions")
			var resp questionRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("question Q%d (#%d) opened\n", resp.Ordinal, resp.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&context, "context", "", "дополнительный контекст (MD)")
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
	cmd := &cobra.Command{
		Use:   "reply <question-id> \"<текст>\"",
		Short: "Ответить в тред вопроса",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket task reply <question-id> \"<текст>\""}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid question id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"body": args[1]}
			path := apiPath("v1", "questions", args[0], "reply")
			var resp questionRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("reply added to Q%d (#%d)\n", resp.Ordinal, resp.ID)
			return nil
		},
	}
	return cmd
}

func newTaskAnswerCmd() *cobra.Command {
	var dismiss bool

	cmd := &cobra.Command{
		Use:   "answer <question-id> [\"<ответ>\"]",
		Short: "Ответить на вопрос или закрыть его без ответа",
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := &usageError{message: "usage: rocket task answer <question-id> \"<ответ>\" | --dismiss (exactly one)"}
			if len(args) < 1 || len(args) > 2 {
				return usage
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid question id"}
			}

			hasBody := len(args) == 2
			if hasBody == dismiss {
				return usage
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{}
			if dismiss {
				reqBody["dismiss"] = true
			} else {
				reqBody["body"] = args[1]
			}

			path := apiPath("v1", "questions", args[0], "answer")
			var resp questionRow
			if err := c.Post(path, reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			if dismiss {
				cmd.Printf("question Q%d (#%d) dismissed\n", resp.Ordinal, resp.ID)
			} else {
				cmd.Printf("question Q%d (#%d) answered\n", resp.Ordinal, resp.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dismiss, "dismiss", false, "закрыть вопрос без ответа")
	return cmd
}

// renderQuestions renders a task's questions block: a "task #<id>" header
// followed by, per question, a header line "Q<ordinal> (#<id>) [status]
// <arrow>" (arrow indicates whose turn it is to speak, empty when
// resolved), the indented question body, an optional indented context
// line, and indented thread lines ("  [user] ..." / "  [<session>] ...").
// Returns "" if qs is empty (callers should skip empty tasks entirely).
func renderQuestions(taskID int64, qs []questionRow) string {
	if len(qs) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "task #%d\n", taskID)
	for _, q := range qs {
		arrow := ""
		switch q.WhoseTurn {
		case "user":
			arrow = " → ждёт ответа пользователя"
		case "orchestrator":
			arrow = " → ждёт оркестратора"
		}
		fmt.Fprintf(&sb, "Q%d (#%d) [%s]%s\n", q.Ordinal, q.ID, q.Status, arrow)
		fmt.Fprintf(&sb, "  %s\n", q.Body)
		if q.Context != "" {
			fmt.Fprintf(&sb, "  context: %s\n", q.Context)
		}
		for _, m := range q.Messages {
			author := m.Author
			if author == "" {
				author = "user"
			}
			fmt.Fprintf(&sb, "  [%s] %s\n", author, m.Body)
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
			fmt.Fprintf(w, "%s\n", line)
		}

		// Show trimmed message if tasks were cut
		if trimmedCount > 0 {
			fmt.Fprintf(w, "  … and %d more (use --status %s)\n", trimmedCount, s.key)
		}
	}
}

// renderTaskCard writes a detailed card view for a task to w.
func renderTaskCard(task taskDetailRow, docs []taskDocRow, logs []taskLogRow, w io.Writer, now time.Time) {
	// Header: #id title (status)
	fmt.Fprintf(w, "# #%d %s (%s)\n\n", task.ID, task.Title, task.Status)

	// Description
	if task.Description != "" {
		fmt.Fprintf(w, "## Description\n%s\n\n", task.Description)
	}

	// Basic info
	fmt.Fprintf(w, "## Info\n")
	fmt.Fprintf(w, "Project: %s\n", task.ProjectID)
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
		fmt.Fprintf(tw, "ID\tTITLE\tSTATUS\tREPO\tSESSION\n")
		for _, st := range task.Subtasks {
			repo := st.RepoID
			if st.RepoID == "" {
				repo = "-"
			}
			session := st.SessionID
			if session == "" {
				session = "-"
			}
			fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\n", st.ID, st.Title, st.Status, repo, session)
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

	// Open questions
	if task.OpenQuestions > 0 {
		fmt.Fprintf(w, "## Open Questions\n%d\n", task.OpenQuestions)
	}
}

func formatAttach(attach []string) string {
	if len(attach) == 0 {
		return ""
	}
	return attach[0]
}
