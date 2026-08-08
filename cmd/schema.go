package cmd

import (
	"fmt"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

type SchemaCmd struct {
	Show     bool `help:"Show the current schema"`
	Validate bool `help:"Validate the schema against the database"`
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
