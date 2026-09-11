// Package agenthooks owns everything seek knows about AI-tool hook
// integrations: the shared Claude-Code-style JSON settings schema, the hook
// target registry (Claude Code and Codex today), the settings-file surgery
// (backup-once-per-run, permission preservation, surgical seek-only edits),
// the writer lock and sync state shared with cmd/sync, and the two
// subprocess entry points the installed hooks execute (`seek hooks sync` and
// `seek hooks context`).
//
// COMPATIBILITY SURFACE: the command strings install writes into user
// settings files (hookCommand output) and the matchers that recognize them
// (directSeekHookCommandPattern, isSeekHookCommand, isLegacyCodexWrapper)
// must never change output format. Existing installations are matched by
// those exact bytes — a change orphans old hooks on upgrade or, worse,
// makes uninstall delete other tools' entries. The golden tests pin them.
package agenthooks
