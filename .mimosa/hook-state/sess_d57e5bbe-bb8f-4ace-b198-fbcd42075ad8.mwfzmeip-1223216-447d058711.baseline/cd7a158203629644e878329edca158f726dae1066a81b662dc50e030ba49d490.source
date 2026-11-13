package jsonl_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/jsonl"
)

type errReader struct {
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, e.err
}

func TestNewScanner_Basic(t *testing.T) {
	input := "{\"id\": 1}\n{\"id\": 2}\n{\"id\": 3}"
	scanner := jsonl.NewScanner(strings.NewReader(input))

	expected := []string{
		`{"id": 1}`,
		`{"id": 2}`,
		`{"id": 3}`,
	}

	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != len(expected) {
		t.Fatalf("expected %d lines, got %d", len(expected), len(got))
	}

	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("line %d: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestNewScanner_BufferBoundaries(t *testing.T) {
	t.Run("exact initial buffer boundary (64 KB)", func(t *testing.T) {
		lineSize := 64 * 1024
		line := strings.Repeat("x", lineSize)
		scanner := jsonl.NewScanner(strings.NewReader(line + "\n"))

		if !scanner.Scan() {
			t.Fatalf("failed to scan 64KB line: %v", scanner.Err())
		}
		if len(scanner.Text()) != lineSize {
			t.Errorf("expected length %d, got %d", lineSize, len(scanner.Text()))
		}
	})

	t.Run("exceed initial buffer boundary (64 KB + 1 byte)", func(t *testing.T) {
		lineSize := 64*1024 + 1
		line := strings.Repeat("y", lineSize)
		scanner := jsonl.NewScanner(strings.NewReader(line + "\n"))

		if !scanner.Scan() {
			t.Fatalf("failed to scan (64KB + 1) line: %v", scanner.Err())
		}
		if len(scanner.Text()) != lineSize {
			t.Errorf("expected length %d, got %d", lineSize, len(scanner.Text()))
		}
	})

	t.Run("maximum supported line size (MaxLineSize - 1)", func(t *testing.T) {
		// When newline '\n' is included, the total buffer needed is MaxLineSize.
		lineSize := jsonl.MaxLineSize - 1
		lineBytes := bytes.Repeat([]byte("z"), lineSize)
		scanner := jsonl.NewScanner(bytes.NewReader(append(lineBytes, '\n')))

		if !scanner.Scan() {
			t.Fatalf("failed to scan max line size line: %v", scanner.Err())
		}
		if len(scanner.Bytes()) != lineSize {
			t.Errorf("expected length %d, got %d", lineSize, len(scanner.Bytes()))
		}
	})
}

func TestNewScanner_LargeLineWithinMaxLineSize(t *testing.T) {
	// 100 KB line (exceeds default initial buffer of 64KB, but well within 10MB MaxLineSize)
	lineSize := 100 * 1024
	largeLine := strings.Repeat("a", lineSize)
	input := largeLine + "\nshort line"

	scanner := jsonl.NewScanner(strings.NewReader(input))

	if !scanner.Scan() {
		t.Fatalf("failed to scan large line, err: %v", scanner.Err())
	}

	if len(scanner.Text()) != lineSize {
		t.Errorf("expected line size %d, got %d", lineSize, len(scanner.Text()))
	}

	if scanner.Text() != largeLine {
		t.Errorf("scanned content mismatch")
	}

	if !scanner.Scan() {
		t.Fatalf("failed to scan second line, err: %v", scanner.Err())
	}

	if scanner.Text() != "short line" {
		t.Errorf("expected %q, got %q", "short line", scanner.Text())
	}

	if scanner.Scan() {
		t.Errorf("expected EOF, but got more lines")
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected error at EOF: %v", err)
	}
}

func TestNewScanner_ExceedsMaxLineSize(t *testing.T) {
	// MaxLineSize + 10 bytes
	overSize := jsonl.MaxLineSize + 10
	largeInput := bytes.Repeat([]byte("b"), overSize)

	scanner := jsonl.NewScanner(bytes.NewReader(largeInput))

	if scanner.Scan() {
		t.Fatalf("expected scan to fail for line exceeding MaxLineSize")
	}

	err := scanner.Err()
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("expected bufio.ErrTooLong, got %v", err)
	}
}

func TestNewScanner_MixedLengthsAndLineEndings(t *testing.T) {
	t.Run("CRLF line endings", func(t *testing.T) {
		input := "line1\r\nline2\r\nline3"
		scanner := jsonl.NewScanner(strings.NewReader(input))

		expected := []string{"line1", "line2", "line3"}
		var got []string
		for scanner.Scan() {
			got = append(got, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(got) != len(expected) {
			t.Fatalf("expected %d lines, got %d", len(expected), len(got))
		}

		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("line %d: expected %q, got %q", i, expected[i], got[i])
			}
		}
	})

	t.Run("varied lengths", func(t *testing.T) {
		shortLine := "short"
		mediumLine := strings.Repeat("m", 70*1024) // 70 KB (> 64 KB initial cap)
		tinyLine := "a"

		input := shortLine + "\n" + mediumLine + "\n" + tinyLine
		scanner := jsonl.NewScanner(strings.NewReader(input))

		expected := []string{shortLine, mediumLine, tinyLine}
		var got []string
		for scanner.Scan() {
			got = append(got, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(got) != len(expected) {
			t.Fatalf("expected %d lines, got %d", len(expected), len(got))
		}

		for i := range expected {
			if got[i] != expected[i] {
				t.Errorf("line %d mismatch", i)
			}
		}
	})
}

func TestNewScanner_ReaderErrorHandling(t *testing.T) {
	customErr := errors.New("read error")
	r := io.MultiReader(strings.NewReader("line1\n"), &errReader{err: customErr})
	scanner := jsonl.NewScanner(r)

	if !scanner.Scan() {
		t.Fatalf("expected first line scan to succeed")
	}
	if scanner.Text() != "line1" {
		t.Errorf("expected line1, got %q", scanner.Text())
	}

	if scanner.Scan() {
		t.Fatalf("expected scan to fail on underlying reader error")
	}

	if !errors.Is(scanner.Err(), customErr) {
		t.Errorf("expected error %v, got %v", customErr, scanner.Err())
	}
}

func TestNewScanner_EmptyAndNoNewline(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		scanner := jsonl.NewScanner(strings.NewReader(""))
		if scanner.Scan() {
			t.Errorf("expected no lines for empty input")
		}
		if err := scanner.Err(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("single line no newline", func(t *testing.T) {
		scanner := jsonl.NewScanner(strings.NewReader("hello world"))
		if !scanner.Scan() {
			t.Fatalf("expected to scan line without newline, err: %v", scanner.Err())
		}
		if scanner.Text() != "hello world" {
			t.Errorf("expected %q, got %q", "hello world", scanner.Text())
		}
		if scanner.Scan() {
			t.Errorf("expected end of input")
		}
	})
}
