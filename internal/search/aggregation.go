package search

import (
	"fmt"
	"strings"
)

// AggregationType represents the type of aggregation requested by a search
// client. It is a domain value; SQL construction belongs to the persistence
// adapter.
type AggregationType string

const (
	AggregationTerm      AggregationType = "terms"
	AggregationHistogram AggregationType = "histogram"
	AggregationRange     AggregationType = "range"
	AggregationCount     AggregationType = "count"
)

// AggregationSpec is the validated, persistence-neutral description of an
// aggregation. The Store adapter maps it to its own query plan.
type AggregationSpec struct {
	Type     AggregationType
	Field    string
	Interval string
	Ranges   []string
}

// Aggregation is the parsed aggregation contract consumed by SearchRepository.
// It deliberately exposes no SQL or database row-scanning behavior.
type Aggregation interface {
	Spec() AggregationSpec
}

// Bucket represents an aggregation bucket.
type Bucket struct {
	Key   string
	Count int
}

type TermAggregation struct{ Field string }

func (a *TermAggregation) Spec() AggregationSpec {
	return AggregationSpec{Type: AggregationTerm, Field: a.Field}
}

type HistogramAggregation struct {
	Field    string
	Interval string
}

func (a *HistogramAggregation) Spec() AggregationSpec {
	interval := a.Interval
	if interval == "" {
		interval = "month"
	}
	return AggregationSpec{Type: AggregationHistogram, Field: a.Field, Interval: interval}
}

type RangeAggregation struct {
	Field  string
	Ranges []string
}

func (a *RangeAggregation) Spec() AggregationSpec {
	ranges := a.Ranges
	if len(ranges) == 0 {
		ranges = []string{"0-100", "100-500", "500-"}
	}
	return AggregationSpec{Type: AggregationRange, Field: a.Field, Ranges: append([]string(nil), ranges...)}
}

type CountAggregation struct{}

func (*CountAggregation) Spec() AggregationSpec {
	return AggregationSpec{Type: AggregationCount}
}

// ParseAggregation parses an aggregation spec string like "type:terms" or
// "created_at:histogram:month". It validates only the domain shape; field
// support and identifier policy are enforced by the Store adapter.
func ParseAggregation(spec string) (Aggregation, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 && spec != "count" {
		return nil, fmt.Errorf("invalid aggregation spec %q: expected field:type[:interval]", spec)
	}
	if len(parts) == 1 && spec == "count" {
		return &CountAggregation{}, nil
	}
	field := parts[0]
	aggType := strings.ToLower(parts[1])

	switch aggType {
	case "terms":
		return &TermAggregation{Field: field}, nil
	case "histogram":
		interval := "month"
		if len(parts) == 3 && parts[2] != "" {
			interval = parts[2]
		}
		return &HistogramAggregation{Field: field, Interval: interval}, nil
	case "range":
		ranges := []string{"0-100", "100-500", "500-"}
		if len(parts) == 3 && parts[2] != "" {
			ranges = strings.Split(parts[2], ",")
		}
		return &RangeAggregation{Field: field, Ranges: ranges}, nil
	case "count":
		return &CountAggregation{}, nil
	default:
		return nil, fmt.Errorf("unknown aggregation type %q", aggType)
	}
}
