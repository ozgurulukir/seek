package chunk

import (
	"strings"
	"unicode/utf8"
)

const (
	DefaultMaxChunkSize = 1000 // chars
	DefaultOverlap      = 100  // chars
)

// ChunkType distinguishes text chunks from image chunks.
type ChunkType int

const (
	ChunkText  ChunkType = iota // 0
	ChunkImage                  // 1
)

type Chunk struct {
	Seq       int
	Content   string    // text content or context for image
	Type      ChunkType // ChunkText or ChunkImage
	ImagePath string    // path to image file on disk (for image chunks)
	StartLine int       // 1-based start line in source document
	EndLine   int       // 1-based end line in source document
}

// ChunkMarkdown splits markdown content into chunks by headers, then by size.
func ChunkMarkdown(content string, maxSize, overlap int) []Chunk {
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}
	if overlap <= 0 {
		overlap = DefaultOverlap
	}
	if overlap >= maxSize {
		overlap = maxSize / 2
	}

	// Split by headers first
	sections := splitByHeaders(content)

	var chunks []Chunk
	seq := 0

	for _, section := range sections {
		text := strings.TrimSpace(section)
		if text == "" {
			continue
		}

		if len(text) <= maxSize {
			chunks = append(chunks, Chunk{Seq: seq, Content: text, Type: ChunkText})
			seq++
			continue
		}

		// Split large sections by paragraphs, then by size
		parts := splitBySize(text, maxSize, overlap)
		for _, p := range parts {
			chunks = append(chunks, Chunk{Seq: seq, Content: p, Type: ChunkText})
			seq++
		}
	}

	return AssignLineNumbers(content, chunks)
}

// ChunkConversation splits conversation text into chunks.
// Each chunk is a sequence of messages up to maxSize.
func ChunkConversation(content string, maxSize int) []Chunk {
	if maxSize <= 0 {
		maxSize = DefaultMaxChunkSize
	}

	lines := strings.Split(content, "\n")
	var chunks []Chunk
	seq := 0
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > maxSize && current.Len() > 0 {
			chunks = append(chunks, Chunk{Seq: seq, Content: strings.TrimSpace(current.String()), Type: ChunkText})
			seq++
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, Chunk{Seq: seq, Content: text, Type: ChunkText})
		}
	}

	return AssignLineNumbers(content, chunks)
}

// AssignLineNumbers computes best-effort 1-based line spans by matching each
// chunk against content. Chunk normalization and character-based overlap can
// make an exact source match impossible; callers must not treat these spans as
// parser-grade source locations. Exact spans require future chunker APIs to
// carry source offsets while constructing chunks.
func AssignLineNumbers(fullContent string, chunks []Chunk) []Chunk {
	if len(chunks) == 0 {
		return chunks
	}
	// PERF: avoid allocating a new string if there are no CRLF newlines.
	normContent := fullContent
	if strings.IndexByte(fullContent, '\r') >= 0 {
		normContent = strings.ReplaceAll(fullContent, "\r\n", "\n")
	}

	// PERF: pre-trim document lines once when scanning line breaks, eliminating redundant strings.TrimSpace calls during chunk matching.
	numLines := strings.Count(normContent, "\n") + 1
	lines := make([]string, 0, numLines)

	offset := 0
	for {
		idx := strings.IndexByte(normContent[offset:], '\n')
		if idx < 0 {
			lines = append(lines, strings.TrimSpace(normContent[offset:]))
			break
		}
		lines = append(lines, strings.TrimSpace(normContent[offset:offset+idx]))
		offset += idx + 1
	}
	totalLines := len(lines)
	if totalLines == 0 {
		totalLines = 1
	}

	searchPos := 0
	// Lookahead bound for the match scan: chunks are sequential, so the true
	// position sits near the last match; a run of misses used to rescan the
	// entire remaining document per chunk (quadratic on large inputs) and,
	// searchPos never advancing, collapsed consecutive fallback spans onto
	// the same lines (review 2026-09-17 M14).
	const lookaheadSlack = 64

	// PERF: reuse a single slice buffer across chunks to avoid per-chunk strings.Split allocations.
	var chunkLines []string

	for i := range chunks {
		cText := strings.TrimSpace(chunks[i].Content)
		if cText == "" {
			chunks[i].StartLine = 1
			chunks[i].EndLine = 1
			continue
		}

		chunkLines = chunkLines[:0]
		remainder := cText
		for len(remainder) > 0 {
			if idx := strings.IndexByte(remainder, '\n'); idx >= 0 {
				chunkLines = append(chunkLines, strings.TrimSpace(remainder[:idx]))
				remainder = remainder[idx+1:]
			} else {
				chunkLines = append(chunkLines, strings.TrimSpace(remainder))
				remainder = ""
			}
		}
		chunkLineCount := len(chunkLines)

		startLine := searchPos + 1
		matched := false
		windowEnd := searchPos + 2*chunkLineCount + lookaheadSlack
		for j := searchPos; j+chunkLineCount <= len(lines) && j < windowEnd; j++ {
			matches := true
			for k := range chunkLines {
				if lines[j+k] != chunkLines[k] {
					matches = false
					break
				}
			}
			if matches {
				startLine = j + 1
				// Advance by one line rather than by the whole chunk so an
				// overlapping successor can still match shared source lines.
				searchPos = j + 1
				matched = true
				break
			}
		}
		if !matched {
			// Step past this chunk's footprint so a run of misses yields
			// distinct, advancing spans instead of identical fallbacks.
			searchPos += chunkLineCount
			if searchPos > len(lines) {
				searchPos = len(lines)
			}
		}

		endLine := startLine + chunkLineCount - 1
		if endLine > totalLines {
			endLine = totalLines
		}
		if startLine > totalLines {
			startLine = totalLines
		}
		if startLine < 1 {
			startLine = 1
		}
		chunks[i].StartLine = startLine
		chunks[i].EndLine = endLine
	}
	return chunks
}

// PERF: scan lines with strings.IndexByte and slice section bounds directly from content, eliminating line slice and string builder allocations.
func splitByHeaders(content string) []string {
	sections := make([]string, 0, max(4, strings.Count(content, "\n#")+1))
	sectionStart := 0
	currOffset := 0

	remainder := content
	for len(remainder) > 0 {
		lineStart := currOffset
		var line string
		if idx := strings.IndexByte(remainder, '\n'); idx >= 0 {
			line = remainder[:idx]
			currOffset += idx + 1
			remainder = remainder[idx+1:]
		} else {
			line = remainder
			currOffset += len(remainder)
			remainder = ""
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && lineStart > sectionStart {
			sections = append(sections, content[sectionStart:lineStart])
			sectionStart = lineStart
		}
	}

	if sectionStart < len(content) {
		sections = append(sections, content[sectionStart:])
	}

	return sections
}

func splitBySize(text string, maxSize, overlap int) []string {
	// Split by paragraphs first
	paragraphs := strings.Split(text, "\n\n")

	var parts []string
	var current strings.Builder

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if current.Len()+len(p)+2 > maxSize && current.Len() > 0 {
			parts = append(parts, strings.TrimSpace(current.String()))
			// Keep overlap from end of current
			tail := current.String()
			current.Reset()
			if overlap > 0 && len(tail) > overlap {
				current.WriteString(tailBytes(tail, overlap))
				current.WriteString("\n\n")
			}
		}
		current.WriteString(p)
		current.WriteString("\n\n")
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// tailBytes returns the last n bytes of s advanced to the next valid UTF-8
// rune boundary so the overlap never starts mid-rune.
func tailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
