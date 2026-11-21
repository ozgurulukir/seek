package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/semantic"
	"github.com/ozgurulukir/seek/internal/store"
)

// semanticStateRecorder owns every write of the documents semantic
// fingerprint/status columns, for both consumers that persist enrichment
// state: the sync path (RecordSyncOutcome, reached through
// Indexer.recordSemanticSyncState) and the collection-scoped backfill
// (RecordBackfillCurrent / RecordBackfillError, reached through
// BackfillSemantic). It also owns the enrichment identity basis behind those
// writes: one lazy, memoized resolution of the service identity + capability
// set + schema version, so the sync and backfill paths share a single
// decision table instead of two differently-implemented basis caches.
//
// The write-side decision table (plan §3.2):
//
//   - sync, enrichment succeeded (non-nil fields): Current with the full
//     desired fingerprint (identity + per-doc content hash).
//   - sync, enrichment failed or not attempted while the capability is
//     enabled (nil fields): Stale with an EMPTY identity basis so the stale
//     query re-selects the document and the next backfill retries it.
//   - sync, capability disabled (nil fields): nothing recorded.
//   - sync, provider exists but /health failed (half-up service): nothing
//     recorded; the failure is warned once per recorder instance and the
//     health check is retried on every subsequent call.
//   - backfill, enrichment succeeded or no readable text: Current with the
//     full desired fingerprint; nil fields preserve prior fast fields, a
//     non-nil empty map explicitly clears them (store nil-map semantics —
//     see store.UpdateSemanticState store/semantic.go:186-195 for the
//     canonical statement).
//   - backfill, enrichment failed: Error with the full desired fingerprint,
//     nil fields preserve prior fast fields; the stale query re-selects
//     semantic_status='error' documents so the next pass retries.
//
// Fingerprint byte-stability: the stale-empty write must stay ("", ""),
// Compute() is untouched, and the identity is computed before the per-doc
// content hash is layered on top — the recorder centralizes all three
// invariants that were previously hand-assembled in five UpdateSemanticState
// call sites with the contract prose duplicated across ~10 comment sites.
type semanticStateRecorder struct {
	db       *store.Store
	cfg      *config.AppConfig
	provider func() semantic.Provider
	// warnf reports the sync-path resolve failure once per recorder
	// instance; nil discards (the backfill warns through its own pass log).
	warnf func(format string, v ...any)
	// mu guards the memoized basis and the warn-once flag. Sync runs
	// sequentially today, but the recorder is shared across documents, so
	// the same lock discipline as the Indexer applies.
	mu          sync.Mutex
	basis       store.SemanticFingerprint
	basisSet    bool
	basisFailed bool
}

func newSemanticStateRecorder(db *store.Store, cfg *config.AppConfig, provider func() semantic.Provider, warnf func(format string, v ...any)) *semanticStateRecorder {
	return &semanticStateRecorder{
		db:       db,
		cfg:      cfg,
		provider: provider,
		warnf:    warnf,
	}
}

// resolve returns the memoized enrichment identity basis. ok=false means
// semantic state must not be recorded: err == nil means no provider is
// resolved (capability disabled or service unreachable — the provider cache
// is authoritative, so this miss is not memoized); err != nil means a
// provider exists but its health check failed (a half-up service answering
// /tag but not /health) — the failure is also not memoized, so every call
// retries, but the sync path warns only on the first failure per recorder.
func (r *semanticStateRecorder) resolve(ctx context.Context) (store.SemanticFingerprint, bool, error) {
	r.mu.Lock()
	if r.basisSet {
		b := r.basis
		r.mu.Unlock()
		return b, true, nil
	}
	r.mu.Unlock()

	p := r.provider()
	if p == nil {
		// Disabled or service down: there is no identity to record against.
		// Do not memoize this miss — the provider cache itself is
		// authoritative and a later call may see a resolved basis.
		return store.SemanticFingerprint{}, false, nil
	}
	basis, err := r.computeBasis(ctx, p)
	if err != nil {
		return store.SemanticFingerprint{}, false, err
	}
	r.mu.Lock()
	r.basis = basis
	r.basisSet = true
	r.mu.Unlock()
	return basis, true, nil
}

// computeBasis computes the identity part of the desired enrichment
// fingerprint for one pass: the service deployment identity (base URL), the
// capability set the service reports via /health, and the enrichment schema
// version. Per-document content hashes are layered on top by the callers
// (backfillDocument, RecordSyncOutcome). A health failure is an ERROR:
// callers skip recording rather than fingerprint against an empty capability
// set, which would invalidate every persisted fingerprint (one transient
// blip would otherwise force two whole-collection re-enrichment passes). A
// /health that RESPONDS with a partial model set is not a failure —
// whatever capabilities are true are populated.
//
// Plan §3.2 calls this "service/model identity". The configured MODEL name is
// deliberately not part of the basis: SemanticConfig has no model selector —
// the semantic service is a single local NLP endpoint (tools/semantic) whose
// runtime models are chosen server-side and published via /health. The base
// URL plus the /health capability set therefore identify the deployment as
// precisely as a model name could. If a client-side model selector is ever
// added to SemanticConfig, it must be folded into this basis resolve() so a
// model change invalidates documents (review minor: model-name identity).
func (r *semanticStateRecorder) computeBasis(ctx context.Context, p semantic.Provider) (store.SemanticFingerprint, error) {
	basis := store.SemanticFingerprint{SchemaVersion: store.SemanticSchemaVersion}
	if r.cfg == nil {
		basis.ServiceModel = ""
		return basis, nil
	}
	basis.ServiceModel = r.cfg.Config.Semantic.EffectiveBaseURL()

	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	health, err := p.Health(hctx)
	if err != nil {
		return basis, fmt.Errorf("semantic health check: %w", err)
	}
	basis.Capabilities = store.SemanticCapabilities{
		Language:  health.Models.LID,
		NER:       health.Models.Ner,
		Keyphrase: health.Models.Keyphrase,
		Topic:     health.Models.Topic,
	}
	return basis, nil
}

// RecordSyncOutcome records the semantic fingerprint/status of one document
// whose sync pass enriched it (plan §3.2). The fast fields were already
// persisted by the replace writer via DocumentIndex.FastFields; this only
// updates the fingerprint/status columns so `collection list/show` coverage
// is accurate after a sync, not just after a backfill.
//
// The fingerprint identity matches backfill: service identity + capability
// set + schema version, with the per-document content hash layered on top.
//
// Three columns are written: semantic_fingerprint (the full fingerprint, used
// by the per-doc skip check), semantic_basis (the identity-only fingerprint,
// the coarse stale-selection key), and semantic_source_hash (a mirror of
// documents.content_hash so the stale query detects a content change SQL-side
// without reading chunks).
//
// fastFields is the enricher's output for the document:
//
//   - nil: enrichment failed (service down/malformed) or was not attempted
//     (disabled, no readable text). When semantic enrichment is enabled the
//     document is marked stale with an empty identity basis (prior fields
//     preserved — the nil map keeps them) so the next backfill re-selects and
//     retries it; when semantic is disabled entirely, no state is recorded
//     (nothing to be stale about, and the capability is simply off).
//   - non-nil (even empty): enrichment succeeded, possibly recomputing to
//     nothing; the document was marked current with the full desired
//     fingerprint so later passes/backfills skip it.
func (r *semanticStateRecorder) RecordSyncOutcome(ctx context.Context, colType store.CollectionType, docID int64, fastFields map[string]string, chunks []store.IndexChunk) error {
	if docID == 0 || !semanticEligible(colType) {
		return nil
	}
	if fastFields == nil && (r.cfg == nil || !r.cfg.Config.Semantic.Enabled) {
		return nil
	}
	// Both remaining paths persist semantic state, so read the document's
	// current content hash here: semantic_source_hash mirrors documents.content_hash.
	doc, err := r.db.GetDocumentByIDContext(ctx, docID)
	if err != nil {
		return fmt.Errorf("load document %d: %w", docID, err)
	}
	if fastFields == nil {
		// Enabled but the service did not respond (or there was no text to
		// send): record the stale state with an EMPTY identity basis so the
		// stale query re-selects this document (semantic_basis != desiredBasis)
		// and the next backfill re-enriches it. Pass nil fast fields so
		// previously stored semantic fields are preserved.
		return r.db.UpdateSemanticState(ctx, docID, "", "", doc.ContentHash, store.SemanticStatusStale, nil)
	}
	basis, ok, err := r.resolve(ctx)
	if err != nil {
		// Half-up service: the enrichment itself succeeded, so record nothing
		// — documents keep their previous fingerprint and are re-selected by a
		// later healthy pass. Warn once per recorder instance (per Indexer on
		// the sync path) instead of once per document.
		r.mu.Lock()
		first := !r.basisFailed
		r.basisFailed = true
		r.mu.Unlock()
		if first && r.warnf != nil {
			r.warnf("  WARN: semantic state not recorded this sync (service health check failed): %v\n", err)
		}
		return nil
	}
	if !ok {
		return nil
	}
	// semantic_basis is the identity-only fingerprint (no content); the full
	// fingerprint (identity + chunk-based content hash) is what the per-doc
	// skip check compares. semantic_source_hash mirrors documents.content_hash
	// for the SQL-side content-change signal.
	identity := basis.Compute()
	basis.ContentHash = semanticContentHash(chunks)
	fullFP := basis.Compute()
	return r.db.UpdateSemanticState(ctx, docID, fullFP, identity, doc.ContentHash, store.SemanticStatusCurrent, nil)
}

// RecordBackfillCurrent records a document as semantically current with the
// full desired fingerprint (identity + chunk content hash), after the
// backfill pass resolved the basis successfully. fields follows the store's
// nil-map contract:
//
//   - nil: preserve previously stored fast fields — the no-readable-text
//     finalize where there is nothing to enrich but the fingerprint should
//     still be recorded so later passes skip without re-reading chunks.
//   - non-nil (even empty): replace the stored fast fields — the explicit
//     clear for a successful recompute-to-nothing with no native fields
//     remaining (review M1).
func (r *semanticStateRecorder) RecordBackfillCurrent(ctx context.Context, docID int64, sourceHash string, chunks []store.IndexChunk, fields map[string]string) error {
	basis, ok, err := r.resolve(ctx)
	if !ok {
		if err != nil {
			return fmt.Errorf("semantic basis unresolved: %w", err)
		}
		return fmt.Errorf("semantic basis unresolved: no provider")
	}
	identity := basis.Compute()
	basis.ContentHash = semanticContentHash(chunks)
	desiredFP := basis.Compute()
	return r.db.UpdateSemanticState(ctx, docID, desiredFP, identity, sourceHash, store.SemanticStatusCurrent, fields)
}

// RecordBackfillError records a failed enrichment with the FULL desired
// fingerprint and nil fast fields: the stale query re-selects every document
// whose semantic_status = 'error', so the document is retried on the next
// pass instead of being pinned as a permanent failure. The error status (not
// the fingerprint) is what guarantees the retry.
func (r *semanticStateRecorder) RecordBackfillError(ctx context.Context, docID int64, sourceHash string, chunks []store.IndexChunk) error {
	basis, ok, err := r.resolve(ctx)
	if !ok {
		if err != nil {
			return fmt.Errorf("semantic basis unresolved: %w", err)
		}
		return fmt.Errorf("semantic basis unresolved: no provider")
	}
	identity := basis.Compute()
	basis.ContentHash = semanticContentHash(chunks)
	desiredFP := basis.Compute()
	return r.db.UpdateSemanticState(ctx, docID, desiredFP, identity, sourceHash, store.SemanticStatusError, nil)
}

// desiredFingerprint returns the full desired fingerprint (identity +
// chunk-based content hash) for the per-document skip check, reporting
// ok=false when the basis is unresolved.
func (r *semanticStateRecorder) desiredFingerprint(ctx context.Context, chunks []store.IndexChunk) (string, bool) {
	basis, ok, _ := r.resolve(ctx)
	if !ok {
		return "", false
	}
	b := basis
	b.ContentHash = semanticContentHash(chunks)
	return b.Compute(), true
}

// semanticContentHash derives a stable per-document content hash over the
// readable chunk contents the semantic service sees (the same filter
// semanticFastFields applies). NUL separates chunks so adjacent chunks cannot
// merge ambiguously.
func semanticContentHash(chunks []store.IndexChunk) string {
	h := sha256.New()
	sep := []byte{0}
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) == "" {
			continue
		}
		h.Write([]byte(c.Content))
		h.Write(sep)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hasReadableText reports whether any chunk carries readable text that could
// be sent for semantic enrichment.
func hasReadableText(chunks []store.IndexChunk) bool {
	for _, c := range chunks {
		if strings.TrimSpace(c.Content) != "" {
			return true
		}
	}
	return false
}

// recordSemanticSyncState is the sync-path entry point the format handlers
// call (document_handler, media_handlers, the conversation path); it routes
// through the indexer-level recorder so the basis is resolved once per
// indexer instance and the resolve failure warns once per instance.
func (idx *Indexer) recordSemanticSyncState(ctx context.Context, colType store.CollectionType, docID int64, fastFields map[string]string, chunks []store.IndexChunk) error {
	return idx.semanticState().RecordSyncOutcome(ctx, colType, docID, fastFields, chunks)
}

// semanticState returns the Indexer-level recorder, creating it lazily on
// first use. The sync path records one fingerprint per document, so the
// recorder (and with it the memoized basis) must live on the Indexer rather
// than per call; WithSemanticProvider resets it so provider swaps observe
// the new identity.
func (idx *Indexer) semanticState() *semanticStateRecorder {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.semState == nil {
		idx.semState = newSemanticStateRecorder(idx.db, idx.cfg, idx.semanticProvider, idx.warnf)
	}
	return idx.semState
}
