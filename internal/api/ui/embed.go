// Package ui embeds the built dashboard (dashboard/dist, copied here by
// `make dashboard`) so the collector ships as a single binary.
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
