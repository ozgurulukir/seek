package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/anthropics/seek/internal/config"
)

// OCRClient extracts text from an image using an OpenAI-compatible vision/OCR
// chat-completions endpoint. Any provider exposing a vision model works
// (DashScope qwen-vl-ocr, OpenAI gpt-4o, etc.).
type OCRClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewOCRClient creates an OCR client. baseURL is the OpenAI-compatible endpoint
// root (e.g. https://dashscope.aliyuncs.com/compatible-mode/v1); the chat
// completions path is appended automatically.
func NewOCRClient(baseURL, apiKey, model string) *OCRClient {
	return &OCRClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: config.DefaultEmbeddingTimeout},
	}
}

// ocrRequest is the OpenAI chat-completions request shape for vision.
type ocrRequest struct {
	Model       string       `json:"model"`
	Messages    []ocrMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
}

type ocrMessage struct {
	Role    string    `json:"role"`
	Content []ocrPart `json:"content"`
}

type ocrPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ocrImage `json:"image_url,omitempty"`
}

type ocrImage struct {
	URL string `json:"url"`
}

type ocrResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ExtractText runs OCR on a base64 data-URI image and returns the extracted text.
func (c *OCRClient) ExtractText(imageDataURI string) (string, error) {
	req := ocrRequest{
		Model: c.model,
		Messages: []ocrMessage{
			{
				Role: "user",
				Content: []ocrPart{
					{Type: "image_url", ImageURL: &ocrImage{URL: imageDataURI}},
					{Type: "text", Text: "Extract all text from this image verbatim. Return only the extracted text, no commentary."},
				},
			},
		},
		Temperature: 0.0,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ocr request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ocr status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var out ocrResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", fmt.Errorf("ocr api error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("ocr: no choices returned")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
