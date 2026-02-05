// internal/embedder/embed.go
package embedder

import "embed"

//go:embed models/*
//go:embed models/1_Pooling/*
var modelFS embed.FS
