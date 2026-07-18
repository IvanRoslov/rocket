// Package web embeds the production dashboard build served by rocketd.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
