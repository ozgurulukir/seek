package store

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/ozgurulukir/seek/internal/config"
)

// Compress compresses data using Zstd with the given compression level (1-22).
// If level <= 0, the config-owned default level is used.
func Compress(data []byte, level int) ([]byte, error) {
	if level <= 0 {
		level = config.DefaultCompressionLevel
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	defer encoder.Close()

	compressed := encoder.EncodeAll(data, nil)
	return compressed, nil
}

// CompressString compresses a string using Zstd.
func CompressString(s string, level int) ([]byte, error) {
	return Compress([]byte(s), level)
}

// Decompress decompresses Zstd-compressed data.
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()

	decompressed, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	return decompressed, nil
}

// DecompressString decompresses Zstd-compressed data to a string.
func DecompressString(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	b, err := Decompress(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// decodeChunkText resolves a chunk's readable text from its two storage
// columns. The write path always stores plaintext `content` alongside
// `content_zstd`, so a corrupt zstd blob falls back to the raw column when it
// survived; only when both are unusable does it fail loudly. This is the
// single policy for every chunk-read path (surrounding context, FTS rebuild,
// vector search, GetChunkContent, chunk listings) — sites must not re-hand-roll
// their own fallback (review 2026-09-17 L4, audit follow-up).
func decodeChunkText(raw string, zstd []byte, errLabel string) (string, error) {
	if len(zstd) == 0 {
		return raw, nil
	}
	decompressed, err := DecompressString(zstd)
	if err == nil {
		return decompressed, nil
	}
	if raw != "" {
		return raw, nil
	}
	return "", fmt.Errorf("decompress %s: %w", errLabel, err)
}
