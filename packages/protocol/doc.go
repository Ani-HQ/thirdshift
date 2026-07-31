package protocol

import "embed"

//go:embed schemas/*.schema.json
var SchemaFS embed.FS
