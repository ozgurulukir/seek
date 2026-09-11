package app

import (
	"context"

	"github.com/ozgurulukir/seek/internal/search"
	"github.com/ozgurulukir/seek/internal/store"
)

// StoreSearchRepository is the composition-root adapter between search's
// persistence-neutral contract and SQLite. Keeping it here prevents the
// search domain package from importing Store types or SQL concerns.
type StoreSearchRepository struct {
	store *store.Store
}

func NewStoreSearchRepository(s *store.Store) *StoreSearchRepository {
	return &StoreSearchRepository{store: s}
}

func (r *StoreSearchRepository) SearchFTS(ctx context.Context, query string, limit int, filters *search.FilterSet) ([]search.Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchFTSContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

func (r *StoreSearchRepository) SearchVector(ctx context.Context, query []float32, limit int, filters *search.FilterSet) ([]search.Result, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	results, err := r.store.SearchVectorContext(ctx, query, limit, storeFilters)
	return fromStoreResults(results), err
}

// SortValues resolves sort keys through the store: documents-column
// pseudo-fields (created_at, line_count, mtime, path, title) come from the
// documents row, every other name from fast_fields.
func (r *StoreSearchRepository) SortValues(ctx context.Context, documentIDs []int64, field string) (map[int64]interface{}, error) {
	return r.store.SortValues(ctx, documentIDs, field)
}

func (r *StoreSearchRepository) GetChunkContent(ctx context.Context, chunkID int64) (string, error) {
	return r.store.GetChunkContentContext(ctx, chunkID)
}

func (r *StoreSearchRepository) ExecuteAggregation(ctx context.Context, aggregation search.Aggregation, filters *search.FilterSet) ([]search.Bucket, error) {
	storeFilters, err := toStoreFilters(filters)
	if err != nil {
		return nil, err
	}
	spec := aggregation.Spec()
	buckets, err := r.store.ExecuteAggregationContext(ctx, store.AggregationSpec{
		Type:     string(spec.Type),
		Field:    spec.Field,
		Interval: spec.Interval,
		Ranges:   spec.Ranges,
	}, storeFilters)
	if err != nil {
		return nil, err
	}
	out := make([]search.Bucket, len(buckets))
	for i, bucket := range buckets {
		out[i] = search.Bucket{Key: bucket.Key, Count: bucket.Count}
	}
	return out, nil
}

func fromStoreResults(results []store.SearchResult) []search.Result {
	if results == nil {
		return nil
	}
	out := make([]search.Result, 0, len(results))
	for _, result := range results {
		out = append(out, search.Result{
			DocumentID: result.DocumentID,
			ChunkID:    result.ChunkID,
			Seq:        result.Seq,
			Title:      result.Title,
			Path:       result.Path,
			Collection: result.Collection,
			Content:    result.Content,
			Score:      result.Score,
			ChunkType:  search.ChunkType(result.ChunkType),
			ImagePath:  result.ImagePath,
			StartLine:  result.StartLine,
			EndLine:    result.EndLine,
		})
	}
	return out
}

func toStoreFilters(filters *search.FilterSet) (*store.FilterSet, error) {
	if filters == nil || len(filters.Items()) == 0 {
		return nil, nil
	}
	target := &storeFilterTarget{filters: store.NewFilterSet()}
	if err := filters.Apply(target); err != nil {
		return nil, err
	}
	return target.filters, nil
}

type storeFilterTarget struct{ filters *store.FilterSet }

func (t *storeFilterTarget) AddCollection(name string) {
	t.filters.Add(&store.CollectionFilter{Name: name})
}
func (t *storeFilterTarget) AddDocType(typ string) { t.filters.Add(&store.DocTypeFilter{Type: typ}) }
func (t *storeFilterTarget) AddLanguage(language string) {
	t.filters.Add(&store.FastFieldFilter{Field: "lang", Value: language})
}
func (t *storeFilterTarget) AddTag(tag string) { t.filters.Add(&store.TagFilter{Tag: tag}) }
func (t *storeFilterTarget) AddRepository(repository string) {
	t.filters.Add(&store.FastFieldFilter{Field: "repo", Value: repository})
}
func (t *storeFilterTarget) AddDateRange(after, before string) {
	t.filters.Add(&store.DateRangeFilter{After: after, Before: before})
}
func (t *storeFilterTarget) AddChunkType(chunkType search.ChunkType) {
	t.filters.Add(&store.ChunkTypeFilter{Type: int(chunkType)})
}
func (t *storeFilterTarget) AddPath(pattern string) {
	t.filters.Add(&store.PathFilter{Pattern: pattern})
}
func (t *storeFilterTarget) AddWorkspace(workspace string) {
	t.filters.Add(&store.FastFieldFilter{Field: "workspace", Value: workspace})
}
func (t *storeFilterTarget) AddFastField(field, value string) {
	// Names are validated upstream (CLI --field / MCP args) against the
	// curated registry plus fields present in the index; unknown names are
	// not silently dropped here because a validated dynamic field must still
	// filter. SQL safety is unaffected either way: the field name is always
	// a bound parameter, never an identifier.
	t.filters.Add(&store.FastFieldFilter{Field: field, Value: value})
}
