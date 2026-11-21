package indexer

import (
	"fmt"
	"sync"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/extractor"
	"github.com/ozgurulukir/seek/internal/store"
)

// ExtractorResolver owns extractor selection and caching. Indexer handlers
// depend on this narrow contract instead of constructing builtin/xberg/OCR
// clients as part of their format logic.
type ExtractorResolver interface {
	Resolve(*store.Collection) (extractor.Extractor, error)
}

type configExtractorResolver struct {
	cfg   *config.AppConfig
	mu    sync.Mutex
	cache map[string]extractor.Extractor
}

func NewConfigExtractorResolver(cfg *config.AppConfig) ExtractorResolver {
	return &configExtractorResolver{cfg: cfg, cache: make(map[string]extractor.Extractor)}
}

// overrideResolver always yields the explicitly selected backend, ignoring
// the collection — the --backend flag path. The extractor is constructed
// lazily on first Resolve and cached, so an add does not pay the
// (potentially network-probing) construction up front and a temporarily down
// external service still lets the collection row be created (the sync then
// surfaces the failure).
type overrideResolver struct {
	cfg     *config.AppConfig
	backend string
	mu      sync.Mutex
	cached  extractor.Extractor
}

// NewOverrideResolver returns a resolver that always resolves the named
// backend, taking precedence over both the per-collection backend and the
// config default (the --backend flag path). Construction fails only on a nil
// config; deterministic configuration conflicts (xberg under
// privacy.offline_only) are surfaced lazily at first Resolve, and callers
// that must decide before persisting a collection row pre-flight them via
// BackendRefusal.
func NewOverrideResolver(cfg *config.AppConfig, backend string) (ExtractorResolver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("extractor resolver: nil config")
	}
	return &overrideResolver{cfg: cfg, backend: backend}, nil
}

func (r *overrideResolver) Resolve(*store.Collection) (extractor.Extractor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil {
		return r.cached, nil
	}
	ext, err := NewExtractor(r.cfg, r.backend)
	if err != nil {
		return nil, err
	}
	r.cached = ext
	return ext, nil
}

func (r *configExtractorResolver) Resolve(col *store.Collection) (extractor.Extractor, error) {
	if r.cfg == nil {
		return nil, fmt.Errorf("extractor resolver: nil config")
	}
	backend := ""
	if col != nil {
		backend = col.Backend
	}
	if backend == "" {
		backend = r.cfg.Config.Extractor.Backend
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.cache[backend]; ok {
		return cached, nil
	}
	ext, err := NewExtractor(r.cfg, backend)
	if err != nil {
		return nil, err
	}
	r.cache[backend] = ext
	return ext, nil
}
