package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/spf13/cobra"
)

type creativeConnection func() (*client.Client, error)

func newCreativeCmd() *cobra.Command {
	return creativeCommands(func() (*client.Client, error) { c, _, err := connect(true); return c, err })
}
func creativeCommands(open creativeConnection) *cobra.Command {
	root := &cobra.Command{Use: "creative", Short: "Creative pipelines, runs and approvals (JSON output)"}
	pipelines := &cobra.Command{Use: "pipeline", Short: "Edit ordered pipeline templates"}
	pipelines.AddCommand(creativeListCommand(open, "pipelines"), creativeShowCommand(open, "pipelines"))
	pipelines.AddCommand(creativeFileCommand(open, "create", 0, "POST", func(_ []string) string { return "/v1/creative/pipelines" }))
	pipelines.AddCommand(creativeFileCommand(open, "update <pipeline>", 1, "PUT", func(a []string) string { return apiPath("v1", "creative", "pipelines", a[0]) }))
	runs := &cobra.Command{Use: "run", Short: "Create and inspect snapshotted runs"}
	runs.AddCommand(creativeListCommand(open, "runs"), creativeShowCommand(open, "runs"))
	runs.AddCommand(creativeFileCommand(open, "create", 0, "POST", func(_ []string) string { return "/v1/creative/runs" }))
	runs.AddCommand(creativeContextCommand(open), creativeOrchestratorCommand(open))
	stages := &cobra.Command{Use: "stage", Short: "Progress and review a run stage"}
	stages.AddCommand(creativeActionCommand(open, false), creativeProgressCommand(open, false))
	items := &cobra.Command{Use: "item", Short: "Manage repeated items and their approvals"}
	items.AddCommand(creativeFileCommand(open, "add <run> <stage>", 2, "POST", func(a []string) string { return creativeTargetPath(a) + "/items" }))
	items.AddCommand(creativeFileCommand(open, "update <run> <stage> <item>", 3, "PATCH", creativeTargetPath))
	items.AddCommand(creativeActionCommand(open, true), creativeProgressCommand(open, true))
	items.AddCommand(&cobra.Command{Use: "rm <run> <stage> <item>", Args: creativeArgs(3), RunE: func(cmd *cobra.Command, a []string) error {
		return creativeCall(cmd, open, "DELETE", creativeTargetPath(a), nil)
	}})
	root.AddCommand(pipelines, runs, stages, items)
	return root
}
func creativeArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, a []string) error {
		if len(a) != n {
			return &usageError{message: "usage: " + cmd.UseLine()}
		}
		for _, v := range a {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil || id <= 0 {
				return &usageError{message: "IDs must be positive integers"}
			}
		}
		return nil
	}
}
func creativeTargetPath(a []string) string {
	parts := []string{"v1", "creative", "runs", a[0]}
	if len(a) > 1 {
		parts = append(parts, "stages", a[1])
	}
	if len(a) > 2 {
		parts = append(parts, "items", a[2])
	}
	return apiPath(parts...)
}
func creativeCall(cmd *cobra.Command, open creativeConnection, method, path string, in any) error {
	c, err := open()
	if err != nil {
		return err
	}
	var out json.RawMessage
	switch method {
	case "GET":
		err = c.Get(path, nil, &out)
	case "POST":
		err = c.Post(path, in, &out)
	case "PUT":
		err = c.Put(path, in, &out)
	case "PATCH":
		err = c.Patch(path, in, &out)
	case "DELETE":
		err = c.Delete(path, nil, &out)
	default:
		return fmt.Errorf("unsupported creative method %s", method)
	}
	if err != nil {
		return err
	}
	return printJSON(cmd, out)
}
func creativeFileCommand(open creativeConnection, use string, n int, method string, path func([]string) string) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: use + " --file <json>", Short: "Write a creative definition from a JSON file", Args: creativeArgs(n), RunE: func(cmd *cobra.Command, a []string) error {
		if file == "" {
			return &usageError{message: "--file is required"}
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !json.Valid(data) || !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
			return &usageError{message: "--file must contain a JSON object"}
		}
		return creativeCall(cmd, open, method, path(a), json.RawMessage(data))
	}}
	cmd.Flags().StringVar(&file, "file", "", "JSON request body file")
	return cmd
}
func creativeListCommand(open creativeConnection, resource string) *cobra.Command {
	var project string
	cmd := &cobra.Command{Use: "ls", Short: "List " + resource, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path := apiPath("v1", "creative", resource)
		if project != "" {
			path += "?" + url.Values{"project_id": []string{project}}.Encode()
		}
		return creativeCall(cmd, open, "GET", path, nil)
	}}
	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	return cmd
}
func creativeShowCommand(open creativeConnection, resource string) *cobra.Command {
	return &cobra.Command{Use: "show <id>", Short: "Read a creative definition and progress", Args: creativeArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return creativeCall(cmd, open, "GET", apiPath("v1", "creative", resource, a[0]), nil)
	}}
}
func creativeActionCommand(open creativeConnection, item bool) *cobra.Command {
	n, use := 2, "action <run> <stage> <start|submit|approve|reject|reopen>"
	if item {
		n, use = 3, "action <run> <stage> <item> <start|submit|approve|reject|reopen>"
	}
	var revision int64
	var versions []int64
	cmd := &cobra.Command{Use: use, Short: "Submit, approve, reject or explicitly reopen work", Args: func(cmd *cobra.Command, a []string) error {
		if len(a) != n+1 {
			return &usageError{message: "usage: " + cmd.UseLine()}
		}
		return creativeArgs(n)(cmd, a[:n])
	}, RunE: func(cmd *cobra.Command, a []string) error {
		action := a[n]
		switch action {
		case "start", "submit", "approve", "reject", "reopen":
		default:
			return &usageError{message: "unknown action: " + action}
		}
		if (action == "approve" || action == "reject") && revision <= 0 {
			return &usageError{message: "--review-revision from the displayed review is required"}
		}
		if action != "submit" && len(versions) > 0 {
			return &usageError{message: "--artifact-version is only valid for submit"}
		}
		for _, v := range versions {
			if v <= 0 {
				return &usageError{message: "artifact version IDs must be positive"}
			}
		}
		return creativeCall(cmd, open, "POST", creativeTargetPath(a[:n])+"/actions", store.CreativeAction{Action: action, ReviewRevision: revision, ArtifactVersionIDs: versions})
	}}
	cmd.Flags().Int64Var(&revision, "review-revision", 0, "Displayed revision required for approve/reject")
	cmd.Flags().Int64SliceVar(&versions, "artifact-version", nil, "Immutable version IDs to submit for review (repeat or comma separate)")
	return cmd
}
func creativeProgressCommand(open creativeConnection, item bool) *cobra.Command {
	n, use := 2, "progress <run> <stage>"
	if item {
		n, use = 3, "progress <run> <stage> <item>"
	}
	var progress int
	var note string
	cmd := &cobra.Command{Use: use, Short: "Update progress without completing or approving work", Args: creativeArgs(n), RunE: func(cmd *cobra.Command, a []string) error {
		if !cmd.Flags().Changed("progress") || progress < 0 || progress > 100 {
			return &usageError{message: "--progress must be 0..100"}
		}
		path := creativeTargetPath(a)
		if item {
			path += "/progress"
		}
		return creativeCall(cmd, open, "PATCH", path, store.CreativeProgress{Progress: progress, ProgressNote: note})
	}}
	cmd.Flags().IntVar(&progress, "progress", 0, "Progress from 0 to 100")
	cmd.Flags().StringVar(&note, "note", "", "Progress description")
	return cmd
}
func creativeContextCommand(open creativeConnection) *cobra.Command {
	var stage, item int64
	cmd := &cobra.Command{Use: "context <run>", Short: "Resolve artifact/discussion context", Args: creativeArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		if stage < 0 || item < 0 || (item > 0 && stage == 0) {
			return &usageError{message: "item context requires a positive stage ID"}
		}
		query := url.Values{}
		if stage > 0 {
			query.Set("stage_id", strconv.FormatInt(stage, 10))
		}
		if item > 0 {
			query.Set("item_id", strconv.FormatInt(item, 10))
		}
		path := creativeTargetPath(a) + "/context"
		if len(query) > 0 {
			path += "?" + query.Encode()
		}
		return creativeCall(cmd, open, "GET", path, nil)
	}}
	cmd.Flags().Int64Var(&stage, "stage", 0, "Stage ID")
	cmd.Flags().Int64Var(&item, "item", 0, "Item ID")
	return cmd
}
func creativeOrchestratorCommand(open creativeConnection) *cobra.Command {
	return &cobra.Command{Use: "orchestrator <run> <session>", Short: "Associate an existing same-project orchestrator session", Args: func(cmd *cobra.Command, a []string) error {
		if len(a) != 2 {
			return &usageError{message: "usage: " + cmd.UseLine()}
		}
		return creativeArgs(1)(cmd, a[:1])
	}, RunE: func(cmd *cobra.Command, a []string) error {
		return creativeCall(cmd, open, "PUT", creativeTargetPath(a[:1])+"/orchestrator", map[string]string{"session_id": a[1]})
	}}
}
