package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

func newCreativeAgentCmd() *cobra.Command {
	return creativeAgentCommands(func() (*client.Client, error) { c, _, err := connect(false); return c, err })
}
func creativeAgentCommands(open creativeConnection) *cobra.Command {
	root := &cobra.Command{Use: "agent", Short: "Creative sessions and contextual discussion"}
	root.AddCommand(creativeCloseCommand(open))
	root.AddCommand(creativeFeedCommand(open))
	for _, action := range []string{"launch", "spawn", "sessions", "send", "messages", "ask", "questions", "reply", "answer"} {
		var stage, item int64
		var versions []int64
		var body, file, profile, agentName, repo, name, to, title, allow string
		var options []string
		var choose int
		var dispute, wait bool
		cmd := &cobra.Command{Use: action + " ID", Args: creativeArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if action == "ask" {
				workerCommand := fmt.Sprintf("rocket creative agent send %s", args[0])
				if stage > 0 {
					workerCommand += fmt.Sprintf(" --stage %d", stage)
				} else {
					workerCommand += " --stage STAGE"
				}
				if item > 0 {
					workerCommand += fmt.Sprintf(" --item %d", item)
				}
				return fmt.Errorf("new Creative questions are retired; use rocket creative agent feed post %s --body 'Your message'; paid proposals: rocket creative agent feed propose %s --body 'Exact calls and cost'; workers: %s --body 'Your question' (include your assigned --item when applicable)", args[0], args[0], workerCommand)
			}

			if stage < 0 || item < 0 || item != 0 && stage == 0 {
				return &usageError{message: "item requires a positive stage; IDs cannot be negative"}
			}
			for _, id := range versions {
				if id <= 0 {
					return &usageError{message: "version IDs must be positive"}
				}
			}
			if file != "" && body != "" {
				return &usageError{message: "use --body or --file, not both"}
			}
			text := body
			if file != "" {
				var err error
				text, err = readTextInput(cmd, file)
				if err != nil {
					return err
				}
			}
			path := apiPath("v1", "creative", "runs", args[0])
			method := "POST"
			in := map[string]any{}
			switch action {
			case "launch", "spawn":
				in = map[string]any{"prompt": text, "profile": profile, "agent": agentName}
				if action == "launch" {
					path += "/sessions"
					if cmd.Flags().Changed("allow") {
						in["allowed_profiles"] = parseProfileList(allow)
					}
				} else {
					if stage == 0 || name == "" || text == "" {
						return &usageError{message: "spawn requires --stage, --name and --body or --file"}
					}
					path += "/workers"
					in["stage_id"] = stage
					in["item_id"] = item
					in["name"] = name
					in["repo"] = repo
				}
			case "sessions", "messages", "questions":
				method = "GET"
				path += "/" + action
				q := url.Values{}
				if stage != 0 {
					q.Set("stage_id", strconv.FormatInt(stage, 10))
				}
				if item != 0 {
					q.Set("item_id", strconv.FormatInt(item, 10))
				}
				if len(q) > 0 {
					path += "?" + q.Encode()
				}
			case "send", "ask":
				if text == "" {
					return &usageError{message: "--body or --file is required"}
				}
				path += "/messages"
				if action == "ask" {
					path = apiPath("v1", "creative", "runs", args[0], "questions")
				}
				in = map[string]any{"stage_id": stage, "item_id": item, "artifact_version_ids": versions, "body": text}
				if action == "send" {
					in["to"] = to
				} else {
					in["title"] = title
					in["options"] = options
				}
			case "reply", "answer":
				if text == "" && choose == 0 {
					return &usageError{message: "--body, --file or --choose is required"}
				}
				path = apiPath("v1", "questions", args[0], action)
				in = map[string]any{"body": text}
				if action == "answer" {
					in["choose"] = choose
				} else {
					in["dispute"] = dispute
				}
			default:
				return fmt.Errorf("unsupported action")
			}
			if action == "send" {
				return creativeSend(cmd, open, path, in, wait)
			}
			return creativeCall(cmd, open, method, path, in)
		}}
		switch action {
		case "launch", "spawn", "send", "ask", "reply", "answer":
			cmd.Flags().StringVar(&body, "body", "", "Message or assignment text")
			cmd.Flags().StringVar(&file, "file", "", "Read message or assignment from file (- for stdin)")
		}
		switch action {
		case "spawn", "send", "ask", "messages", "questions":
			cmd.Flags().Int64Var(&stage, "stage", 0, "Stage context ID")
			cmd.Flags().Int64Var(&item, "item", 0, "Item context ID")
		}
		if action == "send" || action == "ask" {
			cmd.Flags().Int64SliceVar(&versions, "version", nil, "Exact shown immutable version IDs (repeatable)")
		}
		if action == "send" {
			cmd.Flags().StringVar(&to, "to", "", "Recipient session (default run orchestrator; orchestrators must specify a worker)")
			cmd.Flags().BoolVar(&wait, "wait", false, "Poll normal message delivery until confirmed or failed (accepted is not confirmed)")
		}
		if action == "ask" {
			cmd.Flags().StringVar(&title, "title", "", "Question title")
			cmd.Flags().StringArrayVar(&options, "option", nil, "Answer choice (repeatable)")
		}
		if action == "launch" || action == "spawn" {
			cmd.Flags().StringVar(&profile, "profile", "", "Existing model profile")
			cmd.Flags().StringVar(&agentName, "agent", "", "Agent runtime")
		}
		if action == "launch" {
			cmd.Flags().StringVar(&allow, "allow", "", "Set run worker profiles before launch (comma separated; empty clears)")
		}
		if action == "spawn" {
			cmd.Flags().StringVar(&repo, "repo", "", "Project main/linked repository")
			cmd.Flags().StringVar(&name, "name", "", "Worker name slug")
		}
		if action == "reply" {
			cmd.Flags().BoolVar(&dispute, "dispute", false, "Explicitly reopen a resolved decision")
		}
		if action == "answer" {
			cmd.Flags().IntVar(&choose, "choose", 0, "Human selects 1-based option")
		}
		root.AddCommand(cmd)
	}
	return root
}

// creativeSend retains the contextual POST response and uses the ordinary send
// waiter for delivery, rather than implementing a Creative delivery mechanism.
func creativeSend(cmd *cobra.Command, open creativeConnection, path string, in any, wait bool) error {
	c, err := open()
	if err != nil {
		return err
	}
	var out json.RawMessage
	if err := c.Post(path, in, &out); err != nil {
		return err
	}
	if !wait {
		return printJSON(cmd, out)
	}
	var response struct {
		ID     int64  `json:"id"`
		To     string `json:"to"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return err
	}
	if !flags.JSON {
		cmd.Printf("message %d to %s %s\n", response.ID, response.To, response.Status)
	}
	return waitForMessageTo(cmd, c, response.ID, response.To)
}
