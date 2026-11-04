package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/IvanRoslov/rocket/internal/preview"
)

func initializePreviews(d *Deps) {
	if d.Previews != nil {
		return
	}
	cfg := preview.Config{}
	if d.Cfg != nil {
		cfg.AllowedPorts = d.Cfg.PreviewPorts
		cfg.ExcludedPorts = []int{d.Cfg.Port, d.Cfg.TLSPort, d.Cfg.PreviewPort}
		if d.Cfg.PreviewPort > 0 {
			cfg.PublicURL = d.Cfg.PreviewPublicURL
		}
	}
	d.Previews = preview.New(cfg)
}

// listenPreview is deliberately independent of the privileged Rocket listener.
// HTTPS/public forwarding belongs to the installation's private reverse proxy.
func listenPreview(d Deps) (*http.Server, net.Listener) {
	if d.Cfg.PreviewPort == 0 {
		return nil, nil
	}
	host := d.Cfg.PreviewHost
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(d.Cfg.PreviewPort)))
	if err != nil {
		d.Previews.SetUnavailable("Preview listener could not start; check preview_host and preview_port, then restart Rocket when convenient.")
		slog.Error("preview listener disabled", "error", err)
		return nil, nil
	}
	return &http.Server{Handler: d.Previews, ReadHeaderTimeout: 5 * time.Second}, ln
}
