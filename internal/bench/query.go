// Package bench implements the search-benchmark-game engine contract
// (https://github.com/quickwit-stuff/search-benchmark-game): bulk corpus
// ingestion over stdin, a line-protocol query server, and report generation
// that merges seek's numbers with the published tantivy bench table. It is
// the code behind `seek bench index|serve|report`.
package bench

import (
	"errors"
	"fmt"
	"strings"
)

// gameClause is one parsed clause of a search-benchmark-game query.
// The game's queries use a Lucene query language subset: bare terms are
// optional clauses (union semantics, Lucene's default OR operator), terms
// and phrases prefixed with '+' are required (intersection semantics), and
// quoted strings are phrase clauses.
type gameClause struct {
	required bool
	text     string
}

// GameQueryToFTS5 translates a search-benchmark-game query into an FTS5
// MATCH expression with Lucene-compatible boolean semantics so per-query hit
// counts line up with the engines published on tantivy-search.github.io/bench:
//
//   - when the query has any required ('+') clause, the match set is the AND
//     of the required clauses — optional clauses only contribute score in
//     Lucene, so they are dropped from the match condition;
//   - when every clause is optional, the match set is the OR of all clauses
//     (Lucene's default operator is OR, and the game's "union" queries rely
//     on it);
//   - quoted clauses stay phrases ("the who" must match adjacently).
//
// Every clause is emitted as a quoted phrase, which makes the output immune
// to FTS5 operator characters in the query text.
func GameQueryToFTS5(query string) (string, error) {
	clauses, err := parseGameQuery(query)
	if err != nil {
		return "", err
	}
	var required, optional []string
	for _, cl := range clauses {
		if cl.required {
			required = append(required, fts5Clause(cl.text))
		} else {
			optional = append(optional, fts5Clause(cl.text))
		}
	}
	var expr string
	switch {
	case len(required) > 0:
		expr = strings.Join(required, " AND ")
	case len(optional) > 0:
		expr = strings.Join(optional, " OR ")
	default:
		return "", fmt.Errorf("bench query %q: no clauses", query)
	}
	return "(" + expr + ")", nil
}

func parseGameQuery(query string) ([]gameClause, error) {
	runes := []rune(query)
	var clauses []gameClause
	i := 0
	for i < len(runes) {
		switch runes[i] {
		case ' ', '\t':
			i++
			continue
		case '+':
			i++
			if i >= len(runes) {
				return nil, fmt.Errorf("bench query %q: dangling '+'", query)
			}
			cl, next, err := readGameClause(runes, i)
			if err != nil {
				return nil, fmt.Errorf("bench query %q: %w", query, err)
			}
			cl.required = true
			clauses = append(clauses, cl)
			i = next
		default:
			cl, next, err := readGameClause(runes, i)
			if err != nil {
				return nil, fmt.Errorf("bench query %q: %w", query, err)
			}
			clauses = append(clauses, cl)
			i = next
		}
	}
	if len(clauses) == 0 {
		return nil, errors.New("bench query: no clauses")
	}
	return clauses, nil
}

// readGameClause reads one term or quoted phrase starting at index i and
// returns the clause plus the index just past it.
func readGameClause(runes []rune, i int) (gameClause, int, error) {
	if runes[i] == '"' {
		end := -1
		for j := i + 1; j < len(runes); j++ {
			if runes[j] == '"' {
				end = j
				break
			}
		}
		if end < 0 {
			return gameClause{}, i, errors.New("unterminated phrase")
		}
		text := strings.ReplaceAll(string(runes[i+1:end]), `"`, "")
		text = strings.TrimSpace(text)
		if text == "" {
			return gameClause{}, i, errors.New("empty phrase")
		}
		if err := checkGameClauseText(text); err != nil {
			return gameClause{}, i, err
		}
		return gameClause{text: text}, end + 1, nil
	}
	j := i
	for j < len(runes) && runes[j] != ' ' && runes[j] != '\t' {
		j++
	}
	text := strings.TrimSpace(string(runes[i:j]))
	if text == "" {
		return gameClause{}, i, errors.New("empty term")
	}
	if err := checkGameClauseText(text); err != nil {
		return gameClause{}, i, err
	}
	return gameClause{text: text}, j, nil
}

// checkGameClauseText rejects Lucene syntax the translator does not model.
// Answering UNSUPPORTED is strictly safer than silently misinterpreting it:
// a '-'-prefixed clause would invert semantics (match-for instead of
// match-not), wildcard characters would lose their prefix semantics, and
// uppercase AND/OR/NOT would be searched as literal terms.
func checkGameClauseText(text string) error {
	if strings.HasPrefix(text, "-") {
		return fmt.Errorf("unsupported Lucene negation clause %q", text)
	}
	if strings.ContainsAny(text, "*?") {
		return fmt.Errorf("unsupported Lucene wildcard clause %q", text)
	}
	switch text {
	case "AND", "OR", "NOT":
		return fmt.Errorf("unsupported Lucene boolean operator %q", text)
	}
	return nil
}

// fts5Clause renders one clause as a quoted FTS5 phrase. Single tokens become
// one-token phrases; multi-word phrase clauses keep their adjacency.
func fts5Clause(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, "") + `"`
}
