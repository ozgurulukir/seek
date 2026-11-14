// Package jsonl provides shared JSON Lines scanning primitives.
package jsonl

import (
	"bufio"
	"io"
)

// MaxLineSize accommodates conversation entries containing inline images.
const MaxLineSize = 10 * 1024 * 1024

// NewScanner returns a scanner configured for seek conversation JSONL files.
// The buffer starts small and grows lazily up to MaxLineSize, so typical
// files do not pay for the full 10MB allocation up front.
func NewScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineSize)
	return scanner
}
