package store

import (
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Compress compresses data using Zstd with the given compression level (1-22).
// If level <= 0, the default level (3) is used.
func Compress(data []byte, level int) ([]byte, error) {
	if level <= 0 {
		level = 3
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
