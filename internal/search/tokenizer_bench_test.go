package search

import "testing"

func BenchmarkAnalyzeToken(b *testing.B) {
	text := "The quick brown fox jumps over the lazy dog! Hybrid search engines (BM25 + vector semantic) store data in SQLite."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AnalyzeToken(text)
	}
}

func BenchmarkAnalyzerAnalyze(b *testing.B) {
	a := NewAnalyzer("en", true, true)
	text := "The quick brown fox jumps over the lazy dog! Hybrid search engines (BM25 + vector semantic) store data in SQLite."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.Analyze(text)
	}
}

func BenchmarkLowerToken(b *testing.B) {
	tokens := []string{"the", "quick", "BROWN", "Fox", "jumps", "over", "the", "lazy", "dog", "sqlite", "Hybrid", "search"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tok := range tokens {
			_ = LowerToken(tok)
		}
	}
}
