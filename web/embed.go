package web

import "embed"

//go:embed templates/*.html static/* static/images/* default.json
var Files embed.FS
