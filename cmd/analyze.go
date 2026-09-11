package cmd

import (
	"fmt"

	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

type AnalyzeCmd struct {
	Text string `arg:"" help:"Text to analyze"`
	Lang string `short:"l" help:"Language (en, tr); defaults to search.analyze_lang in config, then en"`
}

func (c *AnalyzeCmd) Run(cfg *config.AppConfig) error {
	lang := app.EffectiveAnalyzeLang(c.Lang, cfg)
	analyzer := search.NewAnalyzer(lang, true, true)
	tokens := analyzer.Analyze(c.Text)
	fmt.Printf("Analyzed (%s): %v\n", lang, tokens)
	return nil
}
