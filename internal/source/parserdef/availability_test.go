package parserdef

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryDistinguishesMissingSourceFromUnmatchedVersion(t *testing.T) {
	for _, driver := range []string{"jsonl", "sqlite"} {
		t.Run(driver, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "source.jsonl")
			if driver == "sqlite" {
				path = filepath.Join(dir, "source.db")
			}
			def := &ParserDef{Name: "test", Sources: []SourceSpec{{Driver: driver, Paths: []string{path}}}}
			_, _, _, err := def.MatchContext(context.Background())
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("missing source=%v", err)
			}
			if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
				t.Fatal(err)
			}
			_, _, _, err = def.MatchContext(context.Background())
			if err == nil || errors.Is(err, ErrUnavailable) {
				t.Fatalf("existing incompatible source=%v", err)
			}
		})
	}
}
