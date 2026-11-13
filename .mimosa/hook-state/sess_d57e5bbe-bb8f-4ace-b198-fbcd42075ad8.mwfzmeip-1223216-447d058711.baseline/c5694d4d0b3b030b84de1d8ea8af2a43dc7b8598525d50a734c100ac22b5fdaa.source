package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/ozgurulukir/seek/internal/config"
)

type ConfigCmd struct {
	Edit     bool `help:"Open config in editor"`
	DryRun   bool `help:"Print the editor command instead of launching it (implies --edit)"`
	Advanced bool `help:"Also show advanced-only knobs (vector index / HNSW tuning, compression)"`
}

func (c *ConfigCmd) Run(cfg *config.AppConfig) error {
	// The config service is the single read/write/edit surface for the config
	// file; cmd never touches it directly.
	svc := config.NewService()

	if c.Edit || c.DryRun {
		if c.DryRun {
			// Preserve the legacy "Edit: <editor> <path>" line for scripts that
			// parse it, without launching anything. Fall back to "vim" when no
			// editor is resolvable, matching the legacy stub's $EDITOR-unset
			// default (avoids an empty "Edit:  <path>" on Windows).
			editor := config.ResolveEditor()
			if editor == "" {
				editor = "vim"
			}
			fmt.Printf("Edit: %s %s\n", editor, svc.Path())
			return nil
		}
		if err := svc.Edit(); err != nil {
			return err
		}
		// Fall through to re-render the (possibly changed) config.
	}

	// Show current config grouped by profile. The read below only detects an
	// absent file; the grouped view renders the resolved config from cfg.
	_, err := svc.Read()
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read config: %w", err)
		}
		fmt.Printf("Config file: %s (not found)\n\n", svc.Path())
		fmt.Println("Create it with:")
		fmt.Println("  mkdir -p ~/.config/seek")
		fmt.Println("  cat > ~/.config/seek/config.yaml << 'EOF'")
		fmt.Println("embedding:")
		fmt.Println("  base_url: https://dashscope.aliyuncs.com/compatible-mode/v1")
		fmt.Println("  api_key: ${DASHSCOPE_API_KEY}")
		fmt.Println("  model: text-embedding-v4")
		fmt.Println("  dimensions: 1024")
		fmt.Println("  # optional: force multimodal (image) embeddings and set the VL endpoint")
		fmt.Println("  # multimodal: true")
		fmt.Println("  # vl_base_url: https://your-provider/v1/embeddings")
		fmt.Println("  # optional: input prefixes for asymmetric models (nomic/e5 auto-detected)")
		fmt.Println("  # task_prefix:")
		fmt.Println("  #   query: \"search_query: \"")
		fmt.Println("  #   document: \"search_document: \"")
		fmt.Println("chunk:")
		fmt.Println("  # optional: customize chunk size based on model context capacity (defaults: 1000 chars, 100 overlap)")
		fmt.Println("  # max_size: 2000")
		fmt.Println("  # overlap: 200")
		fmt.Println("ocr:")
		fmt.Println("  # extract text from scanned PDF pages (any OpenAI-compatible vision model)")
		fmt.Println("  enabled: true")
		fmt.Println("  # base_url/api_key/model default to the embedding provider; model defaults to qwen-vl-ocr")
		fmt.Println("  # base_url: https://dashscope.aliyuncs.com/compatible-mode/v1")
		fmt.Println("  # api_key: ${DASHSCOPE_API_KEY}")
		fmt.Println("  # model: qwen-vl-ocr")
		fmt.Println("  # max_tokens: 2048")
		fmt.Println("EOF")
		return nil
	}

	fmt.Printf("Config: %s\n\n", svc.Path())
	printProfiles(config.Group(cfg, c.Advanced))
	// The plain view hides advanced-only knobs; nudge the user when any exist
	// so they are not mistaken for the whole config.
	if !c.Advanced && config.HasAdvancedKnobs(cfg.Config) {
		fmt.Println("\nadvanced knobs (vector_index, compression) are hidden; show them with: seek config --advanced")
	}

	return nil
}

// printProfiles renders each config profile as a titled block of YAML, so the
// output is valid, editable config grouped by concern. Profiles are separated
// by a blank line.
func printProfiles(profiles []config.ProfileView) {
	for i, p := range profiles {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%s — %s\n", p.Title, p.Summary)
		for _, block := range p.Sections {
			for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
				fmt.Printf("  %s\n", line)
			}
		}
	}
}
