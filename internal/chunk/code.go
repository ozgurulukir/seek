package chunk

import (
	"strings"
)

// ChunkCode splits source code content into logical chunks.
// It prioritizes logical block boundaries (blank lines, top-level definitions)
// and falls back to line-based sliding windows with overlap for large blocks.
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

	// Split by blocks (double newlines / logical paragraph separations)
	rawBlocks := splitCodeBlocks(content)

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
