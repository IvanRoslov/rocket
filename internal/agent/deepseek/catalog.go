package deepseek

import (
	"context"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func (d *DeepSeek) Catalog(context.Context) (agent.Catalog, error) {
	return agent.Catalog{
		Source: agent.CatalogSourceBuiltin,
		Models: []agent.CatalogModel{
			{
				ID: "deepseek-flash[1m]", Name: "DeepSeek Flash 1M", Main: true,
				Efforts: d.Efforts(), DefaultEffort: "max",
			},
			{
				ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Main: true,
				Efforts: d.Efforts(), DefaultEffort: "max",
			},
		},
	}, nil
}
