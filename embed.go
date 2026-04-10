package etcdmonitor

import "embed"

// WebFS 嵌入前端静态资源
//
//go:embed web/*
var WebFS embed.FS
