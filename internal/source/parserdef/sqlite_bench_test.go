package parserdef

import (
	"testing"
)

func BenchmarkBuildBatchQuery(b *testing.B) {
	query := `
		SELECT id, url, title, text, date
		FROM document
		WHERE session_id = :session_id
		UNION ALL
		SELECT id, url, title, text, date
		FROM history
		WHERE session_id = :session_id
	`
	for i := 0; i < b.N; i++ {
		_, _, _ = buildBatchQuery(query)
	}
}
