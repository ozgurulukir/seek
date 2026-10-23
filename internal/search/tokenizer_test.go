package search

import (
	"sync"
	"testing"
)

func BenchmarkAnalyze(b *testing.B) {
	a := NewAnalyzer("en", true, true)
	text := "the quick brown fox jumps over the lazy dog running studies happily"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.Analyze(text)
	}
}

func BenchmarkAnalyzeForQuery(b *testing.B) {
	a := NewAnalyzer("en", true, true)
	text := "the quick brown fox jumps over the lazy dog running studies happily"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.AnalyzeForQuery(text)
	}
}

func TestAnalyzerEnglishStemming(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	tests := []struct {
		input    string
		expected []string
	}{
		{"running", []string{"run"}},
		{"runs", []string{"run"}},
		{"ran", []string{"ran"}},
		{"the quick brown fox", []string{"quick", "brown", "fox"}},
		{"running jumps", []string{"run", "jump"}},
		{"studies", []string{"studi"}},
		{"jumping", []string{"jump"}},
		{"lazy", []string{"lazi"}},
	}

	for _, tt := range tests {
		got := a.Analyze(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("Analyze(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("Analyze(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestAnalyzerTurkishStemming(t *testing.T) {
	a := NewAnalyzer("tr", true, true)
	tests := []struct {
		input    string
		expected []string
	}{
		{"kitaplar", []string{"kitap"}},
		{"evlerde", []string{"ev"}},
		{"arabam", []string{"araba"}},
		{"notlarım", []string{"not"}},
		// Turkish verb forms are not stemmed by Snowball TR
		{"koşuyor", []string{"koşuyor"}},
		{"koşmak", []string{"koşmak"}},
	}

	for _, tt := range tests {
		got := a.Analyze(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("Analyze(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("Analyze(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestAnalyzerTurkishDoesNotCorruptASCII(t *testing.T) {
	// The Turkish snowball stemmer must not mangle non-Turkish ASCII tokens
	// (e.g. "config" -> "configi", "launchd" -> "launchdu"). Such bogus stems
	// produce FTS5 prefixes that never match the original word, silently
	// zeroing results. The guard must keep these tokens intact.
	a := NewAnalyzer("tr", true, true)
	tests := []struct {
		input   string
		stemmed string // Analyze output (single token)
		query   string // AnalyzeForQuery output (prefix-expanded)
	}{
		{"launchd", "launchd", "launchd"},
		{"read", "read", "read"},
		{"config", "config", "config"},
		{"build", "build", "build"},
		{"service", "service", "service"},
		{"sqlite", "sqli", "sqli*"}, // harmless: sqli is a prefix of sqlite
		{"token", "toke", "toke*"},  // harmless: toke is a prefix of token
	}

	for _, tt := range tests {
		got := a.Analyze(tt.input)
		if len(got) != 1 || got[0] != tt.stemmed {
			t.Errorf("Analyze(%q) = %v, want [%q]", tt.input, got, tt.stemmed)
		}
		gotQ := a.AnalyzeForQuery(tt.input)
		if len(gotQ) != 1 || gotQ[0] != tt.query {
			t.Errorf("AnalyzeForQuery(%q) = %v, want [%q]", tt.input, gotQ, tt.query)
		}
	}
}

func TestAnalyzerTurkishVoicingPreserved(t *testing.T) {
	// Genuine Turkish root reconstruction (p->b voicing: kitabı -> kitap) must
	// still stem, even though the stem is not a prefix of the token.
	a := NewAnalyzer("tr", true, true)
	tests := []struct {
		input    string
		expected string
	}{
		{"kitabı", "kitap"},
		{"kitapta", "kitap"},
		{"faturayı", "fatura"},
	}

	for _, tt := range tests {
		got := a.Analyze(tt.input)
		if len(got) != 1 || got[0] != tt.expected {
			t.Errorf("Analyze(%q) = %v, want [%q]", tt.input, got, tt.expected)
		}
	}
}

func TestAnalyzerStopWords(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	got := a.Analyze("the quick brown fox jumps over the lazy dog")
	// Stop words removed: "the", "over"
	// Stemmed: "jumps" -> "jump", "lazy" -> "lazi"
	want := []string{"quick", "brown", "fox", "jump", "lazi", "dog"}
	if len(got) != len(want) {
		t.Errorf("Analyze(stop words) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Analyze(stop words)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAnalyzerNoStemmer(t *testing.T) {
	a := NewAnalyzer("en", true, false)
	got := a.Analyze("running")
	if len(got) != 1 || got[0] != "running" {
		t.Errorf("Analyze(running) without stemmer = %v, want [running]", got)
	}
}

func TestAnalyzerForQuery(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	got := a.AnalyzeForQuery("running")
	// Should return stemmed term with * prefix for FTS5 expansion
	if len(got) != 1 {
		t.Fatalf("AnalyzeForQuery(running) len = %d, want 1", len(got))
	}
	if got[0] != "run*" {
		t.Errorf("AnalyzeForQuery(running) = %q, want run*", got[0])
	}
}

// TestAnalyzerEnglishNonPrefixStemFallsBackToSurface pins issue #98: the FTS5
// index is unstemmed, so a porter rewrite that is not a prefix of the surface
// token (body->bodi, city->citi, study->studi) must not become a `bodi*`
// prefix query that can never match. Such terms fall back to the surface form;
// genuine prefix stems still expand.
func TestAnalyzerEnglishNonPrefixStemFallsBackToSurface(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	tests := []struct {
		input string
		want  string
	}{
		{"body", "body"},   // stem "bodi" is not a prefix
		{"city", "city"},   // stem "citi" is not a prefix
		{"study", "study"}, // stem "studi" is not a prefix
		{"happily", "happily"},
		{"studies", "studi*"}, // prefix stem still expands
		{"cities", "citi*"},
		{"running", "run*"},
	}
	for _, tt := range tests {
		got := a.AnalyzeForQuery(tt.input)
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("AnalyzeForQuery(%q) = %v, want [%q]", tt.input, got, tt.want)
		}
	}
}

func TestAnalyzerIsStopWord(t *testing.T) {
	tests := []struct {
		name     string
		analyzer *Analyzer
		word     string
		want     bool
	}{
		{
			name:     "english stop word lowercase",
			analyzer: NewAnalyzer("en", true, true),
			word:     "the",
			want:     true,
		},
		{
			name:     "english stop word uppercase case folding",
			analyzer: NewAnalyzer("en", true, true),
			word:     "THE",
			want:     true,
		},
		{
			name:     "english stop word mixed case",
			analyzer: NewAnalyzer("en", true, true),
			word:     "ThE",
			want:     true,
		},
		{
			name:     "english non-stop word",
			analyzer: NewAnalyzer("en", true, true),
			word:     "running",
			want:     false,
		},
		{
			name:     "english stop words disabled",
			analyzer: NewAnalyzer("en", false, true),
			word:     "the",
			want:     false,
		},
		{
			name:     "turkish stop word lowercase",
			analyzer: NewAnalyzer("tr", true, true),
			word:     "ve",
			want:     true,
		},
		{
			name:     "turkish stop word uppercase",
			analyzer: NewAnalyzer("tr", true, true),
			word:     "VE",
			want:     true,
		},
		{
			name:     "turkish non-stop word",
			analyzer: NewAnalyzer("tr", true, true),
			word:     "kitap",
			want:     false,
		},
		{
			name:     "unsupported language with stop words enabled (nil stopWords map)",
			analyzer: NewAnalyzer("fr", true, true),
			word:     "the",
			want:     false,
		},
		{
			name:     "analyzer with nil stopWords map explicitly",
			analyzer: &Analyzer{enableStopWords: true, stopWords: nil},
			word:     "the",
			want:     false,
		},
		{
			name: "custom stopWords map case folding",
			analyzer: &Analyzer{
				enableStopWords: true,
				stopWords:       map[string]bool{"custom": true, "foo": true},
			},
			word: "Foo",
			want: true,
		},
		{
			name: "custom stopWords map non-matching word",
			analyzer: &Analyzer{
				enableStopWords: true,
				stopWords:       map[string]bool{"custom": true},
			},
			word: "bar",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.analyzer.IsStopWord(tt.word)
			if got != tt.want {
				t.Errorf("IsStopWord(%q) = %v, want %v", tt.word, got, tt.want)
			}
		})
	}
}

func TestAnalyzerUnsupportedLanguage(t *testing.T) {
	a := NewAnalyzer("fr", true, true)
	// French is not supported, should fall back to no-op stemming
	got := a.Analyze("chien")
	if len(got) != 1 || got[0] != "chien" {
		t.Errorf("Analyze(chien) with unsupported lang = %v, want [chien]", got)
	}
}

func TestAnalyzerEmptyInput(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	got := a.Analyze("")
	if len(got) != 0 {
		t.Errorf("Analyze('') = %v, want []", got)
	}
}

func TestAnalyzerConcurrentStemming(t *testing.T) {
	enAnalyzer := NewAnalyzer("en", true, true)
	trAnalyzer := NewAnalyzer("tr", true, true)

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Concurrent English stemming
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				res := enAnalyzer.Analyze("running jumps quickly happily studies")
				want := []string{"run", "jump", "quick", "happili", "studi"}
				if len(res) != len(want) {
					t.Errorf("enAnalyzer len = %d, want %d", len(res), len(want))
					return
				}
				for j := range res {
					if res[j] != want[j] {
						t.Errorf("enAnalyzer[%d] = %q, want %q", j, res[j], want[j])
						return
					}
				}
			}
		}()
	}

	// Concurrent Turkish stemming
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				res := trAnalyzer.Analyze("kitaplar evlerde arabam")
				want := []string{"kitap", "ev", "araba"}
				if len(res) != len(want) {
					t.Errorf("trAnalyzer len = %d, want %d", len(res), len(want))
					return
				}
				for j := range res {
					if res[j] != want[j] {
						t.Errorf("trAnalyzer[%d] = %q, want %q", j, res[j], want[j])
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}
