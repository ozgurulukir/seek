package search

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
