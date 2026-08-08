package search

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/blevesearch/vellum"
)

// Autocomplete provides prefix completion using an FST (Finite State Transducer).
type Autocomplete struct {
	fst     *vellum.FST
	terms   []string
	tmpPath string // temp file backing the mmap'd FST; removed on Close
}

// NewAutocomplete builds an FST from the given terms and returns an Autocomplete.
// Terms are deduplicated and sorted before building the FST.
func NewAutocomplete(terms []string) (*Autocomplete, error) {
	if len(terms) == 0 {
		return &Autocomplete{terms: nil}, nil
	}

	// Deduplicate and sort terms
	seen := make(map[string]bool)
	unique := make([]string, 0, len(terms))
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	sort.Strings(unique)

	// Build FST in memory via a temp file (vellum only supports file-based I/O)
	tmpFile, err := os.CreateTemp("", "autocomplete-fst-*.bin")
	if err != nil {
		return nil, fmt.Errorf("create temp file for fst: %w", err)
	}
	tmpPath := tmpFile.Name()

	builder, err := vellum.New(tmpFile, nil)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("create fst builder: %w", err)
	}

	for _, term := range unique {
		if err := builder.Insert([]byte(term), 0); err != nil {
			builder.Close()
			tmpFile.Close()
			os.Remove(tmpPath)
			return nil, fmt.Errorf("insert term %q: %w", term, err)
		}
	}

	if err := builder.Close(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("close fst builder: %w", err)
	}
	tmpFile.Close()

	fst, err := vellum.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("open fst: %w", err)
	}

	// Remove temp file after loading into memory (vellum keeps it mmap'd)
	// We keep the file because vellum may mmap it. Clean up on Close().
	return &Autocomplete{
		fst:     fst,
		terms:   unique,
		tmpPath: tmpPath,
	}, nil
}

// Complete returns up to max completions for the given prefix.
// If max <= 0, all completions are returned.
func (a *Autocomplete) Complete(prefix string, max int) []string {
	if a == nil || a.fst == nil {
		return nil
	}

	start := []byte(prefix)
	// end is the smallest byte sequence greater than any string with the prefix.
	end := make([]byte, len(start))
	copy(end, start)
	if len(end) > 0 {
		end[len(end)-1]++
	} else {
		end = []byte{0xFF}
	}

	iter, err := a.fst.Iterator(start, end)
	if err != nil {
		return nil
	}
	defer iter.Close()

	var results []string
	for {
		key, _ := iter.Current()
		if key == nil {
			break
		}
		results = append(results, string(key))
		if max > 0 && len(results) >= max {
			break
		}
		if err := iter.Next(); err != nil {
			if err == io.EOF {
				break
			}
			// On other errors, stop iteration
			break
		}
	}

	return results
}

// Close releases resources held by the FST and removes the backing temp file.
func (a *Autocomplete) Close() error {
	if a == nil {
		return nil
	}
	var err error
	if a.fst != nil {
		err = a.fst.Close()
		a.fst = nil
	}
	if a.tmpPath != "" {
		// Best-effort removal; ignore error if the file is already gone.
		os.Remove(a.tmpPath)
		a.tmpPath = ""
	}
	return err
}

// Len returns the number of terms in the autocomplete index.
func (a *Autocomplete) Len() int {
	if a == nil {
		return 0
	}
	return len(a.terms)
}
