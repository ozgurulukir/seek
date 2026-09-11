package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/store"
)

// FieldsCmd inspects fast-field values and taxonomy to reduce friction in --field searches.
type FieldsCmd struct {
	Name       string `arg:"" optional:"" help:"Fast-field name to inspect (tags, topics, entities, language, etc.)"`
	JSON       bool   `help:"Output in JSON format"`
	Limit      int    `short:"l" default:"50" help:"Maximum values to return"`
	Prefix     string `help:"Filter values by prefix"`
	Collection string `short:"c" help:"Limit values to a specific collection"`
}

type jsonSummaryResponse struct {
	TotalDocs  int                     `json:"total_docs"`
	Collection string                  `json:"collection,omitempty"`
	Fields     []jsonFieldSummaryEntry `json:"fields"`
}

type jsonFieldSummaryEntry struct {
	FieldName       string  `json:"field_name"`
	MatchMode       string  `json:"match_mode"`
	DistinctValues  int     `json:"distinct_values"`
	DocCount        int     `json:"doc_count"`
	CoveragePercent float64 `json:"coverage_percent"`
}

func (c *FieldsCmd) Run(cfg *config.AppConfig) (err error) {
	db, err := app.OpenStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	if strings.TrimSpace(c.Name) == "" {
		return c.runSummary(db)
	}
	return c.runValues(db)
}

func (c *FieldsCmd) runSummary(db *store.Store) error {
	summaries, err := db.GetFastFieldSummary(c.Collection)
	if err != nil {
		return fmt.Errorf("get field summary: %w", err)
	}

	totalDocs := 0
	if len(summaries) > 0 {
		totalDocs = summaries[0].TotalDocs
	}

	if c.JSON {
		resp := jsonSummaryResponse{
			TotalDocs:  totalDocs,
			Collection: c.Collection,
			Fields:     make([]jsonFieldSummaryEntry, 0, len(summaries)),
		}
		for _, s := range summaries {
			cov := 0.0
			if totalDocs > 0 {
				cov = (float64(s.DocCount) / float64(totalDocs)) * 100.0
			}
			resp.Fields = append(resp.Fields, jsonFieldSummaryEntry{
				FieldName:       s.FieldName,
				MatchMode:       s.MatchMode,
				DistinctValues:  s.DistinctValues,
				DocCount:        s.DocCount,
				CoveragePercent: cov,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if totalDocs == 0 {
		if c.Collection != "" {
			fmt.Printf("No documents found in collection %q.\n", c.Collection)
		} else {
			fmt.Println("No documents found in index.")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FIELD\tMODE\tDISTINCT\tDOCS\tCOVERAGE")
	for _, s := range summaries {
		cov := 0.0
		if totalDocs > 0 {
			cov = (float64(s.DocCount) / float64(totalDocs)) * 100.0
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%.1f%%\n",
			s.FieldName, s.MatchMode, s.DistinctValues, s.DocCount, cov)
	}
	return w.Flush()
}

func (c *FieldsCmd) runValues(db *store.Store) error {
	name := strings.ToLower(strings.TrimSpace(c.Name))
	if !store.ValidFastField(name) {
		// Not curated — accept it when it is physically present in the index
		// (dynamic discovery, same rule as --field).
		present, err := db.ListFastFieldNames(context.Background())
		if err != nil {
			return fmt.Errorf("list fast field names: %w", err)
		}
		found := false
		for _, n := range present {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown fast field %q (%s)", c.Name, store.FieldDiscoveryHint())
		}
	}

	opts := store.ListFastFieldOptions{
		Collection: c.Collection,
		Prefix:     c.Prefix,
		Limit:      c.Limit,
	}

	values, err := db.ListFastFieldValues(name, opts)
	if err != nil {
		return fmt.Errorf("list fast field values: %w", err)
	}

	if c.JSON {
		if values == nil {
			values = []store.FieldValueCount{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(values)
	}

	if len(values) == 0 {
		if c.Prefix != "" {
			fmt.Printf("No values found for %q with prefix %q.\n", name, c.Prefix)
		} else {
			fmt.Printf("No values found for fast field %q.\n", name)
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VALUE\tCOUNT")
	for _, v := range values {
		fmt.Fprintf(w, "%s\t%d\n", v.Value, v.Count)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if c.Limit > 0 && len(values) >= c.Limit {
		fmt.Printf("\nShowing top %d values. Use -l / --limit to adjust.\n", len(values))
	}

	return nil
}
