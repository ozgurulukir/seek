package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/source/parserdef"
	"github.com/ozgurulukir/seek/internal/store"
)

// ParsersCmd manages schema-driven parser definitions.
type ParsersCmd struct {
	List ParsersListCmd `cmd:"" help:"List all available parser schemas"`
}

// ParsersListCmd implements `seek parsers list`.
type ParsersListCmd struct{}

func (c *ParsersListCmd) Run(cfg *config.AppConfig) error {
	defs, err := parserdef.List()
	if err != nil {
		return fmt.Errorf("list parsers: %w", err)
	}

	// Load collections to show which schemas are linked.
	collections, colErr := loadCollectionsByParser(cfg.DBPath)
	if colErr != nil {
		// Collection lookup is best-effort — don't block the entire listing.
		fmt.Fprintf(os.Stderr, "warning: could not read collections: %v\n", colErr)
	}

	fmt.Printf("%-15s  %-9s  %-6s  %-16s  %s\n",
		"SCHEMA", "SOURCE", "VER", "DETECT", "COLLECTIONS")
	fmt.Println(strings.Repeat("-", 80))

	for _, ld := range defs {
		source := "embedded"
		if !ld.Embedded {
			source = "user-only"
		}
		if ld.Override {
			source = "override"
		}

		// Schema failed to parse — show error state.
		if ld.Def == nil {
			fmt.Printf("%-15s  %-9s  %-6s  %-16s  (parse error)\n",
				ld.Name, source, "-", "-")
			continue
		}

		version, detect := detectSummary(ld.Def)

		// Linked collections.
		cols := collections[ld.Name]
		colSummary := "-"
		if len(cols) > 0 {
			sort.Strings(cols)
			colSummary = fmt.Sprintf("%d: %v", len(cols), cols)
		}

		fmt.Printf("%-15s  %-9s  %-6s  %-16s  %s\n",
			ld.Name, source, version, detect, colSummary)
	}

	fmt.Println()
	fmt.Println("Sources: embedded (built-in), override (user file replaces built-in),")
	fmt.Println("         user-only (extra schema in ~/.config/seek/parsers/)")
	return nil
}

// detectSummary runs Match() on the schema and returns a short status string.
func detectSummary(def *parserdef.ParserDef) (version, detect string) {
	src, ver, files, err := def.Match()
	if err != nil {
		return "?", "not detected"
	}
	return fmt.Sprintf("v%d", ver.Version), fmt.Sprintf("%s: %d file(s)", src.Driver, len(files))
}

// loadCollectionsByParser groups parser collection names by their parser_name.
// If the DB doesn't exist yet (fresh install), returns an empty map without
// creating one — `parsers list` is a read-only introspection command.
func loadCollectionsByParser(dbPath string) (map[string][]string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return make(map[string][]string), nil // DB doesn't exist yet
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	collections, err := db.ListCollections()
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string)
	for _, col := range collections {
		if col.Type == store.CollectionTypeParser && col.ParserName != "" {
			result[col.ParserName] = append(result[col.ParserName], col.Name)
		}
	}
	return result, nil
}
