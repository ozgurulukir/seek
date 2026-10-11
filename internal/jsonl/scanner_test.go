package jsonl_test

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ozgurulukir/seek/internal/jsonl"
)

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
