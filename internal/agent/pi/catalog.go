package pi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var catalogTimeout = 10 * time.Second

func listModelsCmd(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "pi", "--list-models")
	cmd.WaitDelay = time.Second
	return cmd
}

var runListModels = func(ctx context.Context) ([]byte, error) {
	return listModelsCmd(ctx).Output()
}

func parseListModels(data []byte) ([]agent.CatalogModel, error) {
	header := false
	var models []agent.CatalogModel
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !header {
			if len(fields) < 2 || fields[0] != "provider" || fields[1] != "model" {
				return nil, errors.New("missing provider/model header")
			}
			header = true
			continue
		}
		if len(fields) != 6 {
			continue
		}
		models = append(models, agent.CatalogModel{
			ID: fields[0] + "/" + fields[1], Name: fields[1], Main: true, Efforts: (&Pi{}).Efforts(),
		})
	}
	if len(models) == 0 {
		return nil, errors.New("no models listed")
	}
	return models, nil
}

func (p *Pi) Catalog(ctx context.Context) (agent.Catalog, error) {
	cliCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
	out, err := runListModels(cliCtx)
	if err == nil && cliCtx.Err() != nil {
		err = cliCtx.Err()
	}
	cancel()
	if err == nil {
		var models []agent.CatalogModel
		models, err = parseListModels(out)
		if err == nil {
			return agent.Catalog{Source: agent.CatalogSourceCLI, FetchedAt: time.Now(), Models: models}, nil
		}
	}
	warning := fmt.Sprintf("pi --list-models: %v", err)
	log.Printf("pi catalog: %s; using the builtin list", warning)
	return agent.Catalog{Source: agent.CatalogSourceBuiltin, Warning: warning, Models: builtinCatalog()}, nil
}

func builtinCatalog() []agent.CatalogModel {
	return []agent.CatalogModel{
		{ID: "deepseek/deepseek-flash", Name: "DeepSeek Flash", Main: true, Efforts: (&Pi{}).Efforts()},
		{ID: "deepseek/deepseek-v4-pro", Name: "DeepSeek V4 Pro", Main: true, Efforts: (&Pi{}).Efforts()},
	}
}
