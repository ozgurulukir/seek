package parserdef

import (
	"testing"

	"github.com/ozgurulukir/seek/internal/store"
)

// TestKnownMetadataFieldsAreCuratedFastFields keeps the two vocabularies in
// lockstep: parserdef validates session metadata against
// knownMetadataFields and writes those names into fast_fields, while the
// store registry decides which names are filterable/facetable. A name
// present here but missing from the registry would be written but
// unreachable through every read path.
//
// This is a test-only import: production layering (source does not import
// store) is unchanged.
func TestKnownMetadataFieldsAreCuratedFastFields(t *testing.T) {
	for field := range knownMetadataFields {
		if !store.ValidFastField(field) {
			t.Errorf("parserdef metadata field %q is missing from the store fast-field registry; "+
				"add it to curatedFastFields (internal/store/fielddef.go)", field)
		}
	}
}
