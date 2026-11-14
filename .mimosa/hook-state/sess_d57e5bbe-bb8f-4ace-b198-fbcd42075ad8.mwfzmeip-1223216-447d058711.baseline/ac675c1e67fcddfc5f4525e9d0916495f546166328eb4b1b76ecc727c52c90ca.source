package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// EmitQueries writes the harness query set (JSONL {"query","tags"}) extracted
// from a results.json into outPath. The published results.json carries the
// exact query list the published engines ran, so deriving queries.txt from it
// guarantees our run answers the identical queries.
//
// The source list is picked deterministically — alphabetically first command,
// then first engine — so repeated invocations produce byte-identical
// queries.txt files (the harness's random.seed(2) shuffle keys off list
// order).
func EmitQueries(resultsPath, outPath string) error {
	r, err := LoadBenchmarkResults(resultsPath)
	if err != nil {
		return err
	}
	commands := make([]string, 0, len(r.Results))
	for command := range r.Results {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	if len(commands) == 0 {
		return fmt.Errorf("no commands in %s", resultsPath)
	}
	engines := make([]string, 0, len(r.Results[commands[0]]))
	for engine := range r.Results[commands[0]] {
		engines = append(engines, engine)
	}
	sort.Strings(engines)
	if len(engines) == 0 {
		return fmt.Errorf("no engines in %s", resultsPath)
	}
	queries := r.Results[commands[0]][engines[0]]
	if len(queries) == 0 {
		return fmt.Errorf("no queries in %s", resultsPath)
	}

	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create queries dir: %w", err)
		}
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create queries file: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, qr := range queries {
		tags := qr.Tags
		if tags == nil {
			tags = []string{}
		}
		line, err := json.Marshal(map[string]interface{}{"query": qr.Query, "tags": tags})
		if err != nil {
			return fmt.Errorf("encode query: %w", err)
		}
		if _, err := w.Write(line); err != nil {
			return fmt.Errorf("write query: %w", err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("write query: %w", err)
		}
	}
	return w.Flush()
}
