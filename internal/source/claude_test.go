package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestParseClaudeFileUserStringContent(t *testing.T) {
	// User message with plain string content.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Hello there"}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role=user, got %q", msgs[0].Role)
	}
	if msgs[0].Content != "Hello there" {
		t.Errorf("expected content %q, got %q", "Hello there", msgs[0].Content)
	}
}

func TestParseClaudeFileUserArrayContent(t *testing.T) {
	// User message with content as an array of blocks.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"First block"},{"type":"text","text":"Second block"}]}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role=user, got %q", msgs[0].Role)
	}
	// extractTextContent joins text blocks with newline.
	if !strings.Contains(msgs[0].Content, "First block") || !strings.Contains(msgs[0].Content, "Second block") {
		t.Errorf("expected both text blocks in content, got %q", msgs[0].Content)
	}
}

func TestParseClaudeFileAssistantStringContent(t *testing.T) {
	// Assistant message with plain string content.
	lines := []string{
		`{"type":"assistant","message":"This is a response"}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected role=assistant, got %q", msgs[0].Role)
	}
	if msgs[0].Content != "This is a response" {
		t.Errorf("expected content %q, got %q", "This is a response", msgs[0].Content)
	}
}

func TestParseClaudeFileAssistantArrayContent(t *testing.T) {
	// Assistant message with array of blocks.
	lines := []string{
		`{"type":"assistant","message":[{"type":"text","text":"Response part 1"},{"type":"text","text":"Response part 2"}]}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "Response part 1") || !strings.Contains(msgs[0].Content, "Response part 2") {
		t.Errorf("expected both parts in content, got %q", msgs[0].Content)
	}
}

func TestParseClaudeFileMixed(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Question?"}}`,
		`{"type":"assistant","message":"Answer."}`,
		`{"type":"user","message":{"role":"user","content":"Follow up."}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Errorf("unexpected roles: %q, %q, %q", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
}

func TestParseClaudeFileFromLine(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"First"}}`,
		`{"type":"assistant","message":"Second"}`,
		`{"type":"user","message":{"role":"user","content":"Third"}}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	// Skip the first line.
	msgs, err := ParseClaudeFile(path, 1)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (skipping first line), got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "Second" {
		t.Errorf("expected first message to be assistant/Second, got %q/%q", msgs[0].Role, msgs[0].Content)
	}
}

func TestParseClaudeFileSkipsInvalidLines(t *testing.T) {
	lines := []string{
		`not json at all`,
		`{"type":"other","message":"ignored"}`,
		`{"type":"user","message":{"role":"user","content":"Valid"}}`,
		``, // blank line
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected only 1 valid message, got %d", len(msgs))
	}
	if msgs[0].Content != "Valid" {
		t.Errorf("expected content %q, got %q", "Valid", msgs[0].Content)
	}
}

func TestParseClaudeFileEmptyContentSkipped(t *testing.T) {
	// Empty content in message should be skipped.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":""}}`,
		`{"type":"assistant","message":""}`,
	}
	path := writeTempFile(t, strings.Join(lines, "\n"))

	msgs, err := ParseClaudeFile(path, 0)
	if err != nil {
		t.Fatalf("ParseClaudeFile: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty content, got %d", len(msgs))
	}
}

func TestParseClaudeFileNonexistent(t *testing.T) {
	_, err := ParseClaudeFile(filepath.Join(t.TempDir(), "missing.jsonl"), 0)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseClaudeLineUserString(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":"hello"}}`
	msg, ok := parseClaudeLine(line)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if msg.Role != "user" || msg.Content != "hello" {
		t.Errorf("unexpected msg: role=%q content=%q", msg.Role, msg.Content)
	}
}

func TestParseClaudeLineUserArray(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`
	msg, ok := parseClaudeLine(line)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if !strings.Contains(msg.Content, "a") || !strings.Contains(msg.Content, "b") {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestParseClaudeLineAssistantString(t *testing.T) {
	line := `{"type":"assistant","message":"answer text"}`
	msg, ok := parseClaudeLine(line)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if msg.Role != "assistant" || msg.Content != "answer text" {
		t.Errorf("unexpected msg: role=%q content=%q", msg.Role, msg.Content)
	}
}

func TestParseClaudeLineAssistantArray(t *testing.T) {
	line := `{"type":"assistant","message":[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]}`
	msg, ok := parseClaudeLine(line)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if msg.Role != "assistant" {
		t.Errorf("expected role=assistant, got %q", msg.Role)
	}
	if !strings.Contains(msg.Content, "part1") || !strings.Contains(msg.Content, "part2") {
		t.Errorf("unexpected content: %q", msg.Content)
	}
}

func TestParseClaudeLineInvalidJSON(t *testing.T) {
	_, ok := parseClaudeLine("this is not json")
	if ok {
		t.Error("expected parse to fail for invalid JSON")
	}
}

func TestParseClaudeLineWrongType(t *testing.T) {
	_, ok := parseClaudeLine(`{"type":"other","message":"x"}`)
	if ok {
		t.Error("expected parse to fail for non user/assistant type")
	}
}

func TestParseClaudeLineUserNoTextBlock(t *testing.T) {
	// User with content that has no text blocks (e.g., only tool_use).
	line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_use","name":"bash"}]}}`
	_, ok := parseClaudeLine(line)
	if ok {
		t.Error("expected parse to fail when no text content present")
	}
}

func TestExtractTextContentString(t *testing.T) {
	raw, _ := json.Marshal("plain string")
	got := extractTextContent(raw)
	if got != "plain string" {
		t.Errorf("expected %q, got %q", "plain string", got)
	}
}

func TestExtractTextContentArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"one"},{"type":"text","text":"two"}]`)
	got := extractTextContent(raw)
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Errorf("expected both texts, got %q", got)
	}
}

func TestExtractTextContentEmptyArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image","source":{"type":"base64"}}]`)
	got := extractTextContent(raw)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractTextContentInvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{not valid`)
	got := extractTextContent(raw)
	if got != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"long", "hello world", 5, "hello..."},
		{"newline", "first line\nsecond line", 20, "first line"},
		{"empty", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
