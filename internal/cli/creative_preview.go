package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/preview"
	"github.com/spf13/cobra"
)

type creativePreviewClient interface {
	Get(string, any, any) error
	Post(string, any, any) error
	Delete(string, any, any) error
}

func dialCreativePreview() (creativePreviewClient, error) {
	c, _, err := connect(false)
	return c, err
}
func newCreativePreviewCmd(dial func() (creativePreviewClient, error)) *cobra.Command {
	root := &cobra.Command{Use: "creative-preview", Short: "Register and access isolated creative landing previews"}
	setup := &cobra.Command{Use: "setup", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := dial()
		if err != nil {
			return err
		}
		var out preview.Setup
		if err = c.Get("/v1/creative/previews/setup", nil, &out); err != nil {
			return err
		}
		return printJSON(cmd, out)
	}}
	root.AddCommand(setup)
	for _, action := range []string{"register", "list"} {
		var run, stage, item int64
		var port int
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs}
		cmd.Flags().Int64Var(&run, "run", 0, "creative run ID (required)")
		cmd.Flags().Int64Var(&stage, "stage", 0, "creative stage ID")
		cmd.Flags().Int64Var(&item, "item", 0, "creative item ID")
		if action == "register" {
			cmd.Flags().IntVar(&port, "port", 0, "allowed loopback dev-server port (required)")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if run <= 0 || stage < 0 || item < 0 || (action == "register" && (port < 1024 || port > 65535)) {
				return &usageError{message: "positive --run and valid --port (register) required; context IDs cannot be negative"}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			if action == "register" {
				in := struct {
					preview.Context
					Port int `json:"port"`
				}{preview.Context{RunID: run, StageID: stage, ItemID: item}, port}
				var out preview.Access
				if err = c.Post("/v1/creative/previews", in, &out); err != nil {
					return err
				}
				return printJSON(cmd, out)
			}
			q := url.Values{"run_id": {strconv.FormatInt(run, 10)}}
			if stage > 0 {
				q.Set("stage_id", strconv.FormatInt(stage, 10))
			}
			if item > 0 {
				q.Set("item_id", strconv.FormatInt(item, 10))
			}
			var out []preview.Access
			if err = c.Get("/v1/creative/previews?"+q.Encode(), nil, &out); err != nil {
				return err
			}
			return printJSON(cmd, out)
		}
		root.AddCommand(cmd)
	}
	root.AddCommand(&cobra.Command{Use: "revoke <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := dial()
		if err != nil {
			return err
		}
		if err = c.Delete(apiPath("v1", "creative", "previews", args[0]), nil, nil); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Preview revoked")
		return nil
	}})
	return root
}
