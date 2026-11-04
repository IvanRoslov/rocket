// Package creativeweb embeds the separate creative dashboard.
package creativeweb

import "embed"

//go:embed all:dist
var Dist embed.FS
