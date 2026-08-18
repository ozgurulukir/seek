package parserdef

import (
	"testing"
)

func BenchmarkBuildBatchQuery(b *testing.B) {
	query := `
		SELECT id, url, title, visit_time
		FROM visits
		WHERE visit_time > 1000 AND session_id = :session_id
		UNION ALL
		SELECT id, url, title, visit_time
		FROM archived_visits
		WHERE session_id = :session_id
	`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = buildBatchQuery(query)
	}
}
