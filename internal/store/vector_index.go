package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/hnsw"
	"github.com/google/renameio"
	"github.com/ozgurulukir/seek/internal/config"
)

// VectorResult represents a vector search result.
type VectorResult struct {
	ChunkID int64
	Score   float64
}

// VectorIndex abstracts the vector search backend (HNSW or linear scan).
type VectorIndex interface {
	Add(id int64, vector []float32) error
	Search(query []float32, k int) ([]VectorResult, error)
	Delete(id int64) error
	// Clear removes all vectors from the index.
	Clear() error
	Save(path string) error
	Load(path string) error
	Len() int
	Contains(id int64) bool
}

// VectorIndexFlusher is implemented by persistent indexes that need an
// explicit lifecycle flush before the owning Store closes.
type VectorIndexFlusher interface {
	Flush() error
}

// VectorIndexWarning exposes recoverable persistence problems to the runtime
// so a fresh rebuild is visible instead of becoming a silent fallback.
type VectorIndexWarning interface {
	Warning() string
}

// VectorIndexMetadata is implemented by persistent indexes whose manifest is
// tied to the current database vector generation.
type VectorIndexMetadata interface {
	ManifestGeneration() string
	SetManifestGeneration(string)
	SetWarning(string)
}

// VectorIndexRecovery is implemented by persistent indexes whose on-disk
// state could not be trusted and must be rebuilt from the Store.
type VectorIndexRecovery interface {
	NeedsRebuild() bool
	SetNeedsRebuild(bool)
}

// --- HNSW Implementation ---

type hnswIndex struct {
	graph             *hnsw.Graph[int64]
	dim               int
	m                 int
	efSearch          int
	mu                sync.RWMutex
	dirty             bool
	persistPath       string
	warning           string
	configFingerprint string
	generation        string
	needsRebuild      bool
}

type vectorManifest struct {
	Backend           string `json:"backend"`
	Version           int    `json:"version"`
	Dimension         int    `json:"dimension"`
	M                 int    `json:"m"`
	EFSearch          int    `json:"ef_search"`
	ConfigFingerprint string `json:"config_fingerprint,omitempty"`
	VectorGeneration  string `json:"vector_generation,omitempty"`
}

func newHNSWIndex(dim, m, efSearch int) (*hnswIndex, error) {
	g := hnsw.NewGraph[int64]()
	g.M = m
	if efSearch > 0 {
		g.EfSearch = efSearch
	} else {
		g.EfSearch = 50
	}
	g.Distance = hnsw.CosineDistance
	// Register the distance function for persistence (required by coder/hnsw)
	hnsw.RegisterDistanceFunc("cosine", hnsw.CosineDistance)
	return &hnswIndex{graph: g, dim: dim, m: m, efSearch: efSearch}, nil
}

func (h *hnswIndex) Add(id int64, vector []float32) error {
	if len(vector) != h.dim {
		return fmt.Errorf("vector dimension mismatch: got %d, want %d", len(vector), h.dim)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.graph.Add(hnsw.MakeNode(id, vector))
	h.dirty = true
	return nil
}

func (h *hnswIndex) Search(query []float32, k int) ([]VectorResult, error) {
	if len(query) != h.dim {
		return nil, fmt.Errorf("vector dimension mismatch: got %d, want %d", len(query), h.dim)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.graph.Len() == 0 {
		return nil, nil
	}
	nodes := h.graph.Search(query, k)
	out := make([]VectorResult, len(nodes))
	for i, n := range nodes {
		// coder/hnsw Search returns nodes ordered by distance (lower = more similar).
		// We compute the actual cosine distance to convert to similarity score.
		dist := hnsw.CosineDistance(query, n.Value)
		score := 1.0 - float64(dist)
		out[i] = VectorResult{ChunkID: n.Key, Score: score}
	}
	return out, nil
}

func (h *hnswIndex) Delete(id int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.graph.Delete(id)
	h.dirty = true
	return nil
}

// Clear rebuilds an empty graph, preserving M/EfSearch/distance config.
// coder/hnsw has no bulk-clear API, so we allocate a fresh graph.
func (h *hnswIndex) Clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := hnsw.NewGraph[int64]()
	g.M = h.m
	if h.efSearch > 0 {
		g.EfSearch = h.efSearch
	} else {
		g.EfSearch = 50
	}
	g.Distance = hnsw.CosineDistance
	h.graph = g
	h.dirty = true
	return nil
}

func (h *hnswIndex) Save(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.dirty {
		return nil
	}
	// Write to a temp file first, then atomically replace to avoid
	// corrupting the index if the process crashes mid-write.
	dir := filepath.Dir(path)
	tmp, err := renameio.TempFile(dir, path)
	if err != nil {
		return fmt.Errorf("create temp index file: %w", err)
	}
	defer tmp.Cleanup()

	if err := h.graph.Export(tmp); err != nil {
		return fmt.Errorf("export hnsw index: %w", err)
	}
	if err := tmp.CloseAtomicallyReplace(); err != nil {
		return fmt.Errorf("atomically replace hnsw index: %w", err)
	}
	manifest, err := json.Marshal(vectorManifest{
		Backend:           "hnsw",
		Version:           2,
		Dimension:         h.dim,
		M:                 h.m,
		EFSearch:          h.efSearch,
		ConfigFingerprint: h.configFingerprint,
		VectorGeneration:  h.generation,
	})
	if err != nil {
		return fmt.Errorf("encode hnsw manifest: %w", err)
	}
	if err := renameio.WriteFile(path+".meta.json", manifest, 0600); err != nil {
		return fmt.Errorf("write hnsw manifest: %w", err)
	}
	h.dirty = false
	h.persistPath = path
	return nil
}

func (h *hnswIndex) Load(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	manifestBytes, err := os.ReadFile(path + ".meta.json")
	if err != nil {
		return fmt.Errorf("read hnsw manifest: %w", err)
	}
	var manifest vectorManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("parse hnsw manifest: %w", err)
	}
	if manifest.Backend != "hnsw" || manifest.Version != 2 || manifest.Dimension != h.dim || manifest.M != h.m || manifest.EFSearch != h.efSearch || (h.configFingerprint != "" && manifest.ConfigFingerprint != h.configFingerprint) {
		return fmt.Errorf("hnsw manifest mismatch: got backend=%q version=%d dimension=%d m=%d ef_search=%d fingerprint=%q, want backend=hnsw version=2 dimension=%d m=%d ef_search=%d fingerprint=%q", manifest.Backend, manifest.Version, manifest.Dimension, manifest.M, manifest.EFSearch, manifest.ConfigFingerprint, h.dim, h.m, h.efSearch, h.configFingerprint)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := h.graph.Import(bufio.NewReader(f)); err != nil {
		return err
	}
	h.dirty = false
	h.persistPath = path
	h.generation = manifest.VectorGeneration
	return nil
}

// Flush persists a dirty HNSW graph to the configured path. An in-memory test
// index without a persistence path remains a valid non-persistent index.
func (h *hnswIndex) Flush() error {
	h.mu.RLock()
	path := h.persistPath
	h.mu.RUnlock()
	if path == "" {
		return nil
	}
	return h.Save(path)
}

func (h *hnswIndex) NeedsRebuild() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.needsRebuild
}

func (h *hnswIndex) SetNeedsRebuild(value bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.needsRebuild = value
}

func (h *hnswIndex) Warning() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.warning
}

func (h *hnswIndex) SetWarning(warning string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.warning = warning
}

func (h *hnswIndex) SetManifestGeneration(generation string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.generation != generation {
		h.generation = generation
		h.dirty = true
	}
}

func (h *hnswIndex) ManifestGeneration() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.generation
}

func (h *hnswIndex) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graph.Len()
}

func (h *hnswIndex) Contains(id int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.graph.Lookup(id)
	return ok
}

// --- Linear Scan Fallback ---

type linearIndex struct {
	vectors map[int64][]float32
	dim     int
	mu      sync.RWMutex
}

func newLinearIndex(dim int) *linearIndex {
	return &linearIndex{vectors: make(map[int64][]float32), dim: dim}
}

func (l *linearIndex) Add(id int64, vector []float32) error {
	if len(vector) != l.dim {
		return fmt.Errorf("vector dimension mismatch: got %d, want %d", len(vector), l.dim)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Copy vector to avoid external mutation
	cp := make([]float32, len(vector))
	copy(cp, vector)
	l.vectors[id] = cp
	return nil
}

func (l *linearIndex) Search(query []float32, k int) ([]VectorResult, error) {
	if len(query) != l.dim {
		return nil, fmt.Errorf("vector dimension mismatch: got %d, want %d", len(query), l.dim)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	type scored struct {
		id    int64
		score float64
	}
	var all []scored
	for id, vec := range l.vectors {
		sim := cosineSimilarity(query, vec)
		all = append(all, scored{id: id, score: sim})
	}
	// Sort descending by score
	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})
	if len(all) > k {
		all = all[:k]
	}
	out := make([]VectorResult, len(all))
	for i, s := range all {
		out[i] = VectorResult{ChunkID: s.id, Score: s.score}
	}
	return out, nil
}

func (l *linearIndex) Delete(id int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.vectors, id)
	return nil
}

func (l *linearIndex) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.vectors = make(map[int64][]float32)
	return nil
}

func (l *linearIndex) Save(path string) error {
	return nil // linear scan doesn't persist
}

func (l *linearIndex) Load(path string) error {
	return nil // linear scan doesn't persist
}

func (l *linearIndex) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.vectors)
}

func (l *linearIndex) Contains(id int64) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.vectors[id]
	return ok
}

// --- Vector Index Factory ---

// NewVectorIndex creates a VectorIndex based on the config.
// backend: "hnsw" or "linear"
func NewVectorIndex(cfg *config.AppConfig) (VectorIndex, error) {
	dim := cfg.Config.Embedding.Dimensions
	if dim <= 0 {
		dim = config.DefaultEmbeddingDimensions
	}
	backend := cfg.Config.VectorIndex.Backend
	if backend == "" {
		backend = config.DefaultVectorIndexBackend
	}
	switch backend {
	case "hnsw":
		m := cfg.Config.VectorIndex.HNSW.M
		if m <= 0 {
			m = config.DefaultHNSWM
		}
		efSearch := cfg.Config.VectorIndex.HNSW.EFSearch
		if efSearch <= 0 {
			efSearch = config.DefaultHNSEFSearch
		}
		idx, err := newHNSWIndex(dim, m, efSearch)
		if err != nil {
			return nil, err
		}
		idx.configFingerprint = vectorConfigFingerprint(cfg)
		// Try to load existing index
		path := cfg.Config.VectorIndex.HNSW.PersistPath
		if path != "" {
			idx.persistPath = path
			if _, err := os.Stat(path); err == nil {
				if err := idx.Load(path); err != nil {
					// Corrupt, legacy, or dimension-mismatched index — fall back
					// to a fresh HNSW, but retain a warning for the runtime.
					warning := fmt.Sprintf("vector index %q was rebuilt: %v", path, err)
					idx, err = newHNSWIndex(dim, m, efSearch)
					if err != nil {
						return nil, fmt.Errorf("rebuild hnsw index: %w", err)
					}
					idx.configFingerprint = vectorConfigFingerprint(cfg)
					idx.persistPath = path
					idx.warning = warning
					idx.needsRebuild = true
				}
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect vector index %q: %w", path, err)
			} else {
				// No persisted graph is a normal first-run state, but the Store
				// may already contain embeddings (for example after upgrading).
				// Let recovery materialize the graph from SQLite.
				idx.needsRebuild = true
			}
		}
		return idx, nil
	case "linear":
		return newLinearIndex(dim), nil
	default:
		return nil, fmt.Errorf("unsupported vector index backend %q", backend)
	}
}

func vectorConfigFingerprint(cfg *config.AppConfig) string {
	if cfg == nil {
		return ""
	}
	e := cfg.Config.Embedding
	h := sha256.Sum256([]byte(strings.Join([]string{
		e.BaseURL,
		e.Model,
		strconv.Itoa(e.Dimensions),
		e.VLBaseURL,
		strconv.FormatBool(e.Multimodal),
		e.TaskPrefix.Query,
		e.TaskPrefix.Document,
		strconv.FormatBool(e.TaskPrefix.DisableAutoDetect),
	}, "\x00")))
	return hex.EncodeToString(h[:])
}
