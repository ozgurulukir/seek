package search

import (
	"strings"

	"github.com/blevesearch/snowballstem"
	"github.com/blevesearch/snowballstem/english"
	"github.com/blevesearch/snowballstem/turkish"
)

// Analyzer performs text analysis: tokenization, lowercasing, stop-word removal, and stemming.
type Analyzer struct {
	lang            string
	enableStopWords bool
	enableStemmer   bool
	stopWords       map[string]bool
	stemmer         func(string) string
}

// englishStopWords is a minimal set of common English stop words.
var englishStopWords = map[string]bool{
	"the": true, "and": true, "or": true, "but": true, "a": true, "an": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"being": true, "have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "must": true, "shall": true, "can": true, "to": true, "of": true,
	"in": true, "for": true, "on": true, "with": true, "at": true, "by": true,
	"from": true, "up": true, "about": true, "into": true, "through": true,
	"during": true, "before": true, "after": true, "above": true, "below": true,
	"between": true, "out": true, "off": true, "over": true, "under": true,
	"again": true, "further": true, "then": true, "once": true, "here": true,
	"there": true, "when": true, "where": true, "why": true, "how": true,
	"all": true, "each": true, "every": true, "both": true, "few": true,
	"more": true, "most": true, "other": true, "some": true, "such": true,
	"no": true, "nor": true, "not": true, "only": true, "own": true,
	"same": true, "so": true, "than": true, "too": true, "very": true,
	"s": true, "t": true, "just": true, "don": true, "now": true,
}

// turkishStopWords is a minimal set of common Turkish stop words.
var turkishStopWords = map[string]bool{
	"ve": true, "veya": true, "ama": true, "fakat": true, "ancak": true,
	"çünkü": true, "zira": true, "eğer": true, "ise": true, "ki": true,
	"de": true, "da": true, "den": true, "dan": true, "ile": true,
	"gibi": true, "kadar": true, "için": true, "olarak": true, "göre": true,
	"bu": true, "şu": true, "o": true, "bir": true, "her": true,
	"bazı": true, "tüm": true, "çok": true, "az": true, "fazla": true,
	"en": true, "daha": true, "mi": true, "mı": true, "mu": true, "mü": true,
	"ne": true, "nasıl": true, "neden": true, "kim": true, "hangi": true,
}

// NewAnalyzer creates a new text analyzer for the given language.
// lang: "en" (English) or "tr" (Turkish). Unsupported languages fall back to no-op stemming.
// enableStopWords: if true, common stop words are removed.
// enableStemmer: if true, words are stemmed using Snowball.
func NewAnalyzer(lang string, enableStopWords, enableStemmer bool) *Analyzer {
	a := &Analyzer{
		lang:            strings.ToLower(lang),
		enableStopWords: enableStopWords,
		enableStemmer:   enableStemmer,
	}

	if enableStopWords {
		switch a.lang {
		case "en", "english":
			a.stopWords = englishStopWords
		case "tr", "turkish":
			a.stopWords = turkishStopWords
		}
	}

	if enableStemmer {
		switch a.lang {
		case "en", "english":
			a.stemmer = func(s string) string {
				env := snowballstem.NewEnv(s)
				english.Stem(env)
				return env.AssignTo()
			}
		case "tr", "turkish":
			a.stemmer = func(s string) string {
				env := snowballstem.NewEnv(s)
				turkish.Stem(env)
				return env.AssignTo()
			}
		}
	}

	return a
}

// Analyze processes text through the analyzer pipeline:
// tokenization → lowercase → stop-word filter → stemming.
// It returns the analyzed tokens (without FTS5 prefix expansion).
func (a *Analyzer) Analyze(text string) []string {
	tokens := AnalyzeToken(text)
	var result []string
	for _, token := range tokens {
		token = LowerToken(token)
		if token == "" {
			continue
		}
		if a.enableStopWords && a.stopWords != nil && a.stopWords[token] {
			continue
		}
		if a.enableStemmer && a.stemmer != nil {
			result = append(result, a.stemmer(token))
		} else {
			result = append(result, token)
		}
	}
	return result
}

// AnalyzeForQuery is like Analyze but adds a `*` prefix expansion for stemmed terms.
// This allows FTS5 to match both the stemmed form and its suffixes (e.g., "kitap*" matches "kitap" and "kitaplar").
func (a *Analyzer) AnalyzeForQuery(text string) []string {
	tokens := AnalyzeToken(text)
	var result []string
	for _, token := range tokens {
		token = LowerToken(token)
		if token == "" {
			continue
		}
		if a.enableStopWords && a.stopWords != nil && a.stopWords[token] {
			continue
		}
		if a.enableStemmer && a.stemmer != nil {
			stemmed := a.stemmer(token)
			if stemmed != token {
				result = append(result, stemmed+"*")
			} else {
				result = append(result, stemmed)
			}
		} else {
			result = append(result, token)
		}
	}
	return result
}

// Stem stems a single word using the analyzer's stemmer.
// Returns the original word if stemming is disabled or the language is unsupported.
func (a *Analyzer) Stem(word string) string {
	if a.enableStemmer && a.stemmer != nil {
		return a.stemmer(word)
	}
	return word
}

// IsStopWord reports whether the word is a stop word for the analyzer's language.
func (a *Analyzer) IsStopWord(word string) bool {
	if !a.enableStopWords || a.stopWords == nil {
		return false
	}
	return a.stopWords[strings.ToLower(word)]
}
