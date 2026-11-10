package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func creativeFeedCommand(open creativeConnection) *cobra.Command {
	var stage, item int64
	root := &cobra.Command{Use: "feed RUN", Short: "Read the human/orchestrator run or stage conversation, newest first", Long: "Read immutable feed messages and paid-call proposal cards. Use --stage for a stage feed and --item to filter its item tags. Reading does not mark the human's feed read.", Example: "  rocket creative agent feed 7\n  rocket creative agent feed 7 --stage 9 --item 12", Args: creativeArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFeedScope(stage, item, nil); err != nil {
			return err
		}
		q := url.Values{}
		if stage != 0 {
			q.Set("stage_id", strconv.FormatInt(stage, 10))
		}
		if item != 0 {
			q.Set("item_id", strconv.FormatInt(item, 10))
		}
		path := apiPath("v1", "creative", "runs", args[0], "feed")
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		return creativeCall(cmd, open, "GET", path, nil)
	}}
	root.Flags().Int64Var(&stage, "stage", 0, "Stage feed (omit for the run feed)")
	root.Flags().Int64Var(&item, "item", 0, "Filter to an item tag in the selected stage feed")
	for _, action := range []string{"post", "propose"} {
		var stage, item, replaces int64
		var versions []int64
		var body, file string
		short := "Write an immutable message to the human in the selected feed (orchestrator only)"
		if action == "propose" {
			short = "Publish a paid-call proposal card; wait for its human Go button"
		}
		cmd := &cobra.Command{Use: action + " RUN", Short: short, Long: short + ". Keep the output, tool/provider/model, inputs/upload services, complete call list/count, price/basis and exact versions in a proposal body. Changed scope requires a new card; --replaces invalidates a pending earlier card. Typed Go is not consent.", Example: fmt.Sprintf("  rocket creative agent feed %s 7 --stage 9 --item 12 --version 101 --file note.md", action), Args: creativeArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFeedScope(stage, item, versions); err != nil {
				return err
			}
			if replaces < 0 {
				return &usageError{message: "--replaces must be a positive card entry ID"}
			}
			if body != "" && file != "" {
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
			if strings.TrimSpace(text) == "" {
				return &usageError{message: "--body or --file with nonempty text is required"}
			}
			if versions == nil {
				versions = []int64{}
			}
			in := map[string]any{"stage_id": stage, "item_id": item, "body": text, "artifact_version_ids": versions}
			path := apiPath("v1", "creative", "runs", args[0], "feed")
			if action == "propose" {
				path += "/cards"
				in["replaces_entry_id"] = replaces
			}
			return creativeCall(cmd, open, "POST", path, in)
		}}
		cmd.Flags().Int64Var(&stage, "stage", 0, "Stage feed (omit for run feed)")
		cmd.Flags().Int64Var(&item, "item", 0, "Tag an item of the selected stage")
		cmd.Flags().Int64SliceVar(&versions, "version", nil, "Exact immutable artifact version IDs (repeatable)")
		cmd.Flags().StringVar(&body, "body", "", "Message or complete paid-call proposal")
		cmd.Flags().StringVar(&file, "file", "", "Read Markdown/text from file (- for stdin)")
		if action == "propose" {
			cmd.Flags().Int64Var(&replaces, "replaces", 0, "Pending card entry to replace in this run")
			cmd.Example += "\n  rocket creative agent feed propose 7 --stage 9 --replaces 42 --file revised-proposal.md"
		}
		root.AddCommand(cmd)
	}
	root.AddCommand(&cobra.Command{Use: "card RUN ENTRY", Short: "Read a proposal's current human decision and replacement links", Long: "Read pending/decided/replaced state and the immutable proposal. Execute only the exact call set of a card whose human decision is go; never execute a pending or replaced card.", Example: "  rocket creative agent feed card 7 42", Args: creativeArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return creativeCall(cmd, open, "GET", apiPath("v1", "creative", "runs", args[0], "feed", "cards", args[1]), nil)
	}})
	return root
}
func validateFeedScope(stage, item int64, versions []int64) error {
	if stage < 0 || item < 0 || (item != 0 && stage == 0) {
		return &usageError{message: "item requires a positive stage; IDs cannot be negative"}
	}
	for _, id := range versions {
		if id <= 0 {
			return &usageError{message: "version IDs must be positive"}
		}
	}
	return nil
}
