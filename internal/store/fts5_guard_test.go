//go:build !fts5

package store_test

import "testing"

func TestFTS5BuildTagNotice(t *testing.T) {
	t.Log("NOTICE: Tests are running without the 'fts5 sqlite_fts5' build tags.")
	t.Log("SQLite FTS5 requires: go test -tags \"fts5 sqlite_fts5\" ./... or make test")
}
