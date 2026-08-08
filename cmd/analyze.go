package cmd

import (
	"fmt"

	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

type AnalyzeCmd struct {
	Text string `arg:"" help:"Text to analyze"`
	Lang string `short:"l" default:"en" help:"Language (en, tr)"`
}

func (c *AnalyzeCmd) Run(cfg *config.AppConfig) error {
	analyzer := search.NewAnalyzer(c.Lang, true, true)
	tokens := analyzer.Analyze(c.Text)
	fmt.Printf("Analyzed (%s): %v\n", c.Lang, tokens)
	return nil
}
