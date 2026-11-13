package bench

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ozgurulukir/seek/internal/chunk"
	"github.com/ozgurulukir/seek/internal/jsonl"
	"github.com/ozgurulukir/seek/internal/store"
)

// DefaultCollectionName is the collection the benchmark corpus is indexed
// under. It lives in the bench database (see DefaultDBPath in cmd), never in
// the user's main index.
const DefaultCollectionName = "benchmark-game"

// corpusDoc is one line of the search-benchmark-game corpus.json produced by
// corpus_transform.py: {"id": "<wikipedia url>", "text": "<normalized body>"}.
type corpusDoc struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// IngestStats summarizes one corpus ingestion run.
type IngestStats struct {
	Docs    int64
	Bytes   int64
	Skipped int64
	Elapsed time.Duration
}

// IngestOptions configures corpus ingestion.
type IngestOptions struct {
	DBPath         string
	CollectionName string // "" → DefaultCollectionName
	BatchSize      int    // 0 → 500
	MaxChunkSize   int    // 0 → chunk.DefaultMaxChunkSize
	Overlap        int    // 0 → chunk.DefaultOverlap
}

// Ingest reads search-benchmark-game corpus JSONL from in and indexes every
// document exactly the way seek indexes plain text: full body into the FTS
// table, body chunked by seek's default chunker into chunk rows. Documents
// have no title (the corpus ships only id+text), so the FTS title weight
// never applies. Progress is reported through the optional callback after
// every committed batch.
func Ingest(ctx context.Context, in io.Reader, opts IngestOptions, progress func(docs, bytes int64)) (*IngestStats, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	st, err := store.Open(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open bench store: %w", err)
	}
	defer st.Close()

	col, err := ensureCollection(st, opts.CollectionName)
	if err != nil {
		return nil, fmt.Errorf("ensure bench collection: %w", err)
	}

	scanner := jsonl.NewScanner(in)
	batch := make([]store.DocumentIndex, 0, batchSize)
	stats := &IngestStats{}
	start := time.Now()

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := st.UpsertAndReplaceIndexBatchContext(ctx, batch, batchSize); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var doc corpusDoc
		if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil {
			return nil, fmt.Errorf("decode corpus line %d: %w", stats.Docs+stats.Skipped+1, err)
		}
		if doc.ID == "" {
			stats.Skipped++
			continue
		}
		sum := sha256.Sum256([]byte(doc.Text))
		chunks := chunk.ChunkMarkdown(doc.Text, opts.MaxChunkSize, opts.Overlap)
		batch = append(batch, store.DocumentIndex{
			CollectionID: col.ID,
			Path:         doc.ID,
			Title:        "",
			ContentHash:  hex.EncodeToString(sum[:]),
			Mtime:        0,
			LineCount:    strings.Count(doc.Text, "\n") + 1,
			FTSContent:   doc.Text,
			Chunks:       toIndexChunks(chunks),
			FastFields:   nil,
		})
		stats.Docs++
		stats.Bytes += int64(len(doc.Text))
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(stats.Docs, stats.Bytes)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan corpus: %w", err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	stats.Elapsed = time.Since(start)
	return stats, nil
}

// ensureCollection returns the bench collection, creating it on first use so
// re-running the index step is idempotent.
func ensureCollection(st *store.Store, name string) (*store.Collection, error) {
	if name == "" {
		name = DefaultCollectionName
	}
	col, err := st.GetCollectionByName(name)
	if err == nil {
		return col, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return st.CreateCollection(name, store.CollectionTypeDocuments, "", "")
}

func toIndexChunks(chunks []chunk.Chunk) []store.IndexChunk {
	out := make([]store.IndexChunk, len(chunks))
	for i, c := range chunks {
		out[i] = store.IndexChunk{
			Seq:       c.Seq,
			Content:   c.Content,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			ChunkType: store.ChunkTypeText,
		}
	}
	return out
}
