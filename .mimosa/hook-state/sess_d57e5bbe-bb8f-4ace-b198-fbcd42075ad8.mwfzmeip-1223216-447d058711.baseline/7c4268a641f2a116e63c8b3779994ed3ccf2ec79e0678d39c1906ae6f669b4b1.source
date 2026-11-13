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
