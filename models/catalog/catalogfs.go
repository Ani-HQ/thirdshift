// Package catalogfs embeds the curated model catalog so released binaries
// work without a repo checkout. Disk files take precedence when present, so
// operators can still override or extend the catalog locally.
package catalogfs

import "embed"

//go:embed *.yaml *.json
var FS embed.FS
