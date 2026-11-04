package cli

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

type creativeArtifactPublishOptions struct {
	StageID, ItemID        int64
	Name, Kind, File, MIME string
}

func creativeArtifactCLIPositiveID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, &usageError{message: "positive integer ID required"}
	}
	return id, nil
}
func creativeArtifactCLIContext(run string, stage, item int64) (string, error) {
	if _, err := creativeArtifactCLIPositiveID(run); err != nil {
		return "", err
	}
	if stage < 0 || item < 0 || (item != 0 && stage == 0) {
		return "", &usageError{message: "item requires a stage; context IDs must be positive"}
	}
	q := url.Values{}
	if stage != 0 {
		q.Set("stage_id", strconv.FormatInt(stage, 10))
	}
	if item != 0 {
		q.Set("item_id", strconv.FormatInt(item, 10))
	}
	return apiPath("v1", "creative", "runs", run, "artifacts") + "?" + q.Encode(), nil
}
func (o creativeArtifactPublishOptions) prepare(run string) (string, *os.File, string, error) {
	path, err := creativeArtifactCLIContext(run, o.StageID, o.ItemID)
	if err != nil {
		return "", nil, "", err
	}
	if strings.TrimSpace(o.Name) == "" || len(strings.TrimSpace(o.Name)) > 512 || (o.Kind != "source" && o.Kind != "result") || o.File == "" {
		return "", nil, "", &usageError{message: "publish requires --name, --file and --kind source|result"}
	}
	f, err := os.Open(o.File)
	if err != nil {
		return "", nil, "", fmt.Errorf("open artifact file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return "", nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 2<<30 {
		f.Close()
		return "", nil, "", fmt.Errorf("artifact must be a nonempty regular file of at most 2 GiB")
	}
	typ := o.MIME
	if typ == "" {
		typ = mime.TypeByExtension(strings.ToLower(filepath.Ext(o.File)))
	}
	if typ == "" {
		typ = "application/octet-stream"
	}
	typ, _, err = mime.ParseMediaType(typ)
	if err != nil || !strings.Contains(typ, "/") || strings.Contains(typ, "*") {
		f.Close()
		return "", nil, "", &usageError{message: "valid --mime type required"}
	}
	u, _ := url.Parse(path)
	q := u.Query()
	q.Set("name", strings.TrimSpace(o.Name))
	q.Set("kind", o.Kind)
	u.RawQuery = q.Encode()
	return u.String(), f, typ, nil
}

func newCreativeArtifactCmd() *cobra.Command {
	return creativeArtifactCommands(func() (*client.Client, error) { c, _, err := connect(true); return c, err })
}
func creativeArtifactCommands(open creativeConnection) *cobra.Command {
	cmd := &cobra.Command{Use: "artifact", Short: "Publish and inspect durable creative sources and results"}
	var o creativeArtifactPublishOptions
	publish := &cobra.Command{Use: "publish RUN", Short: "Stream a source/result file (up to 2 GiB) into a named retained version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, f, typ, err := o.prepare(args[0])
		if err != nil {
			return err
		}
		defer f.Close()
		c, err := open()
		if err != nil {
			return err
		}
		var out json.RawMessage
		if err := c.UploadCreativeArtifact(cmd.Context(), path, filepath.Base(o.File), typ, f, &out); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
	}}
	publish.Flags().Int64Var(&o.StageID, "stage", 0, "Stage ID (optional)")
	publish.Flags().Int64Var(&o.ItemID, "item", 0, "Repeated item ID (requires --stage)")
	publish.Flags().StringVar(&o.Name, "name", "", "Named source/result")
	publish.Flags().StringVar(&o.Kind, "kind", "result", "source or result")
	publish.Flags().StringVar(&o.File, "file", "", "Local regular file to upload")
	publish.Flags().StringVar(&o.MIME, "mime", "", "Content type (defaults from file extension)")
	cmd.AddCommand(publish)
	var stage, item int64
	ls := &cobra.Command{Use: "ls RUN", Short: "List named materials in an exact run/stage/item context", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := creativeArtifactCLIContext(args[0], stage, item)
		if err != nil {
			return err
		}
		c, err := open()
		if err != nil {
			return err
		}
		var out json.RawMessage
		if err := c.Get(path, nil, &out); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
	}}
	ls.Flags().Int64Var(&stage, "stage", 0, "Stage ID")
	ls.Flags().Int64Var(&item, "item", 0, "Repeated item ID")
	cmd.AddCommand(ls)
	show := &cobra.Command{Use: "show ID", Aliases: []string{"versions"}, Short: "Show artifact and all retained versions/content URLs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := creativeArtifactCLIPositiveID(args[0]); err != nil {
			return err
		}
		c, err := open()
		if err != nil {
			return err
		}
		var out json.RawMessage
		if err := c.Get(apiPath("v1", "creative", "artifacts", args[0]), nil, &out); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
	}}
	cmd.AddCommand(show)
	current := &cobra.Command{Use: "current ID VERSION", Short: "Select a retained version as current", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := creativeArtifactCLIPositiveID(args[0]); err != nil {
			return err
		}
		version, err := creativeArtifactCLIPositiveID(args[1])
		if err != nil {
			return err
		}
		c, err := open()
		if err != nil {
			return err
		}
		var out json.RawMessage
		if err := c.Put(apiPath("v1", "creative", "artifacts", args[0], "current"), map[string]int64{"version_id": version}, &out); err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
	}}
	cmd.AddCommand(current)
	return cmd
}
