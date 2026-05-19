package server

import "embed"

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed assets/styles.css
var assetsFS embed.FS
