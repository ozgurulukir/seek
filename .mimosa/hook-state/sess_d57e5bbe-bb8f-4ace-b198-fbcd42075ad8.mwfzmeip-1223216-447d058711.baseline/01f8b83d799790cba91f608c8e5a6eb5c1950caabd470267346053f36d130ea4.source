package search

import (
	"fmt"
	"strconv"
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
// aggregation. ParseAggregation produces it; the Store adapter maps it to
// its own query plan.
type AggregationSpec struct {
	Type     AggregationType
	Field    string
	Interval string
	Ranges   []string
	Size     int
}

// Bucket represents an aggregation bucket.
type Bucket struct {
	Key   string
	Count int
}

// ParseAggregation parses an aggregation spec string like "type:terms" or
// "created_at:histogram:month" into the validated, persistence-neutral
// AggregationSpec. Defaults are applied exactly once, here at parse time:
// histogram intervals default to "month" and ranges to the default bucket
// set, while an unset terms size stays 0 — the persistence adapter applies
// its own configured bucket limit. It validates only the domain shape; field
// support and identifier policy are enforced by the Store adapter.
func ParseAggregation(spec string) (AggregationSpec, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 && spec != "count" {
		return AggregationSpec{}, fmt.Errorf("invalid aggregation spec %q: expected field:type[:interval]", spec)
	}
	if len(parts) == 1 && spec == "count" {
		return AggregationSpec{Type: AggregationCount}, nil
	}
	field := parts[0]
	aggType := strings.ToLower(parts[1])

	switch aggType {
	case "terms":
		size := 0
		if len(parts) == 3 && parts[2] != "" {
			var err error
			size, err = strconv.Atoi(parts[2])
			if err != nil || size <= 0 {
				return AggregationSpec{}, fmt.Errorf("invalid aggregation spec %q: terms size must be a positive integer", spec)
			}
		}
		return AggregationSpec{Type: AggregationTerm, Field: field, Size: size}, nil
	case "histogram":
		interval := "month"
		if len(parts) == 3 && parts[2] != "" {
			interval = parts[2]
		}
		return AggregationSpec{Type: AggregationHistogram, Field: field, Interval: interval}, nil
	case "range":
		ranges := []string{"0-100", "100-500", "500-"}
		if len(parts) == 3 && parts[2] != "" {
			ranges = strings.Split(parts[2], ",")
		}
		// Copy as defense in depth: both sources above are already fresh
		// slices, but callers may mutate the returned slice freely.
		return AggregationSpec{Type: AggregationRange, Field: field, Ranges: append([]string(nil), ranges...)}, nil
	case "count":
		return AggregationSpec{Type: AggregationCount}, nil
	default:
		return AggregationSpec{}, fmt.Errorf("unknown aggregation type %q", aggType)
	}
}
