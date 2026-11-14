package store

import (
	"testing"
)

func TestCompressDecompressRoundtrip(t *testing.T) {
	input := []byte("The quick brown fox jumps over the lazy dog. " +
		"This is a test of the compression system. " +
		"Running tests ensures code quality.")
	level := 3

	compressed, err := Compress(input, level)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}

	if string(decompressed) != string(input) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decompressed, input)
	}
}

func TestCompressDecompressString(t *testing.T) {
	input := "Hello, world! This is a test string."
	level := 3

	compressed, err := CompressString(input, level)
	if err != nil {
		t.Fatalf("CompressString: %v", err)
	}

	decompressed, err := DecompressString(compressed)
	if err != nil {
		t.Fatalf("DecompressString: %v", err)
	}

	if decompressed != input {
		t.Errorf("roundtrip mismatch: got %q, want %q", decompressed, input)
	}
}

func TestCompressEmpty(t *testing.T) {
	compressed, err := Compress(nil, 3)
	if err != nil {
		t.Fatalf("Compress(nil): %v", err)
	}
	if len(compressed) == 0 {
		t.Error("Compress(nil) returned empty, expected some data")
	}

	decompressed, err := Decompress(nil)
	if err != nil {
		t.Fatalf("Decompress(nil): %v", err)
	}
	if decompressed != nil {
		t.Errorf("Decompress(nil) = %v, want nil", decompressed)
	}
}

func TestCompressEmptyString(t *testing.T) {
	decompressed, err := DecompressString(nil)
	if err != nil {
		t.Fatalf("DecompressString(nil): %v", err)
	}
	if decompressed != "" {
		t.Errorf("DecompressString(nil) = %q, want empty", decompressed)
	}
}

func TestCompressRatio(t *testing.T) {
	// Repetitive text should compress well
	input := make([]byte, 0, 5000)
	pattern := []byte("Lorem ipsum dolor sit amet. ")
	for i := 0; i < 100; i++ {
		input = append(input, pattern...)
	}
	compressed, err := Compress(input, 3)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	ratio := float64(len(input)) / float64(len(compressed))
	if ratio < 2.0 {
		t.Errorf("compression ratio = %.1f, want >= 2.0 for repetitive text", ratio)
	}
}

func TestCompressLevels(t *testing.T) {
	input := make([]byte, 0, 2500)
	pattern := []byte("The quick brown fox jumps over the lazy dog. ")
	for i := 0; i < 50; i++ {
		input = append(input, pattern...)
	}
	for _, level := range []int{1, 3, 10, 22} {
		compressed, err := Compress(input, level)
		if err != nil {
			t.Fatalf("Compress(level=%d): %v", level, err)
		}
		decompressed, err := Decompress(compressed)
		if err != nil {
			t.Fatalf("Decompress(level=%d): %v", level, err)
		}
		if string(decompressed) != string(input) {
			t.Errorf("roundtrip mismatch at level %d", level)
		}
	}
}
