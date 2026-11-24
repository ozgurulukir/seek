package embed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
)

// DefaultVLEndpoint is the DashScope multimodal embedding endpoint, used when
// no custom VL base URL is configured.
const DefaultVLEndpoint = config.DefaultVLBaseURL

const (
	vlMaxContents   = config.DefaultVLMaxContents // max content elements per request
	vlMaxImages     = config.DefaultVLMaxImages   // max images per request
	maxImageWorkers = 8                           // max concurrent image readers

	// maxImageBytes caps a single image file before it is base64-encoded into
	// a data URI (100 MiB, matching the upstream skip threshold). Without it an
	// oversized file would be read whole and retained as an even larger string.
	maxImageBytes = 100 << 20 // 100 MiB
	// maxInFlightImageBytes bounds the bytes being encoded concurrently across
	// workers. maxInFlightImages slots, each holding at most one image of at
	// most maxImageBytes, keep that bound at ~4x the per-image cap.
	maxInFlightImageBytes = 4 * maxImageBytes
	maxInFlightImages     = maxInFlightImageBytes / maxImageBytes // 4
)

// EmbedItem represents a single item to embed — either text-only or image+context.
type EmbedItem struct {
	Text     string // required for text chunks
	ImageURI string // optional: "data:image/png;base64,..." for image chunks
}

// VLClient is a client for a multimodal embedding API (vision-language models,
// e.g. DashScope qwen3-vl-embedding or any compatible endpoint).
type VLClient struct {
	apiKey     string
	model      string
	dimensions int
	endpoint   string
	taskPrefix TaskPrefix
	http       *http.Client
	// refuseNetwork marks a client that must not send requests to a
	// non-loopback endpoint (search.privacy.offline_only). Requests fail fast,
	// before any chunk, query, or image text leaves the machine. This mirrors
	// Client.offline and provides defense-in-depth beyond the factory check.
	refuseNetwork bool
}

// NewVLClient creates a new multimodal embedding client. If endpoint is empty,
// DefaultVLEndpoint (DashScope) is used. The task prefix is applied to query
// texts (EmbedText) and document texts (EmbedBatch) like the text Client.
func NewVLClient(apiKey, model string, dimensions int, endpoint string, taskPrefix TaskPrefix) *VLClient {
	return newVLClient(apiKey, model, dimensions, endpoint, taskPrefix, false)
}

func newVLClient(apiKey, model string, dimensions int, endpoint string, taskPrefix TaskPrefix, offline bool) *VLClient {
	if endpoint == "" {
		endpoint = DefaultVLEndpoint
	}
	// Offline-only mode must never egress to a non-loopback host. The factory
	// enforces this too, but a direct newVLClient call could otherwise bypass
	// it, so refuse here as well (defense-in-depth).
	refuseNetwork := offline && !config.IsNumericLoopbackURL(endpoint)
	return &VLClient{
		apiKey:        apiKey,
		model:         model,
		dimensions:    dimensions,
		endpoint:      endpoint,
		taskPrefix:    taskPrefix,
		http:          newHTTPClient(config.DefaultVLTimeout, offline, endpoint),
		refuseNetwork: refuseNetwork,
	}
}

// vlRequest is the DashScope multimodal embedding request format.
type vlRequest struct {
	Model      string   `json:"model"`
	Input      vlInput  `json:"input"`
	Parameters vlParams `json:"parameters"`
}

type vlInput struct {
	Contents []vlContent `json:"contents"`
}

type vlContent map[string]string // {"text": "..."} or {"image": "data:..."} or {"text": "...", "image": "data:..."}

type vlParams struct {
	Dimension int `json:"dimension"`
}

// vlResponse is the DashScope multimodal embedding response format.
type vlResponse struct {
	Output struct {
		Embeddings []indexedEmbedding `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// EmbedTextContext embeds a search query via the VL API, prepending the
// configured query task prefix.
func (c *VLClient) EmbedTextContext(ctx context.Context, text string) ([]float32, error) {
	results, err := c.embedRawContext(ctx, []EmbedItem{{Text: c.taskPrefix.applyQuery(text)}})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedImage embeds an image with optional context text.
func (c *VLClient) EmbedImage(imageDataURI string, context string) ([]float32, error) {
	item := EmbedItem{
		Text:     context,
		ImageURI: imageDataURI,
	}
	results, err := c.EmbedBatch([]EmbedItem{item})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedBatch embeds document items, prepending the configured document task
// prefix to each item's text (image context included). It automatically
// splits into sub-batches respecting the 20-content and 5-image limits per
// request.
func (c *VLClient) EmbedBatch(items []EmbedItem) ([][]float32, error) {
	return c.EmbedBatchContext(context.Background(), items)
}

func (c *VLClient) EmbedBatchContext(ctx context.Context, items []EmbedItem) ([][]float32, error) {
	if c.taskPrefix.Document != "" {
		prefixed := make([]EmbedItem, len(items))
		copy(prefixed, items)
		for i := range prefixed {
			if (prefixed[i].Text != "" || prefixed[i].ImageURI == "") && !strings.HasPrefix(prefixed[i].Text, c.taskPrefix.Document) {
				prefixed[i].Text = c.taskPrefix.Document + prefixed[i].Text
			}
		}
		items = prefixed
	}
	return c.embedRawContext(ctx, items)
}

// embedRaw batches and sends items without any task-prefix transformation.
func (c *VLClient) embedRaw(items []EmbedItem) ([][]float32, error) {
	return c.embedRawContext(context.Background(), items)
}

func (c *VLClient) embedRawContext(ctx context.Context, items []EmbedItem) ([][]float32, error) {
	if len(items) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(items))

	// Split items into batches respecting limits
	batches := splitIntoBatches(items)

	globalOffset := 0
	for _, batch := range batches {
		embeddings, err := c.doRequestContext(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("vl batch at offset %d: %w", globalOffset, err)
		}

		for i, emb := range embeddings {
			if globalOffset+i < len(results) {
				results[globalOffset+i] = emb
			}
		}
		globalOffset += len(batch)
	}

	return results, nil
}

// splitIntoBatches splits items into batches respecting vlMaxContents and vlMaxImages limits.
func splitIntoBatches(items []EmbedItem) [][]EmbedItem {
	var batches [][]EmbedItem
	var currentBatch []EmbedItem
	imageCount := 0

	for _, item := range items {
		isImage := item.ImageURI != ""

		// Check if adding this item would exceed limits
		if len(currentBatch) >= vlMaxContents || (isImage && imageCount >= vlMaxImages) {
			if len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
			}
			currentBatch = nil
			imageCount = 0
		}

		currentBatch = append(currentBatch, item)
		if isImage {
			imageCount++
		}
	}

	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// doRequest sends a single batch request to the VL API.
func (c *VLClient) doRequest(items []EmbedItem) ([][]float32, error) {
	return c.doRequestContext(context.Background(), items)
}

func (c *VLClient) doRequestContext(ctx context.Context, items []EmbedItem) ([][]float32, error) {
	if c.refuseNetwork {
		return nil, fmt.Errorf("offline_only is enabled: refusing to send request to %q", c.endpoint)
	}

	var contents []vlContent

	for _, item := range items {
		content := make(vlContent)
		if item.ImageURI != "" {
			content["image"] = item.ImageURI
			if item.Text != "" {
				content["text"] = item.Text
			}
		} else {
			text := item.Text
			if text == "" {
				text = " " // VL API requires non-empty text
			}
			content["text"] = text
		}
		contents = append(contents, content)
	}

	req := vlRequest{
		Model: c.model,
		Input: vlInput{Contents: contents},
		Parameters: vlParams{
			Dimension: c.dimensions,
		},
	}

	var vlResp vlResponse
	if err := postJSON(ctx, c.http, c.endpoint, c.apiKey, "vl", req, &vlResp); err != nil {
		return nil, err
	}

	if vlResp.Code != "" {
		return nil, fmt.Errorf("vl API error [%s]: %s", vlResp.Code, vlResp.Message)
	}

	return reorderEmbeddings("vl", vlResp.Output.Embeddings, len(items))
}

// ImageToDataURI reads an image file and returns a data URI string. Files
// larger than maxImageBytes are rejected before being read, and the read is
// itself bounded so a file that grows between the stat and the open cannot
// bypass the cap.
func ImageToDataURI(imagePath string, mediaType string) (string, error) {
	info, err := os.Stat(imagePath)
	if err != nil {
		return "", fmt.Errorf("stat image %s: %w", imagePath, err)
	}
	if info.Size() > maxImageBytes {
		return "", fmt.Errorf("image %s exceeds %d MiB limit (%d bytes)", imagePath, maxImageBytes>>20, info.Size())
	}
	f, err := os.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("open image %s: %w", imagePath, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", imagePath, err)
	}
	if len(data) > maxImageBytes {
		return "", fmt.Errorf("image %s exceeds %d MiB limit", imagePath, maxImageBytes>>20)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mediaType, b64), nil
}

// readImageWithBudget encodes one image while holding a slot from the
// in-flight byte budget. At most maxInFlightImages images (each at most
// maxImageBytes) are encoded at once, so in-flight encoded bytes stay bounded
// to maxInFlightImageBytes. The slot is released as soon as the data URI is
// built; a whole batch retains at most batchSize such URIs.
func readImageWithBudget(ctx context.Context, imagePath, mediaType string, slots chan struct{}) (string, error) {
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-slots }()
	return ImageToDataURI(imagePath, mediaType)
}

// ImagePathToMediaType infers media type from file extension.
func ImagePathToMediaType(path string) string {
	ext := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = path[i+1:]
			break
		}
	}
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// EmbedTextsInBatchesContext embeds text strings in batches with rate-limiting pauses.
func (c *VLClient) EmbedTextsInBatchesContext(ctx context.Context, texts []string, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32) error) (int, error) {
	if batchSize <= 0 {
		batchSize = vlMaxContents
	}
	updated := 0
	var firstErr error
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		items := make([]EmbedItem, end-i)
		for j := i; j < end; j++ {
			items[j-i] = EmbedItem{Text: texts[j]}
		}

		if err := ctx.Err(); err != nil {
			return updated, err
		}
		embeddings, err := c.EmbedBatchContext(ctx, items)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("text batch %d-%d: %w", i, end, err)
			}
			continue
		}

		if onBatch != nil {
			if err := onBatch(i, embeddings); err != nil {
				return updated, err
			}
		}
		for _, emb := range embeddings {
			if emb != nil {
				updated++
			}
		}

		if end < len(texts) && pause > 0 {
			if err := sleepContext(ctx, pause); err != nil {
				return updated, err
			}
		}
	}
	return updated, firstErr
}

// ImageBatchItem pairs an image path with optional context text.
type ImageBatchItem struct {
	ImagePath string
	Text      string
}

// EmbedImagesInBatchesContext reads images concurrently, formats data URIs, and embeds them in batches.
func (c *VLClient) EmbedImagesInBatchesContext(ctx context.Context, items []ImageBatchItem, batchSize int, pause time.Duration, onBatch func(batchStart int, embeddings [][]float32, validIndices []int) error) (int, error) {
	if batchSize <= 0 {
		batchSize = vlMaxImages
	}
	// A batch retains one data URI per item until the embed call returns, so
	// cap the batch size at the in-flight image budget as well: worst-case
	// retained image bytes stay at maxInFlightImageBytes (4 x 100 MiB).
	if batchSize > maxInFlightImages {
		batchSize = maxInFlightImages
	}
	updated := 0
	var firstErr error
	var firstReadErr error
	readFailures := 0
	// Slots sized so that concurrent encoding never holds more than
	// maxInFlightImageBytes of image data at once (see readImageWithBudget).
	// The batch-size cap above bounds the bytes retained for the whole batch.
	imageSlots := make(chan struct{}, maxInFlightImages)
	for i := 0; i < len(items); i += batchSize {
		if err := ctx.Err(); err != nil {
			return updated, err
		}
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		type readResult struct {
			dataURI string
			err     error
		}
		batchLen := end - i
		results := make([]readResult, batchLen)

		numWorkers := batchLen
		if numWorkers > maxImageWorkers {
			numWorkers = maxImageWorkers
		}

		if numWorkers <= 1 {
			for j := i; j < end; j++ {
				if ctx.Err() != nil {
					break
				}
				mediaType := ImagePathToMediaType(items[j].ImagePath)
				dataURI, err := readImageWithBudget(ctx, items[j].ImagePath, mediaType, imageSlots)
				results[j-i] = readResult{dataURI, err}
			}
		} else {
			var wg sync.WaitGroup
			wg.Add(numWorkers)
			var nextIdx int32 = int32(i)
			for w := 0; w < numWorkers; w++ {
				go func() {
					defer wg.Done()
					for {
						if ctx.Err() != nil {
							break
						}
						j := int(atomic.AddInt32(&nextIdx, 1) - 1)
						if j >= end {
							break
						}
						item := items[j]
						mediaType := ImagePathToMediaType(item.ImagePath)
						dataURI, err := readImageWithBudget(ctx, item.ImagePath, mediaType, imageSlots)
						results[j-i] = readResult{dataURI, err}
					}
				}()
			}
			wg.Wait()
		}

		embedItems := make([]EmbedItem, 0, end-i)
		validIndices := make([]int, 0, end-i)

		for j := i; j < end; j++ {
			item := items[j]
			res := results[j-i]
			if res.err != nil {
				// Count and surface read failures: silently dropping them
				// made permanently unembeddable images invisible (apparent
				// success with only a low count), masking disk/permission
				// errors (review 2026-09-17 M15).
				readFailures++
				if firstReadErr == nil {
					firstReadErr = fmt.Errorf("read image %q: %w", item.ImagePath, res.err)
				}
				continue
			}
			embedItems = append(embedItems, EmbedItem{
				Text:     item.Text,
				ImageURI: res.dataURI,
			})
			validIndices = append(validIndices, j)
		}

		if len(embedItems) == 0 {
			continue
		}

		embeddings, err := c.EmbedBatchContext(ctx, embedItems)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("image batch %d-%d: %w", i, end, err)
			}
			continue
		}

		if onBatch != nil {
			if err := onBatch(i, embeddings, validIndices); err != nil {
				return updated, err
			}
		}
		for _, emb := range embeddings {
			if emb != nil {
				updated++
			}
		}

		if end < len(items) && pause > 0 {
			if err := sleepContext(ctx, pause); err != nil {
				return updated, err
			}
		}
	}
	if readFailures > 0 {
		readErr := fmt.Errorf("%d of %d image(s) could not be read and were skipped: %w", readFailures, len(items), firstReadErr)
		return updated, errors.Join(firstErr, readErr)
	}
	return updated, firstErr
}
