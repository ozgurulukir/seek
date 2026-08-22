package source

import (
	"os"
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

func TestParseCodexFileWithImages_EmptyTextWithImages(t *testing.T) {
	// Redirect HOME so image writes go to a temp dir, not the user's real cache.
	t.Setenv("HOME", t.TempDir())

	// A 1x1 PNG data URI (70-byte PNG).
	const pngDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

	// A prior text message, followed by a response item whose content is ONLY
	// an image block (no input_text / output_text).
	lines := []string{
		`{"type":"session_meta","payload":{"id":"sess-123"}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Here is a screenshot of my app"}]}}`,
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_image","image_url":"` + pngDataURI + `"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, sessionID, images, err := ParseCodexFileWithImages(path, 0)
	if err != nil {
		t.Fatalf("ParseCodexFileWithImages: %v", err)
	}
	if sessionID != "sess-123" {
		t.Errorf("sessionID = %q, want %q", sessionID, "sess-123")
	}

	// An image-only response item must NOT append a new empty message:
	// the result is exactly the single prior text message.
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message (no empty message from image-only item), got %d: %#v", len(msgs), msgs)
	}
	if msgs[0].Role != RoleUser || msgs[0].Content != "Here is a screenshot of my app" {
		t.Errorf("unexpected first message: %#v", msgs[0])
	}

	// The image must be extracted and attached to the prior message:
	// its context is the last message's content (image has no text of its own),
	// Index is 0, and it is persisted into the (temp-HOME) image cache dir.
	if len(images) != 1 {
		t.Fatalf("expected exactly 1 image, got %d", len(images))
	}
	img := images[0]
	wantContext := "Here is a screenshot of my app"
	if img.Context != wantContext {
		t.Errorf("image context = %q, want prior message content %q", img.Context, wantContext)
	}
	if img.Index != 0 {
		t.Errorf("image index = %d, want 0", img.Index)
	}
	if img.MediaType != "image/png" {
		t.Errorf("image media type = %q, want %q", img.MediaType, "image/png")
	}
	if len(img.Data) == 0 || img.Data[0] != 0x89 || img.Data[1] != 'P' {
		t.Errorf("image data is not a PNG: % x", img.Data)
	}
	wantSavedPath := filepath.Join(ImageCacheDir(), "codex-sess-123-0.png")
	if img.SavedPath != wantSavedPath {
		t.Errorf("saved path = %q, want %q", img.SavedPath, wantSavedPath)
	}
	if _, err := os.Stat(img.SavedPath); err != nil {
		t.Errorf("expected saved image at %q: %v", img.SavedPath, err)
	}
}
