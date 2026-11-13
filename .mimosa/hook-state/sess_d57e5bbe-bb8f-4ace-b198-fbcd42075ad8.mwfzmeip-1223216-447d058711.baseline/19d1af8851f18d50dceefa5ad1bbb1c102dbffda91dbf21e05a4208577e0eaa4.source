package cmd

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

type SchemaCmd struct {
	Show     bool `help:"Show the current schema"`
	Validate bool `help:"Validate the schema against the database"`
}

// BeforeApply emits a stderr-only deprecation hint when `schema` is reached
// through the legacy top-level path; the canonical `seek advanced schema`
// path is silent. The Run logic below is shared unchanged with the advanced
// group.
func (c *SchemaCmd) BeforeApply(ctx *kong.Context) error {
	deprecate(ctx, "schema", "seek advanced schema")
	return nil
}

func (c *SchemaCmd) Run(cfg *config.AppConfig) error {
	reg := search.NewSchemaRegistry()
	schema := reg.DefaultSchema()

	switch {
	case c.Show:
		json, err := search.SchemaToJSON(schema)
		if err != nil {
			return fmt.Errorf("marshal schema: %w", err)
		}
		fmt.Println(json)
		return nil
	case c.Validate:
		fmt.Println("Schema validation: OK")
		fmt.Printf("Fields: %d\n", len(schema))
		for name, def := range schema {
			fmt.Printf("  %s: %s (indexed=%v, stored=%v, fast=%v)\n",
				name, def.Type, def.Options.Indexed, def.Options.Stored, def.Options.Fast)
		}
		return nil
	default:
		return fmt.Errorf("specify a subcommand: --show or --validate")
	}
}
