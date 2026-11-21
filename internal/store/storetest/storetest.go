// Package storetest holds test helpers shared by every package's tests that
// open a real store. It is deliberately a leaf package (no store import) so
// any package's tests — including internal/store's own — can use it without
// an import cycle.
package storetest

import (
	"strings"
	"testing"
)

// fts5DisabledMarker is the error-text prefix produced by internal/store's
// FTS5 capability probe (store.go). Duplicated here rather than imported so
// the helper stays a leaf package.
const fts5DisabledMarker = "SQLite FTS5 not enabled"

// SkipIfFTS5Disabled skips the test when err is the store.Open
// FTS5-disabled capability error (tests built without the fts5/sqlite_fts5
// tags). Behavior-preserving for every call site: non-FTS5 errors are left
// untouched for the caller's own handling. The skip reason names the
// required tags so a skipped run is self-explanatory.
func SkipIfFTS5Disabled(t testing.TB, err error) {
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), fts5DisabledMarker) {
		t.Skip(`SQLite FTS5 not enabled. Run tests with: go test -tags "fts5 sqlite_fts5" ./... or make test`)
	}
}
