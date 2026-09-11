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
	resp, err := buildFieldsSummary(db, c.Collection)
	if err != nil {
		return err
	}

	totalDocs := resp.TotalDocs

	if c.JSON {
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
	for _, s := range resp.Fields {
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%.1f%%\n",
			s.FieldName, s.MatchMode, s.DistinctValues, s.DocCount, s.CoveragePercent)
	}
	return w.Flush()
}

// buildFieldsSummary builds the field summary payload shared by the
// `seek fields` command and the MCP seek_fields tool.
func buildFieldsSummary(db *store.Store, collection string) (jsonSummaryResponse, error) {
	summaries, err := db.GetFastFieldSummary(collection)
	if err != nil {
		return jsonSummaryResponse{}, fmt.Errorf("get field summary: %w", err)
	}

	totalDocs := 0
	if len(summaries) > 0 {
		totalDocs = summaries[0].TotalDocs
	}

	resp := jsonSummaryResponse{
		TotalDocs:  totalDocs,
		Collection: collection,
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
	return resp, nil
}

// listFieldValues resolves, validates, and lists one field's values, shared
// by the `seek fields` command and the MCP seek_fields tool. The result is
// non-nil so JSON emits [] for no values.
func listFieldValues(ctx context.Context, db *store.Store, field, collection, prefix string, limit int) ([]store.FieldValueCount, error) {
	field, err := app.ValidateFastField(ctx, db, field)
	if err != nil {
		return nil, err
	}

	values, err := db.ListFastFieldValues(field, store.ListFastFieldOptions{
		Collection: collection,
		Prefix:     prefix,
		Limit:      limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list fast field values: %w", err)
	}
	if values == nil {
		values = []store.FieldValueCount{}
	}
	return values, nil
}

func (c *FieldsCmd) runValues(db *store.Store) error {
	values, err := listFieldValues(context.Background(), db, c.Name, c.Collection, c.Prefix, c.Limit)
	if err != nil {
		return err
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(values)
	}

	if len(values) == 0 {
		if c.Prefix != "" {
			fmt.Printf("No values found for %q with prefix %q.\n", c.Name, c.Prefix)
		} else {
			fmt.Printf("No values found for fast field %q.\n", c.Name)
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
