package store

import "testing"

func BenchmarkSplitMembershipTokens(b *testing.B) {
	input := "go, rust, python, javascript, typescript, react, nodejs, docker, kubernetes, aws"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = splitMembershipTokens(input)
	}
}

func BenchmarkDecodeFastFieldText(b *testing.B) {
	encoded := `"go,rust,org:OpenAI"`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = decodeFastFieldText(encoded)
	}
}
