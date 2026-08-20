package parserdef

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed parsers/*.yaml
var embeddedSchemas embed.FS

// parserOverrideDir returns the user override directory for schemas.
// Defaults to ~/.config/seek/parsers/.
func parserOverrideDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "seek", "parsers")
}

// Load reads a parser definition by name. It first checks the user override
// directory (~/.config/seek/parsers/<name>.yaml), then falls back to the
// embedded defaults. A user file completely overrides the embedded one (no merge).
func Load(name string) (*ParserDef, error) {
	// Sanitize: reject names with path separators or ".." to prevent path traversal.
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid parser name %q: must not contain path separators or '..'", name)
	}

	// 1. User override (if present, it wins).
	overridePath := filepath.Join(parserOverrideDir(), name+".yaml")
	if data, err := os.ReadFile(overridePath); err == nil {
		def, err := parseSchema(data, overridePath)
		if err != nil {
			return nil, err
		}
		return def, nil
	}

	// 2. Embedded default.
	data, err := embeddedSchemas.ReadFile("parsers/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("parser schema %q not found (neither embedded nor in %s): %w",
			name, parserOverrideDir(), err)
	}
	return parseSchema(data, "embed:parsers/"+name+".yaml")
}

// parseSchema unmarshals and validates a schema.
func parseSchema(data []byte, source string) (*ParserDef, error) {
	// Use strict decoding to reject unknown fields.
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var def ParserDef
	if err := dec.Decode(&def); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", source, err)
	}
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("validate schema %s: %w", source, err)
	}
	return &def, nil
}

// LoadedDef is a parser definition with metadata about its provenance.
type LoadedDef struct {
	Name     string // schema name (filename without .yaml)
	Embedded bool   // true if loaded from embedded defaults
	Override bool   // true if a user override exists
	Def      *ParserDef
}

// List returns all available parser definitions: embedded defaults plus any
// user overrides that don't shadow an embedded schema.
func List() ([]LoadedDef, error) {
	return listFrom(parserOverrideDir())
}

func listFrom(overrideDir string) ([]LoadedDef, error) {
	// Collect embedded schema names.
	entries, err := embeddedSchemas.ReadDir("parsers")
	if err != nil {
		return nil, fmt.Errorf("read embedded schemas: %w", err)
	}

	var (
		result []LoadedDef
		seen   = make(map[string]bool)
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")

		wg.Add(1)
		go func(entry os.DirEntry, schemaName string) {
			defer wg.Done()

			// If a user override exists, it completely wins (consistent with Load).
			overridePath := filepath.Join(overrideDir, schemaName+".yaml")
			if overrideData, oErr := os.ReadFile(overridePath); oErr == nil {
				def, err := parseSchema(overrideData, overridePath)
				loaded := LoadedDef{
					Name:     schemaName,
					Embedded: true, // an embedded version exists
					Override: true, // but the user override is what's actually loaded
					Def:      def,
				}
				if err != nil {
					loaded.Def = nil // parse error → nil def
				}

				mu.Lock()
				result = append(result, loaded)
				seen[schemaName] = true
				mu.Unlock()
				return
			}

			// No override — use the embedded version.
			data, err := embeddedSchemas.ReadFile("parsers/" + entry.Name())
			if err != nil {
				return
			}
			def, err := parseSchema(data, "embed:parsers/"+entry.Name())
			if err != nil {
				mu.Lock()
				result = append(result, LoadedDef{Name: schemaName, Embedded: true, Def: nil})
				mu.Unlock()
				return
			}

			mu.Lock()
			result = append(result, LoadedDef{
				Name:     schemaName,
				Embedded: true,
				Override: false,
				Def:      def,
			})
			seen[schemaName] = true
			mu.Unlock()
		}(e, name)
	}

	// Wait for embedded schemas to finish before checking user-only schemas,
	// so that seen map is fully populated.
	wg.Wait()

	// Collect user-only schemas (not shadowing embedded).

	if overrideEntries, err := os.ReadDir(overrideDir); err == nil {
		for _, e := range overrideEntries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".yaml")

			// We can check 'seen' without locking here since wg.Wait() was called
			if seen[name] {
				continue // already handled as embedded+override
			}

			wg.Add(1)
			go func(entry os.DirEntry, schemaName string) {
				defer wg.Done()

				data, err := os.ReadFile(filepath.Join(overrideDir, entry.Name()))
				if err != nil {
					return
				}
				def, err := parseSchema(data, filepath.Join(overrideDir, entry.Name()))
				if err != nil {
					mu.Lock()
					result = append(result, LoadedDef{Name: schemaName, Def: nil})
					mu.Unlock()
					return
				}

				mu.Lock()
				result = append(result, LoadedDef{
					Name:     schemaName,
					Embedded: false,
					Override: true,
					Def:      def,
				})
				mu.Unlock()
			}(e, name)
		}
	}

	wg.Wait()

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
