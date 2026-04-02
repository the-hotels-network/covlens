package report

import "embed"

//go:embed templates/* static/*
var content embed.FS
