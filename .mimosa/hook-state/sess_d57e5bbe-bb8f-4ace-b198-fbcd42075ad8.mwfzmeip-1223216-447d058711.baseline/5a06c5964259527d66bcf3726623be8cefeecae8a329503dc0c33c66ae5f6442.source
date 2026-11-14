package agenthooks

import (
	"testing"
)

func BenchmarkIsTargetSeekHookCommand(b *testing.B) {
	cmd := `"/usr/local/bin/seek" hooks sync --agent claude --embed`
	target := Target{
		agent: "claude",
		event: "Stop",
		embed: true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !isTargetSeekHookCommand(cmd, target) {
			b.Fatal("expected match")
		}
	}
}
