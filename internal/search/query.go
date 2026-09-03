package search

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// --- AST Types ---

type Query interface {
	String() string
}

type BooleanQuery struct {
	Left  Query
	Op    string // "AND", "OR", "NOT"
	Right Query
}

func (q *BooleanQuery) String() string {
	return fmt.Sprintf("(%s %s %s)", q.Left.String(), q.Op, q.Right.String())
}

type PhraseQuery struct {
	Terms []string
}

func (q *PhraseQuery) String() string {
	return fmt.Sprintf(`"%s"`, strings.Join(q.Terms, " "))
}

type PrefixQuery struct {
	Prefix string
}

func (q *PrefixQuery) String() string {
	return q.Prefix + "*"
}

type FuzzyQuery struct {
	Term string
	N    int
}

func (q *FuzzyQuery) String() string {
	return fmt.Sprintf("%s~%d", q.Term, q.N)
}

type FieldQuery struct {
	Field string
	Query Query
}

func (q *FieldQuery) String() string {
	return fmt.Sprintf("%s:%s", q.Field, q.Query.String())
}

type NearQuery struct {
	Terms []string
	N     int
}

func (q *NearQuery) String() string {
	return fmt.Sprintf("NEAR(%s, %d)", strings.Join(q.Terms, " "), q.N)
}

type TermQuery struct {
	Value string
}

func (q *TermQuery) String() string {
	return q.Value
}

// --- Scanner ---

type tokenType int

const (
	tokenEOF tokenType = iota
	tokenIdent
	tokenString
	tokenNumber
	tokenLParen
	tokenRParen
	tokenComma
	tokenColon
	tokenStar
)

type token struct {
	typ tokenType
	val string
	pos int
}

type scanner struct {
	input string
	pos   int
}

func newScanner(s string) *scanner {
	return &scanner{input: s}
}

func (s *scanner) peek() rune {
	if s.pos >= len(s.input) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s.input[s.pos:])
	return r
}

func (s *scanner) next() rune {
	if s.pos >= len(s.input) {
		return 0
	}
	r, size := utf8.DecodeRuneInString(s.input[s.pos:])
	s.pos += size
	return r
}

func (s *scanner) skipSpace() {
	for s.pos < len(s.input) {
		r, size := utf8.DecodeRuneInString(s.input[s.pos:])
		if unicode.IsSpace(r) {
			s.pos += size
		} else {
			break
		}
	}
}

func (s *scanner) scanIdent() string {
	start := s.pos
	for s.pos < len(s.input) {
		r, size := utf8.DecodeRuneInString(s.input[s.pos:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			s.pos += size
		} else {
			break
		}
	}
	// Include trailing * for prefix queries (e.g., pref*)
	if s.pos < len(s.input) && s.peek() == '*' {
		s.next()
	}
	// Include trailing ~N for fuzzy queries (e.g., term~ or term~2)
	if s.pos < len(s.input) && s.peek() == '~' {
		s.next()
		for s.pos < len(s.input) {
			r, size := utf8.DecodeRuneInString(s.input[s.pos:])
			if unicode.IsDigit(r) {
				s.pos += size
			} else {
				break
			}
		}
	}
	return s.input[start:s.pos]
}

func (s *scanner) scanNumber() string {
	start := s.pos
	for s.pos < len(s.input) {
		r, size := utf8.DecodeRuneInString(s.input[s.pos:])
		if r >= '0' && r <= '9' {
			s.pos += size
		} else {
			break
		}
	}
	return s.input[start:s.pos]
}

func (s *scanner) scanString(quote rune) string {
	start := s.pos
	s.next() // skip opening quote
	for s.pos < len(s.input) {
		r := s.next()
		if r == quote {
			return s.input[start:s.pos]
		}
	}
	return s.input[start:s.pos]
}

func (s *scanner) scanToken() token {
	s.skipSpace()
	if s.pos >= len(s.input) {
		return token{typ: tokenEOF, pos: s.pos}
	}
	pos := s.pos
	r := s.peek()

	switch {
	case r == '(':
		s.next()
		return token{typ: tokenLParen, pos: pos}
	case r == ')':
		s.next()
		return token{typ: tokenRParen, pos: pos}
	case r == ',':
		s.next()
		return token{typ: tokenComma, pos: pos}
	case r == ':':
		s.next()
		return token{typ: tokenColon, pos: pos}
	case r == '*':
		s.next()
		return token{typ: tokenStar, pos: pos}
	case r == '"':
		return token{typ: tokenString, val: s.scanString('"'), pos: pos}
	case r == '\'':
		return token{typ: tokenString, val: s.scanString('\''), pos: pos}
	case r >= '0' && r <= '9':
		return token{typ: tokenNumber, val: s.scanNumber(), pos: pos}
	case unicode.IsLetter(r) || r == '_':
		val := s.scanIdent()
		upper := strings.ToUpper(val)
		switch upper {
		case "AND", "OR", "NOT", "NEAR":
			return token{typ: tokenIdent, val: upper, pos: pos}
		}
		return token{typ: tokenIdent, val: val, pos: pos}
	default:
		s.next()
		return token{typ: tokenEOF, val: string(r), pos: pos}
	}
}

// --- Parser ---

type parser struct {
	s *scanner
}

func newParser(input string) *parser {
	return &parser{s: newScanner(input)}
}

func (p *parser) parse() (Query, error) {
	q, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	// Check for trailing garbage
	if tok := p.s.scanToken(); tok.typ != tokenEOF {
		return nil, fmt.Errorf("unexpected token %q at position %d", tok.val, tok.pos)
	}
	return q, nil
}

func (p *parser) parseExpr() (Query, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Query, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		tok := p.s.scanToken()
		if tok.typ == tokenEOF || tok.typ == tokenRParen {
			p.s.pos = tok.pos // put back
			return left, nil
		}
		if tok.typ != tokenIdent || tok.val != "OR" {
			p.s.pos = tok.pos // put back
			return left, nil
		}
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BooleanQuery{Left: left, Op: "OR", Right: right}
	}
}

func (p *parser) parseAnd() (Query, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		tok := p.s.scanToken()
		if tok.typ == tokenEOF || tok.typ == tokenRParen {
			p.s.pos = tok.pos // put back
			return left, nil
		}
		if tok.typ == tokenIdent && tok.val == "OR" {
			p.s.pos = tok.pos // put back
			return left, nil
		}
		if tok.typ == tokenIdent && tok.val == "AND" {
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &BooleanQuery{Left: left, Op: "AND", Right: right}
			continue
		}
		// Implicit AND: adjacent terms/expressions without operator are treated as AND
		p.s.pos = tok.pos // put back
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BooleanQuery{Left: left, Op: "AND", Right: right}
	}
}

func (p *parser) parseUnary() (Query, error) {
	tok := p.s.scanToken()
	if tok.typ == tokenEOF || tok.typ == tokenRParen {
		p.s.pos = tok.pos
		return nil, fmt.Errorf("unexpected end of query at position %d", tok.pos)
	}
	if tok.typ == tokenIdent && tok.val == "NOT" {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		// NOT wraps the right side in a BooleanQuery with a dummy left
		return &BooleanQuery{Left: &TermQuery{Value: ""}, Op: "NOT", Right: right}, nil
	}
	p.s.pos = tok.pos // put back
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Query, error) {
	tok := p.s.scanToken()
	if tok.typ == tokenEOF {
		return nil, fmt.Errorf("unexpected end of query")
	}

	switch tok.typ {
	case tokenLParen:
		q, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if tok := p.s.scanToken(); tok.typ != tokenRParen {
			return nil, fmt.Errorf("expected ')' at position %d", tok.pos)
		}
		return q, nil
	case tokenString:
		return &PhraseQuery{Terms: AnalyzeToken(tok.val)}, nil
	case tokenIdent:
		// Check for NEAR
		if tok.val == "NEAR" {
			return p.parseNear()
		}
		// Check for field:term
		if next := p.s.peek(); next == ':' {
			p.s.pos++ // skip ':'
			term, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			return &FieldQuery{Field: strings.ToLower(tok.val), Query: term}, nil
		}
		// Check for prefix (ends with *)
		if strings.HasSuffix(tok.val, "*") {
			return &PrefixQuery{Prefix: strings.TrimSuffix(tok.val, "*")}, nil
		}
		// Check for fuzzy (term~N)
		if idx := strings.Index(tok.val, "~"); idx >= 0 {
			prefix := tok.val[:idx]
			suffix := tok.val[idx+1:]
			if suffix == "" {
				return &FuzzyQuery{Term: prefix, N: 2}, nil
			}
			var n int
			if _, err := fmt.Sscanf(suffix, "%d", &n); err != nil || n < 1 {
				n = 2
			}
			return &FuzzyQuery{Term: prefix, N: n}, nil
		}
		return &TermQuery{Value: tok.val}, nil
	case tokenNumber:
		// A bare number is treated as a term
		return &TermQuery{Value: tok.val}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q at position %d", tok.val, tok.pos)
	}
}

func (p *parser) parseNear() (Query, error) {
	tok := p.s.scanToken()
	if tok.typ != tokenLParen {
		return nil, fmt.Errorf("expected '(' after NEAR at position %d", tok.pos)
	}
	var terms []string
	for {
		tok = p.s.scanToken()
		if tok.typ == tokenEOF || tok.typ == tokenRParen {
			break
		}
		if tok.typ == tokenIdent || tok.typ == tokenString || tok.typ == tokenNumber {
			terms = append(terms, tok.val)
		}
		// skip comma
		if tok.typ == tokenComma {
			continue
		}
	}
	if len(terms) < 2 {
		return nil, fmt.Errorf("NEAR requires at least 2 terms")
	}
	// Last term might be N
	n := 5
	if last := terms[len(terms)-1]; isNumber(last) {
		n = atoi(last)
		terms = terms[:len(terms)-1]
	}
	return &NearQuery{Terms: terms, N: n}, nil
}

func isNumber(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// --- Public API ---

// ParseQuery parses a query string into an AST.
// Returns nil, nil if the input is empty.
func ParseQuery(input string) (Query, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	p := newParser(input)
	q, err := p.parse()
	if err != nil {
		return nil, fmt.Errorf("cannot parse query: %w", err)
	}
	return q, nil
}

// --- FTS5 Mapper ---

// ToFTS5 converts a Query AST to an FTS5 MATCH string.
// Fuzzy queries are mapped to prefix expansion (stem*).
// Returns the FTS5 query string and a flag indicating if fuzzy fallback was used.
func ToFTS5(q Query) (string, bool) {
	if q == nil {
		return "", false
	}
	return toFTS5(q, nil)
}

// ToFTS5WithAnalyzer converts a Query AST to an FTS5 MATCH string using the given analyzer.
// The analyzer is applied to term values before generating the FTS5 query.
// Stemmed terms are expanded with a `*` prefix for FTS5 matching.
func ToFTS5WithAnalyzer(q Query, a *Analyzer) (string, bool) {
	if q == nil {
		return "", false
	}
	return toFTS5(q, a)
}

// firstAnalyzed returns the first analyzer-produced term for s, or s itself
// when no analyzer is configured or analysis yields nothing.
func firstAnalyzed(a *Analyzer, s string) string {
	if a != nil {
		if analyzed := a.AnalyzeForQuery(s); len(analyzed) > 0 {
			return analyzed[0]
		}
	}
	return s
}

func toFTS5(q Query, a *Analyzer) (string, bool) {
	switch v := q.(type) {
	case *BooleanQuery:
		ls, _ := toFTS5(v.Left, a)
		rs, _ := toFTS5(v.Right, a)
		op := strings.ToUpper(v.Op)
		if op == "NOT" {
			if ls == "" || ls == "()" || ls == `""` {
				// Pure negation queries (unary NOT) cannot be answered by FTS5 alone without a positive set.
				return "", false
			}
			return fmt.Sprintf("(%s NOT %s)", ls, rs), false
		}
		if ls == "" {
			return rs, false
		}
		if rs == "" {
			return ls, false
		}
		return fmt.Sprintf("(%s %s %s)", ls, op, rs), false
	case *PhraseQuery:
		var phraseTerms []string
		for _, t := range v.Terms {
			phraseTerms = append(phraseTerms, firstAnalyzed(a, t))
		}
		return fmt.Sprintf(`"%s"`, strings.Join(phraseTerms, " ")), false
	case *PrefixQuery:
		if a != nil {
			analyzed := a.AnalyzeForQuery(v.Prefix)
			if len(analyzed) > 0 {
				return analyzed[0], false
			}
		}
		return v.Prefix + "*", false
	case *FuzzyQuery:
		// FTS5 doesn't support fuzzy; map to prefix expansion
		if a != nil {
			analyzed := a.AnalyzeForQuery(v.Term)
			if len(analyzed) > 0 {
				return analyzed[0], true
			}
		}
		return v.Term + "*", true
	case *FieldQuery:
		fs, _ := toFTS5(v.Query, a)
		return fmt.Sprintf("%s:%s", v.Field, fs), false
	case *NearQuery:
		terms := make([]string, len(v.Terms))
		for i, t := range v.Terms {
			terms[i] = firstAnalyzed(a, t)
		}
		return fmt.Sprintf("NEAR(%s, %d)", strings.Join(terms, " "), v.N), false
	case *TermQuery:
		return firstAnalyzed(a, v.Value), false
	default:
		return "", false
	}
}

// --- Query Analysis ---

// AnalyzeToken splits text into tokens compatible with unicode61.
func AnalyzeToken(s string) []string {
	var tokens []string
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// LowerToken lowercases a token (basic ASCII + Unicode case folding).
func LowerToken(s string) string {
	return strings.ToLower(s)
}

// --- Error types ---

var ErrParse = errors.New("parse error")
