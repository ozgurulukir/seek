package embed

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
)

// OCRClient extracts text from an image using an OpenAI-compatible vision/OCR
// chat-completions endpoint. Any provider exposing a vision model works
// (DashScope qwen-vl-ocr, OpenAI gpt-4o, etc.).
type OCRClient struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
}

// NewOCRClient creates an OCR client. baseURL is the OpenAI-compatible endpoint
// root (e.g. https://dashscope.aliyuncs.com/compatible-mode/v1); the chat
// completions path is appended automatically.
func NewOCRClient(baseURL, apiKey, model string, maxTokens int) *OCRClient {
	if maxTokens <= 0 {
		maxTokens = config.DefaultOCRMaxTokens
	}
	httpClient := newHTTPClient(config.DefaultEmbeddingTimeout, false, baseURL)
	// OCR requests may carry private document images. Never follow a redirect
	// automatically — for remote endpoints too, not just the loopback
	// isolation the shared policy applies: a 3xx must not cause the payload
	// to be re-sent to a different host.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &OCRClient{
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		http:      httpClient,
	}
}

// ocrRequest is the OpenAI chat-completions request shape for vision.
type ocrRequest struct {
	Model       string       `json:"model"`
	Messages    []ocrMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
	Stream      bool         `json:"stream"`
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
	return c.ExtractTextWithContext(context.Background(), imageDataURI)
}

// ExtractTextWithContext runs OCR on a base64 data-URI image with a context and returns the extracted text.
func (c *OCRClient) ExtractTextWithContext(ctx context.Context, imageDataURI string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
		MaxTokens:   c.maxTokens,
		Stream:      false,
	}

	var out ocrResponse
	url := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	if err := postJSON(ctx, c.http, url, c.apiKey, "ocr", req, &out); err != nil {
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
