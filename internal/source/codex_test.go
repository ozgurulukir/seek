package source

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexFileUserInputText(t *testing.T) {
	lines := []string{
		`{"type":"session_meta","payload":{"id":"sess-123"}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Hello codex"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, sessionID, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if sessionID != "sess-123" {
		t.Errorf("expected sessionID %q, got %q", "sess-123", sessionID)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role=user, got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Hello codex" {
		t.Errorf("expected content %q, got %q", "Hello codex", msgs[0].Content)
	}
}

func TestParseCodexFileAssistantOutputText(t *testing.T) {
	lines := []string{
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Here is the answer"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, _, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected role=assistant, got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Here is the answer" {
		t.Errorf("expected content %q, got %q", "Here is the answer", msgs[0].Content)
	}
}

func TestParseCodexFileMultipleTextBlocks(t *testing.T) {
	lines := []string{
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"First"},{"type":"output_text","text":"Second"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, _, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "First") || !strings.Contains(msgs[0].Content, "Second") {
		t.Errorf("expected both text blocks in content, got %q", msgs[0].Content)
	}
}

func TestParseCodexFileMixedConversation(t *testing.T) {
	lines := []string{
		`{"type":"session_meta","payload":{"id":"abc-123"}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Question 1"}]}}`,
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Answer 1"}]}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Question 2"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, sessionID, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if sessionID != "abc-123" {
		t.Errorf("expected sessionID %q, got %q", "abc-123", sessionID)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	wantRoles := []string{"user", "assistant", "user"}
	for i, m := range msgs {
		if m.Role != wantRoles[i] {
			t.Errorf("msg %d: expected role %q, got %q", i, wantRoles[i], m.Role)
		}
	}
}

func TestParseCodexFileFromLine(t *testing.T) {
	lines := []string{
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"First"}]}}`,
		`{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Second"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, _, err := ParseCodexFile(path, 1)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (skipping first line), got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "Second" {
		t.Errorf("expected assistant/Second, got %q/%q", msgs[0].Role, msgs[0].Content)
	}
}

func TestParseCodexFileSkipsInvalidAndNonText(t *testing.T) {
	lines := []string{
		`not json`,
		`{"type":"other","payload":{}}`,
		`{"type":"response_item","payload":{"role":"system","content":[{"type":"input_text","text":"ignored role"}]}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"function_call","name":"foo"}]}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Valid message"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, _, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected only 1 valid message, got %d", len(msgs))
	}
	if msgs[0].Content != "Valid message" {
		t.Errorf("expected content %q, got %q", "Valid message", msgs[0].Content)
	}
}

func TestParseCodexFileEmptyContentSkipped(t *testing.T) {
	lines := []string{
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":""}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, _, err := ParseCodexFile(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFile: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty text, got %d", len(msgs))
	}
}

func TestParseCodexFileNonexistent(t *testing.T) {
	_, _, err := ParseCodexFile(filepath.Join(t.TempDir(), "missing.jsonl"), 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseDataURIValid(t *testing.T) {
	// base64 for "hello" = aGVsbG8=
	uri := "data:image/png;base64,aGVsbG8="
	mediaType, data, ok := parseDataURI(uri)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if mediaType != "image/png" {
		t.Errorf("expected mediaType image/png, got %q", mediaType)
	}
	if string(data) != "hello" {
		t.Errorf("expected decoded data %q, got %q", "hello", string(data))
	}
}

func TestParseDataURINoPadding(t *testing.T) {
	// base64 for "hi" without padding = aGk
	uri := "data:image/jpeg;base64,aGk"
	mediaType, data, ok := parseDataURI(uri)
	if !ok {
		t.Fatal("expected parse to succeed with raw base64")
	}
	if mediaType != "image/jpeg" {
		t.Errorf("expected mediaType image/jpeg, got %q", mediaType)
	}
	if string(data) != "hi" {
		t.Errorf("expected decoded data %q, got %q", "hi", string(data))
	}
}

func TestParseDataURIInvalid(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{"not data uri", "http://example.com/image.png"},
		{"missing semicolon", "data:image/png,iVBOR"},
		{"missing base64 prefix", "data:image/png;raw,iVBOR"},
		{"invalid base64", "data:image/png;base64,!!!notbase64!!!"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := parseDataURI(tt.uri)
			if ok {
				t.Errorf("expected parse to fail for %q", tt.uri)
			}
		})
	}
}

func TestParseCodexFileWithImages(t *testing.T) {
	// Redirect HOME so image writes go to a temp dir, not the user's real cache.
	t.Setenv("HOME", t.TempDir())

	// A user message with an input_image using a data URI.
	lines := []string{
		`{"type":"session_meta","payload":{"id":"sess-img-1"}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Here is an image"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, sessionID, _, err := ParseCodexFileWithImages(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFileWithImages: %v", err)
	}
	if sessionID != "sess-img-1" {
		t.Errorf("expected sessionID %q, got %q", "sess-img-1", sessionID)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "Here is an image") {
		t.Errorf("expected text content, got %q", msgs[0].Content)
	}
}

func TestScanCodexFilesNoDirectory(t *testing.T) {
	// Set HOME to a temp dir with no .codex — should return empty, no error.
	t.Setenv("HOME", t.TempDir())
	files, err := ScanCodexFiles()
	if err != nil {
		t.Fatalf("ScanCodexFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}
