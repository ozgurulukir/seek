package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/indexer"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
	"github.com/ozgurulukir/seek/internal/store"
)

// AddFlags is the raw `seek add` flag surface (mirrors cmd.AddCmd). It is the
// only input BuildAddRequest reads, so the flag→request mapping lives in this
// domain layer instead of the CLI and can be unit-tested without kong.
type AddFlags struct {
	Path         string
	Name         string
	Pattern      string
	Type         string
	Agent        string
	Parser       string
	Backend      string
	Claude       bool
	Codex        bool
	Images       bool
	Pdf          bool
	Code         bool
	Documents    bool
	Docs         bool
	Opencode     bool
	Copilot      bool
	Zed          bool
	Hermes       bool
	ClaudeSchema bool
	CodexSchema  bool
}

// selector records which collection kind a flag maps to, so aliases of the same
// kind collapse and different kinds can be detected as conflicts.
type selector struct {
	label  string
	kind   store.CollectionType
	parser string
}

// AddRequest is the single internal representation of a `seek add` intent. Every
// flag combination (native, --type/--agent, parser, legacy alias) maps to exactly
// one AddRequest via BuildAddRequest; AddCollectionService.Add turns it into a
// collection with no parallel add code paths.
type AddRequest struct {
	// Path is the collection source path for path-based types (markdown/code/
	// images/pdf/documents). It is empty for claude/codex (derived from the
	// home dir) and parser collections (derived from the schema match).
	Path string
	// Name is the collection name (already resolved to its default when unset).
	Name string
	// Pattern is the glob pattern (already resolved to its default when unset).
	Pattern string
	// Type is the persisted collection type.
	Type store.CollectionType
	// Agent is the user-facing agent (claude|codex|opencode|copilot|zed|hermes).
	// It captures the --agent value (and the implied agent for --claude/--codex).
	Agent string
	// Parser is the schema name for parser collections (empty otherwise).
	Parser string
	// Backend is the per-collection extractor backend override ("", "builtin",
	// or "xberg"). Empty means "use the config default".
	Backend string
}

// BuildAddRequest maps the raw add flags to an AddRequest. It validates that
// exactly one collection kind is selected: aliases of the same kind (e.g.
// --documents/--docs/--type documents) collapse and are accepted, while two
// selectors of different kinds — or the same parser kind with different schema
// names — return a clear conflict error. --type and --agent values are
// validated; --backend must be builtin|xberg.
func BuildAddRequest(flags AddFlags) (AddRequest, error) {
	if flags.Backend != "" && flags.Backend != "builtin" && flags.Backend != "xberg" {
		return AddRequest{}, fmt.Errorf("invalid --backend %q (want builtin or xberg)", flags.Backend)
	}

	var selectors []selector
	addNative := func(label string, kind store.CollectionType) {
		selectors = append(selectors, selector{label: label, kind: kind})
	}
	addParser := func(label, parser string) {
		selectors = append(selectors, selector{label: label, kind: store.CollectionTypeParser, parser: parser})
	}

	// Native selectors.
	if flags.Claude {
		addNative("--claude", store.CollectionTypeClaude)
	}
	if flags.Codex {
		addNative("--codex", store.CollectionTypeCodex)
	}
	if flags.Images {
		addNative("--images", store.CollectionTypeImages)
	}
	if flags.Pdf {
		addNative("--pdf", store.CollectionTypePDF)
	}
	if flags.Code {
		addNative("--code", store.CollectionTypeCode)
	}
	if flags.Documents {
		addNative("--documents", store.CollectionTypeDocuments)
	}
	if flags.Docs {
		addNative("--docs", store.CollectionTypeDocuments)
	}

	// Parser selectors (native claude/codex schema variants included).
	if flags.ClaudeSchema {
		addParser("--claude-schema", "claude")
	}
	if flags.CodexSchema {
		addParser("--codex-schema", "codex")
	}
	if flags.Opencode {
		addParser("--opencode", "opencode")
	}
	if flags.Copilot {
		addParser("--copilot", "copilot-cli")
	}
	if flags.Zed {
		addParser("--zed", "zed")
	}
	if flags.Hermes {
		addParser("--hermes", "hermes")
	}
	if flags.Parser != "" {
		addParser("--parser", flags.Parser)
	}

	// --type is the canonical native selector.
	switch strings.ToLower(strings.TrimSpace(flags.Type)) {
	case "":
		// no --type
	case "markdown":
		addNative("--type", store.CollectionTypeMarkdown)
	case "code":
		addNative("--type", store.CollectionTypeCode)
	case "documents", "doc":
		addNative("--type", store.CollectionTypeDocuments)
	case "pdf":
		addNative("--type", store.CollectionTypePDF)
	case "images":
		addNative("--type", store.CollectionTypeImages)
	default:
		return AddRequest{}, fmt.Errorf("invalid --type %q (want markdown, code, documents, pdf, or images)", flags.Type)
	}

	// --agent maps to native claude/codex or to a parser schema.
	switch strings.ToLower(strings.TrimSpace(flags.Agent)) {
	case "":
		// no --agent
	case "claude":
		addNative("--agent", store.CollectionTypeClaude)
	case "codex":
		addNative("--agent", store.CollectionTypeCodex)
	case "opencode":
		addParser("--agent", "opencode")
	case "copilot":
		addParser("--agent", "copilot-cli")
	case "zed":
		addParser("--agent", "zed")
	case "hermes":
		addParser("--agent", "hermes")
	default:
		return AddRequest{}, fmt.Errorf("invalid --agent %q (want claude, codex, opencode, copilot, zed, or hermes)", flags.Agent)
	}

	kind, parser, err := resolveKind(selectors)
	if err != nil {
		return AddRequest{}, err
	}

	req := AddRequest{
		Path:    flags.Path,
		Name:    defaultName(kind, parser, flags.Name, flags.Path),
		Pattern: defaultPattern(kind, flags.Pattern),
		Type:    kind,
		Backend: flags.Backend,
	}
	switch kind {
	case store.CollectionTypeClaude, store.CollectionTypeCodex:
		req.Agent = agentForKind(kind)
	case store.CollectionTypeParser:
		req.Agent = agentForParser(parser)
		req.Parser = parser
	}
	return req, nil
}

// resolveKind collapses the selected aliases to a single (kind, parser) and
// errors when two different kinds were selected. An empty selector set defaults
// to markdown (the implicit collection type).
func resolveKind(selectors []selector) (store.CollectionType, string, error) {
	if len(selectors) == 0 {
		return store.CollectionTypeMarkdown, "", nil
	}
	type key struct {
		kind   store.CollectionType
		parser string
	}
	distinct := map[key]selector{}
	var order []key
	for _, s := range selectors {
		k := key{s.kind, s.parser}
		if _, ok := distinct[k]; !ok {
			distinct[k] = s
			order = append(order, k)
		}
	}
	if len(order) > 1 {
		labels := make([]string, 0, len(order))
		for _, k := range order {
			labels = append(labels, distinct[k].label)
		}
		if order[0].kind == store.CollectionTypeParser {
			return "", "", fmt.Errorf("multiple parser sources selected (%s); specify only one", strings.Join(labels, ", "))
		}
		return "", "", fmt.Errorf("conflicting collection types selected (%s); specify only one", strings.Join(labels, ", "))
	}
	k := order[0]
	return distinct[k].kind, distinct[k].parser, nil
}

// defaultName resolves the collection name to its default when the caller did
// not pass one.
func defaultName(kind store.CollectionType, parser, name, path string) string {
	if name != "" {
		return name
	}
	switch kind {
	case store.CollectionTypeClaude:
		return "claude-conversations"
	case store.CollectionTypeCodex:
		return "codex-conversations"
	case store.CollectionTypeParser:
		return parser + "-conversations"
	default:
		if path != "" {
			return filepath.Base(path)
		}
	}
	return ""
}

// defaultPattern resolves the glob pattern to its default for the collection
// kind. images/pdf carry a fixed pattern (the caller's -p is ignored, matching
// the historical behavior); markdown/code honor the caller's pattern.
func defaultPattern(kind store.CollectionType, pattern string) string {
	switch kind {
	case store.CollectionTypeMarkdown:
		if pattern != "" {
			return pattern
		}
		return "**/*.md"
	case store.CollectionTypeCode:
		if pattern != "" {
			return pattern
		}
		return "**/*"
	case store.CollectionTypeImages:
		return "**/*.{png,jpg,jpeg,webp}"
	case store.CollectionTypePDF:
		return "**/*.pdf"
	case store.CollectionTypeDocuments:
		return "**/*"
	default:
		return ""
	}
}

func agentForKind(kind store.CollectionType) string {
	switch kind {
	case store.CollectionTypeClaude:
		return "claude"
	case store.CollectionTypeCodex:
		return "codex"
	}
	return ""
}

func agentForParser(parser string) string {
	switch parser {
	case "opencode":
		return "opencode"
	case "copilot-cli":
		return "copilot"
	case "zed":
		return "zed"
	case "hermes":
		return "hermes"
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	}
	return ""
}

// AddCollectionService is the single `seek add` execution path. It turns an
// AddRequest into a collection (name/pattern/path defaults, the existing-
// collection guard, the CreateCollection/CreateParserCollection/
// CreateCollectionWithBackend selection) and syncs it, with no per-type helpers
// in the CLI.
type AddCollectionService struct {
	store *store.Store
	cfg   *config.AppConfig
}

// NewAddCollectionService builds the add service from the lightweight store path
// (the same path `seek status`/`rm` use): add only needs SQLite persistence plus
// an indexer to sync the freshly created collection, so it must not open the
// full runtime (embedding provider, vector index).
func NewAddCollectionService(s *store.Store, cfg *config.AppConfig) *AddCollectionService {
	if s == nil {
		return nil
	}
	return &AddCollectionService{store: s, cfg: cfg}
}

// Add creates the collection described by req and syncs it.
func (s *AddCollectionService) Add(req AddRequest) error {
	if s == nil || s.store == nil || s.cfg == nil {
		return fmt.Errorf("add service: not configured")
	}

	switch req.Type {
	case store.CollectionTypeParser:
		return s.addParser(req, req.Name)
	case store.CollectionTypeClaude, store.CollectionTypeCodex:
		return s.addConversation(req, req.Name)
	default:
		return s.addPathBased(req, req.Name, req.Pattern)
	}
}

// addPathBased handles markdown/code/images/pdf/documents: the path is required
// and must exist; the collection is created with the resolved pattern (and the
// per-collection backend for documents), then synced.
func (s *AddCollectionService) addPathBased(req AddRequest, name, pattern string) error {
	if req.Path == "" {
		return fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	if s.checkExisting(name, true) {
		return nil
	}

	if req.Type == store.CollectionTypeDocuments {
		s.documentsNote(req.Backend)
	}

	var col *store.Collection
	if req.Type == store.CollectionTypeDocuments {
		col, err = s.store.CreateCollectionWithBackend(name, req.Type, absPath, pattern, req.Backend)
	} else {
		col, err = s.store.CreateCollection(name, req.Type, absPath, pattern)
	}
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (%s) → %s\n", col.Name, req.Type, col.Path)
	return s.sync(req, col)
}

// addConversation handles native claude/codex collections, whose source path is
// derived from the home directory (no path argument, no existence check).
func (s *AddCollectionService) addConversation(req AddRequest, name string) error {
	home, _ := os.UserHomeDir()
	var path string
	if req.Type == store.CollectionTypeClaude {
		path = filepath.Join(home, ".claude", "projects")
	} else {
		path = filepath.Join(home, ".codex")
	}

	if s.checkExisting(name, false) {
		return nil
	}

	col, err := s.store.CreateCollection(name, req.Type, path, "")
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (%s) → %s\n", col.Name, req.Type, col.Path)
	return s.sync(req, col)
}

// addParser handles schema-driven parser collections: it loads the schema,
// matches a source (fail-fast on a broken schema or no match), derives the
// primary path from the match, then creates and syncs the collection.
func (s *AddCollectionService) addParser(req AddRequest, name string) error {
	def, err := parserdef.Load(req.Parser)
	if err != nil {
		return fmt.Errorf("load parser schema: %w", err)
	}
	src, ver, files, err := def.Match()
	if err != nil {
		return fmt.Errorf("detect source for parser %q: %w", req.Parser, err)
	}

	primaryPath := ""
	if len(files) > 0 {
		primaryPath = filepath.Dir(files[0])
	}

	if s.checkExisting(name, false) {
		return nil
	}

	col, err := s.store.CreateParserCollection(name, primaryPath, "*", req.Parser)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	fmt.Printf("Created collection %q (parser: %s v%d, %d source files) → %s\n",
		col.Name, req.Parser, ver.Version, len(files), src.Paths)
	return s.sync(req, col)
}

// checkExisting reports (and prints) when a collection of this name already
// exists; the add is a no-op in that case, matching the historical behavior.
func (s *AddCollectionService) checkExisting(name string, withPath bool) bool {
	if existing, err := s.store.GetCollectionByName(name); err == nil {
		if withPath {
			fmt.Printf("Collection %q already exists (id=%d, path=%s)\n", existing.Name, existing.ID, existing.Path)
		} else {
			fmt.Printf("Collection %q already exists (id=%d)\n", existing.Name, existing.ID)
		}
		return true
	}
	return false
}

// documentsNote warns when the effective documents backend is builtin (or
// unset and the config default is builtin), since builtin only indexes
// markdown/pdf/images and reports the rich formats as unsupported.
func (s *AddCollectionService) documentsNote(backend string) {
	effective := backend
	if effective == "" {
		effective = s.cfg.Config.Extractor.Backend
	}
	if effective == "" || effective == "builtin" {
		fmt.Printf("Note: documents collection with the builtin backend only indexes markdown/pdf/images.\n")
		fmt.Printf("      For docx/xlsx/pptx/epub/html/... use: --backend xberg (or set extractor.backend: xberg).\n")
	}
}

// sync builds the indexer (applying the --backend override when set) and runs
// the collection sync.
func (s *AddCollectionService) sync(req AddRequest, col *store.Collection) error {
	return s.indexer(req).SyncCollection(col)
}

// indexer builds an indexer, applying the --backend override when set. When
// --backend is given it takes precedence over both the per-collection backend
// and the config default. A bad --backend value is surfaced as a stderr WARN
// and degrades to the default extractor rather than aborting the add.
func (s *AddCollectionService) indexer(req AddRequest) *indexer.Indexer {
	idx := indexer.New(s.cfg, s.store)
	if req.Backend == "" {
		return idx
	}
	ext, err := indexer.NewExtractor(s.cfg, req.Backend)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: --backend %s: %v\n", req.Backend, err)
		return idx
	}
	return idx.WithExtractor(ext)
}
