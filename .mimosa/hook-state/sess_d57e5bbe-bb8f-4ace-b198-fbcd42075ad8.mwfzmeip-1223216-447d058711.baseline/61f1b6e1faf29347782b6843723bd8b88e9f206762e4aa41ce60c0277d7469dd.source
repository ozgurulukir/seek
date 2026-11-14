package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// ConfigFingerprint returns the vector-space fingerprint the persisted
	// graph was built for (a cache copy of the SQLite embedding profile).
	ConfigFingerprint() string
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
	// Normalize the search width once so the runtime value, the persisted
	// manifest, and the Load mismatch check all agree. Persisting a raw <=0
	// value made default-config round-trips declare a good graph "mismatched"
	// and trigger an unnecessary rebuild (review 2026-09-17 L2).
	if efSearch <= 0 {
		efSearch = 50
	}
	g := hnsw.NewGraph[int64]()
	g.M = m
	g.EfSearch = efSearch
	g.Distance = hnsw.CosineDistance
	// "cosine" is pre-registered in coder/hnsw's distanceFuncs map; calling
	// RegisterDistanceFunc here re-wrote that shared package map without
	// synchronization while graph.Export iterates it during concurrent
	// flushes — a data race (review 2026-09-17 L1).
	return &hnswIndex{graph: g, dim: dim, m: m, efSearch: efSearch}, nil
}

func (h *hnswIndex) Add(id int64, vector []float32) error {
	if len(vector) != h.dim {
		return fmt.Errorf("vector dimension mismatch: got %d, want %d", len(vector), h.dim)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// coder/hnsw v0.6.1 panics while replacing an existing key. Reject the
	// duplicate here; Store.UpdateChunkEmbeddingContext handles replacements
	// through an atomic full rebuild instead of mutating graph connectivity.
	if _, exists := h.graph.Lookup(id); exists {
		return fmt.Errorf("vector id %d already exists", id)
	}
	h.graph.Add(hnsw.MakeNode(id, vector))
	h.dirty = true
	return nil
}

// emptyVectorIndexLike creates an empty index with the same backend and
// runtime settings. Full rebuilds populate this candidate off to the side and
// publish it only after every persisted embedding has been accepted, so a
// failed rebuild cannot replace a usable graph with a partial one.
func emptyVectorIndexLike(current VectorIndex) (VectorIndex, bool, error) {
	switch index := current.(type) {
	case *hnswIndex:
		replacement, err := newHNSWIndex(index.dim, index.m, index.efSearch)
		if err != nil {
			return nil, true, err
		}
		replacement.persistPath = index.persistPath
		replacement.configFingerprint = index.configFingerprint
		return replacement, true, nil
	case *linearIndex:
		return newLinearIndex(index.dim), true, nil
	default:
		return nil, false, nil
	}
}

// publishVectorIndexRebuild atomically swaps a completed candidate into the
// existing index object. Keeping the object identity preserves recovery
// metadata, warnings, persistence configuration, and references held by the
// runtime while ensuring readers never observe a partially rebuilt graph.
func publishVectorIndexRebuild(current, candidate VectorIndex) error {
	switch currentIndex := current.(type) {
	case *hnswIndex:
		candidateIndex, ok := candidate.(*hnswIndex)
		if !ok {
			return fmt.Errorf("vector rebuild type mismatch: current %T, candidate %T", current, candidate)
		}
		candidateIndex.mu.RLock()
		graph := candidateIndex.graph
		candidateIndex.mu.RUnlock()
		currentIndex.mu.Lock()
		currentIndex.graph = graph
		currentIndex.dirty = true
		currentIndex.mu.Unlock()
		return nil
	case *linearIndex:
		candidateIndex, ok := candidate.(*linearIndex)
		if !ok {
			return fmt.Errorf("vector rebuild type mismatch: current %T, candidate %T", current, candidate)
		}
		candidateIndex.mu.RLock()
		vectors := candidateIndex.vectors
		candidateIndex.mu.RUnlock()
		currentIndex.mu.Lock()
		currentIndex.vectors = vectors
		currentIndex.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("unsupported vector rebuild type %T", current)
	}
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

func (h *hnswIndex) ConfigFingerprint() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.configFingerprint
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

// vectorConfigFingerprint is the HNSW manifest's cache copy of the SQLite
// embedding profile fingerprint. It is computed over the semantic vector-space
// identity (provider kind, model, dimensions, task prefixes, normalization) and
// deliberately excludes the endpoint host so a provider URL change does not
// invalidate an otherwise identical vector space. The SQLite profile record is
// the source of truth; this value is cross-validated against it at runtime.
func vectorConfigFingerprint(cfg *config.AppConfig) string {
	if cfg == nil {
		return ""
	}
	return ProfileFromConfig(cfg).ComputeFingerprint()
}
