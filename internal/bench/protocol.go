package bench

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ozgurulukir/seek/internal/jsonl"
	"github.com/ozgurulukir/seek/internal/store"
)

// ServeOptions configures the benchmark query server.
type ServeOptions struct {
	DBPath string
}

// Serve implements the search-benchmark-game engine protocol: every input
// line is "COMMAND\tquery" and every output line is the integer hit count (or
// UNSUPPORTED), flushed immediately — the harness times the round trip per
// query and crashes on stalled engines, so answers must never be buffered.
//
// Command semantics follow CONTRIBUTE.md:
//
//	COUNT           total matching document count
//	TOP_K           compute the ranked top-K, output "1"
//	TOP_K_COUNT     compute the ranked top-K, output the total count
//
// seek's real search path (bm25 ranking, snippets, line spans) runs for the
// TOP_K commands so the numbers reflect what seek actually does, including
// the absence of lazy top-K pruning (no WAND in FTS5).
func Serve(ctx context.Context, in io.Reader, out io.Writer, opts ServeOptions) error {
	st, err := store.Open(opts.DBPath)
	if err != nil {
		return fmt.Errorf("open bench store: %w", err)
	}
	defer st.Close()

	scanner := jsonl.NewScanner(in)
	w := bufio.NewWriter(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := answerQueryLine(ctx, st, scanner.Text())
		if err != nil {
			return err
		}
		if _, err := w.WriteString(resp + "\n"); err != nil {
			return fmt.Errorf("write answer: %w", err)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("flush answer: %w", err)
		}
	}
	return scanner.Err()
}

// answerQueryLine translates one protocol line to its response. Malformed
// lines and untranslatable queries answer UNSUPPORTED (the harness tolerates
// it); store errors abort the run — a benchmark that silently lies is worse
// than one that crashes.
func answerQueryLine(ctx context.Context, st *store.Store, line string) (string, error) {
	command, query, ok := strings.Cut(line, "\t")
	if !ok || strings.TrimSpace(query) == "" {
		return "UNSUPPORTED", nil
	}
	ftsQuery, err := GameQueryToFTS5(query)
	if err != nil {
		return "UNSUPPORTED", nil
	}

	switch command {
	case "COUNT":
		count, err := st.CountFTSMatching(ctx, ftsQuery, nil)
		if err != nil {
			return "", fmt.Errorf("count %q: %w", query, err)
		}
		return strconv.FormatInt(count, 10), nil
	case "TOP_10", "TOP_100", "TOP_1000", "TOP_10_COUNT", "TOP_100_COUNT", "TOP_1000_COUNT":
		if _, err := st.SearchFTSContext(ctx, ftsQuery, topK(command), nil); err != nil {
			return "", fmt.Errorf("top-k %q: %w", query, err)
		}
		if !strings.HasSuffix(command, "_COUNT") {
			return "1", nil
		}
		count, err := st.CountFTSMatching(ctx, ftsQuery, nil)
		if err != nil {
			return "", fmt.Errorf("count %q: %w", query, err)
		}
		return strconv.FormatInt(count, 10), nil
	default:
		return "UNSUPPORTED", nil
	}
}

// topK extracts the K from a TOP_10/TOP_100/TOP_1000 (optionally _COUNT)
// command name.
func topK(command string) int {
	name := strings.TrimSuffix(command, "_COUNT")
	switch name {
	case "TOP_10":
		return 10
	case "TOP_100":
		return 100
	case "TOP_1000":
		return 1000
	default:
		return 10
	}
}
