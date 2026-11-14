// Package buildinfo holds build-time identity injected via
// -ldflags -X at release time. It lives in internal so both package main
// (kong --version) and cmd (MCP server implementation version) can read it —
// cmd cannot import package main.
package buildinfo

// Version is overridden at release build time via
// -ldflags "-X github.com/ozgurulukir/seek/internal/buildinfo.Version=...".
// The default is what a plain `go build` reports.
var Version = "dev"

// Commit is overridden at release build time alongside Version.
var Commit = "none"
