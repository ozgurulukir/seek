package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ozgurulukir/seek/internal/config"
	"golang.org/x/term"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Configure API key"`
	Status AuthStatusCmd `cmd:"" help:"Show auth status"`
}

type AuthLoginCmd struct{}

type AuthStatusCmd struct{}

type provider struct {
	Name       string
	BaseURL    string
	Model      string
	Dimensions int
}

var providers = []provider{
	{
		Name:       "dashscope (阿里百炼)",
		BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:      "text-embedding-v4",
		Dimensions: 1024,
	},
	{
		Name:       "openai",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
	},
	{
		Name:       "ollama (local - nomic-embed-text)",
		BaseURL:    "http://localhost:11434/v1",
		Model:      "nomic-embed-text",
		Dimensions: 768,
	},
	{
		Name:       "custom (OpenAI-compatible)",
		BaseURL:    "",
		Model:      "",
		Dimensions: 0,
	},
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func isLocalEndpoint(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") || strings.Contains(lower, "::1")
}

func (c *AuthLoginCmd) Run(cfg *config.AppConfig) error {
	fmt.Println("\nSelect embedding provider:")
	for i, p := range providers {
		fmt.Printf("  %d) %s\n", i+1, p.Name)
	}
	fmt.Print("\nChoice [1]: ")

	choiceStr := readLine()
	choice := 0
	if choiceStr != "" {
		fmt.Sscanf(choiceStr, "%d", &choice)
		choice--
	}
	if choice < 0 || choice >= len(providers) {
		choice = 0
	}

	p := providers[choice]
	baseURL := p.BaseURL
	model := p.Model
	dimensions := p.Dimensions

	if p.BaseURL == "" {
		fmt.Print("Base URL (e.g. http://localhost:11434/v1 or https://api.example.com/v1): ")
		baseURL = strings.TrimSpace(readLine())
		if baseURL == "" {
			return fmt.Errorf("base URL cannot be empty")
		}
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			if isLocalEndpoint(baseURL) {
				baseURL = "http://" + baseURL
			} else {
				baseURL = "https://" + baseURL
			}
		}

		fmt.Print("Model name (e.g. text-embedding-3-small, bge-m3): ")
		model = readLine()
		if model == "" {
			return fmt.Errorf("model name cannot be empty")
		}

		fmt.Print("Dimensions [1024]: ")
		dimStr := readLine()
		if dimStr != "" {
			fmt.Sscanf(dimStr, "%d", &dimensions)
		}
		if dimensions <= 0 {
			dimensions = 1024
		}
	}

	var apiKey string
	if isLocalEndpoint(baseURL) {
		fmt.Print("\nAPI Key (leave blank for local 'ollama'): ")
		apiKey = strings.TrimSpace(readLine())
		if apiKey == "" {
			apiKey = "ollama"
		}
	} else {
		fmt.Print("\nAPI Key (input hidden): ")
		keyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		apiKey = strings.TrimSpace(string(keyBytes))
		if apiKey == "" {
			return fmt.Errorf("API key cannot be empty")
		}
	}

	multimodal := config.EmbeddingConfig{Model: model}.IsMultimodal()
	fmt.Print("\nUse vision-language (image) embedding endpoint? [y/N]: ")
	if yes := strings.ToLower(strings.TrimSpace(readLine())); yes == "y" || yes == "yes" {
		multimodal = true
	}
	var vlBaseURL string
	if multimodal {
		fmt.Print("VL endpoint (blank for DashScope default): ")
		vlBaseURL = strings.TrimSpace(readLine())
	}

	// Preserve existing configuration sections (rerank, extractor, vector_index, etc.)
	savedCfg := cfg.Config
	savedCfg.Embedding = config.EmbeddingConfig{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		Dimensions: dimensions,
		Multimodal: multimodal,
		VLBaseURL:  vlBaseURL,
	}

	if err := config.Save(savedCfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nSaved to %s\n", cfg.ConfigPath())
	fmt.Printf("  Provider:   %s\n", providers[choice].Name)
	fmt.Printf("  Base URL:   %s\n", baseURL)
	fmt.Printf("  Model:      %s\n", model)
	fmt.Printf("  Dimensions: %d\n", dimensions)
	fmt.Printf("  Key:        %s\n", maskKey(apiKey))

	return nil
}

func (c *AuthStatusCmd) Run(cfg *config.AppConfig) error {
	key := cfg.Config.Embedding.APIKey
	if key == "" {
		fmt.Println("Not configured. Run: seek auth login")
		return nil
	}

	fmt.Printf("Provider:   %s\n", cfg.Config.Embedding.BaseURL)
	fmt.Printf("Model:      %s\n", cfg.Config.Embedding.Model)
	fmt.Printf("Dimensions: %d\n", cfg.Config.Embedding.Dimensions)
	fmt.Printf("API Key:    %s\n", maskKey(key))
	if cfg.Config.Embedding.IsMultimodal() {
		vl := cfg.Config.Embedding.VLBaseURL
		if vl == "" {
			vl = "dashscope (default)"
		}
		fmt.Printf("Multimodal: true (VL endpoint: %s)\n", vl)
	} else {
		fmt.Printf("Multimodal: false\n")
	}

	if cfg.Config.Rerank.Enabled {
		fmt.Printf("Re-ranking: enabled (model: %s, endpoint: %s, top_n: %d)\n",
			cfg.Config.Rerank.Model, cfg.Config.Rerank.BaseURL, cfg.Config.Rerank.TopN)
	} else {
		fmt.Printf("Re-ranking: disabled\n")
	}

	if cfg.Config.Extractor.Backend != "" {
		fmt.Printf("Extractor:  %s (%s)\n", cfg.Config.Extractor.Backend, cfg.Config.Extractor.XbergBaseURL)
	}

	return nil
}

// readLine reads a line in raw mode, handling ESC, backspace, paste, and Ctrl-C cleanly.
func readLine() string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line)
	}
	defer term.Restore(fd, oldState)

	var line []rune
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}

		input := buf[:n]
		// Handle single byte controls first
		if n == 1 {
			ch := input[0]
			switch {
			case ch == '\r' || ch == '\n':
				os.Stdout.Write([]byte("\r\n"))
				return string(line)
			case ch == 3: // Ctrl-C
				term.Restore(fd, oldState)
				os.Stdout.Write([]byte("^C\r\n"))
				os.Exit(130)
			case ch == 27: // Bare ESC
				continue
			case ch == 127 || ch == 8: // Backspace / DEL
				if len(line) > 0 {
					line = line[:len(line)-1]
					os.Stdout.Write([]byte("\b \b"))
				}
				continue
			}
		}

		// Escape sequence detection (e.g. arrow keys, CSI sequences)
		if input[0] == 27 {
			continue
		}

		// Process input runes (handles paste and UTF-8 characters)
		str := string(input)
		for _, r := range str {
			if r == '\r' || r == '\n' {
				os.Stdout.Write([]byte("\r\n"))
				return string(line)
			} else if r == 3 {
				term.Restore(fd, oldState)
				os.Stdout.Write([]byte("^C\r\n"))
				os.Exit(130)
			} else if r >= 32 || r == '\t' {
				line = append(line, r)
				var runeBytes [utf8.UTFMax]byte
				rlen := utf8.EncodeRune(runeBytes[:], r)
				os.Stdout.Write(runeBytes[:rlen])
			}
		}
	}
	return string(line)
}
