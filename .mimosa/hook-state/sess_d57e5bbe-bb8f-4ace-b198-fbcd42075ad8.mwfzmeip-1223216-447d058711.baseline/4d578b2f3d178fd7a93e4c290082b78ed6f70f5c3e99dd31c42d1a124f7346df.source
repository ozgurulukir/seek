package cmd

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/ozgurulukir/seek/internal/app"
	"github.com/ozgurulukir/seek/internal/config"
	"github.com/ozgurulukir/seek/internal/search"
)

type AnalyzeCmd struct {
	Text string `arg:"" help:"Text to analyze"`
	Lang string `short:"l" help:"Language (en, tr); defaults to search.analyze_lang in config, then en"`
}

// BeforeApply emits a stderr-only deprecation hint when `analyze` is reached
// through the legacy top-level path; the canonical `seek advanced analyze`
// path is silent. The Run logic below is shared unchanged with the advanced
// group.
func (c *AnalyzeCmd) BeforeApply(ctx *kong.Context) error {
	deprecate(ctx, "analyze", "seek advanced analyze")
	return nil
}

func (c *AnalyzeCmd) Run(cfg *config.AppConfig) error {
	lang := app.EffectiveAnalyzeLang(c.Lang, cfg)
	analyzer := search.NewAnalyzer(lang, true, true)
	tokens := analyzer.Analyze(c.Text)
	fmt.Printf("Analyzed (%s): %v\n", lang, tokens)
	return nil
}
