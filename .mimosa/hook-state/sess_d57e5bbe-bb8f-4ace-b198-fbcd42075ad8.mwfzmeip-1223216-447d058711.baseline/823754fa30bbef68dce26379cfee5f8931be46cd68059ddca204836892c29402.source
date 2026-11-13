package indexer

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type Page struct {
	Seq  int
	Text string
}

func BenchmarkOriginal(b *testing.B) {
	pages := []Page{
		{Seq: 0, Text: "This is some extracted text from page 1. It might be moderately long."},
		{Seq: 1, Text: "This is some extracted text from page 2. It might be moderately long."},
		{Seq: 2, Text: "This is some extracted text from page 3. It might be moderately long."},
		{Seq: 3, Text: "This is some extracted text from page 4. It might be moderately long."},
		{Seq: 4, Text: "This is some extracted text from page 5. It might be moderately long."},
	}
	fName := "example_document.pdf"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var pageText strings.Builder
		for _, pg := range pages {
			content := fmt.Sprintf("PDF page %d of %s", pg.Seq+1, fName)
			if pg.Text != "" {
				content += "\n" + pg.Text
				pageText.WriteString(pg.Text)
				pageText.WriteString("\n")
			}
			_ = content // simulate use
		}
		_ = pageText.String()
	}
}

func BenchmarkOptimized(b *testing.B) {
	pages := []Page{
		{Seq: 0, Text: "This is some extracted text from page 1. It might be moderately long."},
		{Seq: 1, Text: "This is some extracted text from page 2. It might be moderately long."},
		{Seq: 2, Text: "This is some extracted text from page 3. It might be moderately long."},
		{Seq: 3, Text: "This is some extracted text from page 4. It might be moderately long."},
		{Seq: 4, Text: "This is some extracted text from page 5. It might be moderately long."},
	}
	fName := "example_document.pdf"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var pageText strings.Builder
		for _, pg := range pages {
			var cb strings.Builder

			seqStr := strconv.Itoa(pg.Seq + 1)
			length := len("PDF page ") + len(seqStr) + len(" of ") + len(fName)
			if pg.Text != "" {
				length += 1 + len(pg.Text)
			}
			cb.Grow(length)

			cb.WriteString("PDF page ")
			cb.WriteString(seqStr)
			cb.WriteString(" of ")
			cb.WriteString(fName)

			if pg.Text != "" {
				cb.WriteByte('\n')
				cb.WriteString(pg.Text)

				pageText.WriteString(pg.Text)
				pageText.WriteByte('\n')
			}
			content := cb.String()
			_ = content // simulate use
		}
		_ = pageText.String()
	}
}
