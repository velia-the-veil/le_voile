package frontend

import "embed"

//go:embed all:index.html all:src all:assets
var Assets embed.FS
