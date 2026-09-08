package search

import (
	"testing"
)

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

func TestAnalyzerIsStopWord(t *testing.T) {
	a := NewAnalyzer("en", true, true)
	if !a.IsStopWord("the") {
		t.Error("IsStopWord(the) = false, want true")
	}
	if a.IsStopWord("running") {
		t.Error("IsStopWord(running) = true, want false")
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
