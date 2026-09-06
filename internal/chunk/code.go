package chunk

import (
	"regexp"
	"strings"
)

// ChunkCode splits source code content into logical chunks.
// It prioritizes logical block boundaries (blank lines, top-level definitions)
// and falls back to line-based sliding windows with overlap for large blocks.
//
// The lang parameter selects language-aware top-level-definition split points
// (e.g. ^func in Go, ^def|^class in Python) so chunks align to symbol
// boundaries when possible. Unrecognised languages fall back to a generic
// blank-line + indented-block heuristic.
func ChunkCode(content string, lang string, maxSize, overlap int) []Chunk {
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}
	if overlap <= 0 {
		overlap = DefaultOverlap
	}
	if overlap >= maxSize {
		overlap = maxSize / 2
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// If whole content fits in maxSize, return single chunk
	if len(content) <= maxSize {
		lineCount := strings.Count(content, "\n") + 1
		return []Chunk{{Seq: 0, Content: content, Type: ChunkText, StartLine: 1, EndLine: lineCount}}
	}

	// Split by top-level definitions first when the language is known,
	// then by blank-line blocks inside each definition.
	rawBlocks := splitCodeTopLevel(content, lang)

	var chunks []Chunk
	seq := 0
	var current strings.Builder

	for _, block := range rawBlocks {
		block = strings.TrimRight(block, "\r\n")
		if block == "" {
			continue
		}

		// If block itself exceeds maxSize, flush current buffer and split block by lines
		if len(block) > maxSize {
			if current.Len() > 0 {
				chunks = append(chunks, Chunk{Seq: seq, Content: strings.TrimSpace(current.String()), Type: ChunkText})
				seq++
				current.Reset()
			}

			subChunks := splitCodeLines(block, maxSize, overlap)
			for _, sc := range subChunks {
				chunks = append(chunks, Chunk{Seq: seq, Content: sc, Type: ChunkText})
				seq++
			}
			continue
		}

		// If appending this block exceeds maxSize, flush current buffer
		if current.Len() > 0 && current.Len()+len(block)+2 > maxSize {
			chunks = append(chunks, Chunk{Seq: seq, Content: strings.TrimSpace(current.String()), Type: ChunkText})
			seq++

			// Overlap: keep tail of current buffer if overlap is configured
			tail := current.String()
			current.Reset()
			if overlap > 0 && len(tail) > overlap {
				// Take last N lines or characters
				lines := strings.Split(tail, "\n")
				var ovBuilder strings.Builder
				for i := len(lines) - 1; i >= 0; i-- {
					line := lines[i]
					if ovBuilder.Len()+len(line)+1 > overlap && ovBuilder.Len() > 0 {
						break
					}
					ovBuilder.WriteString(line)
					ovBuilder.WriteString("\n")
				}
				if ovBuilder.Len() > 0 {
					current.WriteString(strings.TrimSpace(ovBuilder.String()))
					current.WriteString("\n\n")
				}
			}
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(block)
	}

	if current.Len() > 0 {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, Chunk{Seq: seq, Content: text, Type: ChunkText})
		}
	}

	return AssignLineNumbers(content, chunks)
}

// codeDefPatterns maps language IDs to a regex matching lines that start a
// top-level definition. Keep patterns anchored to line start (no leading
// whitespace) so only true top-level forms split.
var codeDefPatterns = map[string]*regexp.Regexp{
	"go":         regexp.MustCompile(`^(func|type)\s+`),
	"python":     regexp.MustCompile(`^(def|class|async\s+def)\s+`),
	"rust":       regexp.MustCompile(`^(pub(\([^)]+\))?\s+)?(async\s+)?(unsafe\s+)?(fn|struct|enum|impl|trait|type|mod|const|static)\s+`),
	"javascript": regexp.MustCompile(`^(export\s+)?(async\s+)?(function|class|const|let|var)\s+`),
	"typescript": regexp.MustCompile(`^(export\s+)?(async\s+)?(function|class|const|let|var|interface|enum|namespace|type)\s+`),
	"ruby":       regexp.MustCompile(`^(def|class|module)\s+`),
	"java":       regexp.MustCompile(`^(public|private|protected|abstract|final|static|\s)*[a-zA-Z][a-zA-Z0-9_<>\[\]]*\s+[a-zA-Z_$][a-zA-Z0-9_$]*\s*\(`),
	"csharp":     regexp.MustCompile(`^(public|private|protected|internal|static|abstract|sealed|partial|async|\s)*[a-zA-Z][a-zA-Z0-9_<>\[\]]*\s+[a-zA-Z_$][a-zA-Z0-9_$]*\s*\(`),
	"c":          regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\s\*]*\s+[a-zA-Z_][a-zA-Z0-9_]*\s*\(`),
	"cpp":        regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\s\*:<>,]*\s+[a-zA-Z_][a-zA-Z0-9_]*\s*\(`),
	"php":        regexp.MustCompile(`^(public|private|protected|static|final|abstract|\s)*function\s+`),
	"swift":      regexp.MustCompile(`^func\s+`),
	"kotlin":     regexp.MustCompile(`^fun\s+`),
	"scala":      regexp.MustCompile(`^(def|class|object|trait)\s+`),
	"lua":        regexp.MustCompile(`^(local\s+)?function\s+`),
	"haskell":    regexp.MustCompile(`^[a-z_][a-zA-Z0-9_']*\s*::`),
	"elm":        regexp.MustCompile(`^[a-z_][a-zA-Z0-9_']*\s*:`),
}

// splitCodeTopLevel attempts a language-aware split at top-level definitions.
// It returns a slice of blocks: one per definition plus any preamble before
// the first definition. Unknown languages fall back to splitCodeBlocks so the
// chunker keeps working even without a pattern.
func splitCodeTopLevel(content, lang string) []string {
	pattern, ok := codeDefPatterns[lang]
	if !ok {
		return splitCodeBlocks(content)
	}

	var blocks []string
	var current strings.Builder
	seenDef := false

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if pattern.MatchString(line) {
			// Flush everything before this definition
			if current.Len() > 0 {
				blocks = append(blocks, current.String())
				current.Reset()
			}
			seenDef = true
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}

	// If no definitions matched, fall back to blank-line blocks so the
	// downstream packing logic still receives *something* to work with.
	if !seenDef {
		return splitCodeBlocks(content)
	}
	return blocks
}

// splitCodeBlocks breaks code on double newlines while normalizing line breaks.
func splitCodeBlocks(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	blocks := strings.Split(content, "\n\n")
	var result []string
	for _, b := range blocks {
		t := strings.TrimSpace(b)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

// splitCodeLines splits a large code block line-by-line with line overlap.
func splitCodeLines(block string, maxSize, overlap int) []string {
	// Guard against degenerate parameters that would cause infinite loops.
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxSize {
		overlap = maxSize / 2
	}

	lines := strings.Split(block, "\n")
	var chunks []string
	var current strings.Builder
	var currentLines []string

	for _, line := range lines {
		lineLen := len(line) + 1

		// If line alone exceeds maxSize, break it into character slices
		if lineLen > maxSize {
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(current.String()))
				current.Reset()
				currentLines = nil
			}
			step := maxSize - overlap
			if step <= 0 {
				step = maxSize
			}
			for i := 0; i < len(line); i += step {
				end := i + maxSize
				if end > len(line) {
					end = len(line)
				}
				chunks = append(chunks, line[i:end])
				if end == len(line) {
					break
				}
			}
			continue
		}

		if current.Len()+lineLen > maxSize && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))

			// Calculate line overlap from end of currentLines
			var overlapBuilder strings.Builder
			overlapLinesCount := 0
			for i := len(currentLines) - 1; i >= 0; i-- {
				l := currentLines[i]
				if overlapBuilder.Len()+len(l)+1 > overlap && overlapLinesCount > 0 {
					break
				}
				overlapBuilder.WriteString(l)
				overlapBuilder.WriteString("\n")
				overlapLinesCount++
			}

			current.Reset()
			currentLines = nil

			// Write overlap content in forward order into the new chunk buffer
			if overlapBuilder.Len() > 0 {
				ovLines := strings.Split(strings.TrimSpace(overlapBuilder.String()), "\n")
				for i := len(ovLines) - 1; i >= 0; i-- {
					current.WriteString(ovLines[i])
					current.WriteString("\n")
					currentLines = append(currentLines, ovLines[i])
				}
			}
		}

		current.WriteString(line)
		current.WriteString("\n")
		currentLines = append(currentLines, line)
	}

	if current.Len() > 0 {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, text)
		}
	}

	return chunks
}
