package cmd

import (
	"fmt"
	"os"

	"github.com/ozgurulukir/seek/internal/config"
)

type ConfigCmd struct {
	Edit bool `help:"Open config in editor"`
}

func (c *ConfigCmd) Run(cfg *config.AppConfig) error {
	path := cfg.ConfigPath()

	if c.Edit {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim"
		}
		fmt.Printf("Edit: %s %s\n", editor, path)
		return nil
	}

	// Show current config
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Config file: %s (not found)\n\n", path)
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
		fmt.Println("EOF")
		return nil
	}

	fmt.Printf("Config: %s\n\n", path)
	fmt.Println(string(data))

	return nil
}
