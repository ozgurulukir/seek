package search

import "fmt"

// FilterKind identifies a domain filter. It intentionally has no SQL
// behavior; the persistence adapter owns translating it to its query plan.
type FilterKind string

const (
	FilterCollection FilterKind = "collection"
	FilterDocType    FilterKind = "doc_type"
	FilterLanguage   FilterKind = "language"
	FilterTag        FilterKind = "tag"
	FilterRepository FilterKind = "repository"
	FilterDateRange  FilterKind = "date_range"
	FilterChunkType  FilterKind = "chunk_type"
	FilterPath       FilterKind = "path"
	FilterWorkspace  FilterKind = "workspace"
)

// Filter is a persistence-neutral search predicate.
type Filter struct {
	Kind    FilterKind
	Field   string
	Value   string
	Pattern string
	After   string
	Before  string
	Chunk   ChunkType
}

// FilterSet combines domain predicates with AND semantics. Store-specific
// SQL types never cross this boundary.
type FilterSet struct {
	filters []Filter
}

// FilterTarget is the persistence adapter's construction surface. Search
// owns the kind-to-operation mapping; adapters only implement how each domain
// predicate is represented by their storage engine.
type FilterTarget interface {
	AddCollection(string)
	AddDocType(string)
	AddLanguage(string)
	AddTag(string)
	AddRepository(string)
	AddDateRange(string, string)
	AddChunkType(ChunkType)
	AddPath(string)
	AddWorkspace(string)
}

func NewFilterSet() *FilterSet {
	return &FilterSet{}
}

func (fs *FilterSet) Add(filter Filter) {
	if fs == nil {
		return
	}
	fs.filters = append(fs.filters, filter)
}

func (fs *FilterSet) Items() []Filter {
	if fs == nil {
		return nil
	}
	items := make([]Filter, len(fs.filters))
	copy(items, fs.filters)
	return items
}

func (fs *FilterSet) Apply(target FilterTarget) error {
	if fs == nil || len(fs.filters) == 0 {
		return nil
	}
	if target == nil {
		return fmt.Errorf("filter target is nil")
	}
	for _, filter := range fs.filters {
		switch filter.Kind {
		case FilterCollection:
			target.AddCollection(filter.Value)
		case FilterDocType:
			target.AddDocType(filter.Value)
		case FilterLanguage:
			target.AddLanguage(filter.Value)
		case FilterTag:
			target.AddTag(filter.Value)
		case FilterRepository:
			target.AddRepository(filter.Value)
		case FilterDateRange:
			target.AddDateRange(filter.After, filter.Before)
		case FilterChunkType:
			target.AddChunkType(filter.Chunk)
		case FilterPath:
			target.AddPath(filter.Pattern)
		case FilterWorkspace:
			target.AddWorkspace(filter.Value)
		default:
			return fmt.Errorf("unsupported search filter kind %q", filter.Kind)
		}
	}
	return nil
}

func CollectionFilter(name string) Filter {
	return Filter{Kind: FilterCollection, Value: name}
}

func DocTypeFilter(docType string) Filter {
	return Filter{Kind: FilterDocType, Value: docType}
}

func LanguageFilter(language string) Filter {
	return Filter{Kind: FilterLanguage, Value: language}
}

func TagFilter(tag string) Filter {
	return Filter{Kind: FilterTag, Value: tag}
}

func RepositoryFilter(repository string) Filter {
	return Filter{Kind: FilterRepository, Value: repository}
}

func DateRangeFilter(after, before string) Filter {
	return Filter{Kind: FilterDateRange, After: after, Before: before}
}

func ChunkTypeFilter(chunkType ChunkType) Filter {
	return Filter{Kind: FilterChunkType, Chunk: chunkType}
}

func PathFilter(pattern string) Filter {
	return Filter{Kind: FilterPath, Pattern: pattern}
}

func WorkspaceFilter(workspace string) Filter {
	return Filter{Kind: FilterWorkspace, Value: workspace}
}
