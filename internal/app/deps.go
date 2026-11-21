package app

import (
	"fmt"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/pipeline"
	"github.com/ozgurulukir/seek/internal/store"
)

// depView is the nil-checkable dependency surface shared by the app
// services. Each service hands its filled view to the shared guard so the
// check-and-format logic exists exactly once (audit A6) instead of two
// "mirroring" copies that can drift. Unknown names are reported as missing,
// matching the historical per-service switches.
type depView struct {
	store    *store.Store
	cfg      *config.AppConfig
	pipeline *pipeline.Pipeline
	indexer  *indexer.Indexer
}

func (d depView) missing(names []string) []string {
	var out []string
	for _, name := range names {
		var ok bool
		switch name {
		case "store":
			ok = d.store != nil
		case "cfg":
			ok = d.cfg != nil
		case "pipeline":
			ok = d.pipeline != nil
		case "indexer":
			ok = d.indexer != nil
		}
		if !ok {
			out = append(out, name)
		}
	}
	return out
}

// notConfigured is the uniform nil-dependency error (#191): the nil-receiver
// form is "<service>: not configured"; missing dependencies append
// " (missing <names>)" comma-joined in call order — the shape pinned by
// guards_test.go.
func notConfigured(service string, missing []string) error {
	if len(missing) > 0 {
		return fmt.Errorf("%s: not configured (missing %s)", service, strings.Join(missing, ", "))
	}
	return fmt.Errorf("%s: not configured", service)
}
