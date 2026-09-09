package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CodeFileInfo represents a scanned source code file.
type CodeFileInfo struct {
	Path         string
	RelativePath string
	Title        string
	Content      string
	ContentHash  string
	Mtime        float64
	LineCount    int
	Language     string
	Extension    string
}

// SupportedExtensions maps file extensions to language identifiers.
var SupportedExtensions = map[string]string{
	// Go
	".go": "go",
	// Rust
	".rs": "rust",
	// Python
	".py":  "python",
	".pyw": "python",
	// JavaScript & TypeScript
	".js":  "javascript",
	".mjs": "javascript",
	".cjs": "javascript",
	".jsx": "javascript",
	".ts":  "typescript",
	".mts": "typescript",
	".cts": "typescript",
	".tsx": "typescript",
	// C / C++
	".c":   "c",
	".h":   "c",
	".cpp": "cpp",
	".hpp": "cpp",
	".cc":  "cpp",
	".cxx": "cpp",
	".hh":  "cpp",
	// C#
	".cs": "csharp",
	// Java, Kotlin, Scala
	".java":  "java",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".scala": "scala",
	// Swift & Objective-C
	".swift": "swift",
	".m":     "objc",
	".mm":    "objcpp",
	// Ruby & PHP
	".rb":  "ruby",
	".php": "php",
	// Shell & PowerShell
	".sh":   "shell",
	".bash": "shell",
	".zsh":  "shell",
	".ps1":  "powershell",
	".psm1": "powershell",
	".bat":  "batch",
	".cmd":  "batch",
	// Web & Styles
	".html":   "html",
	".htm":    "html",
	".css":    "css",
	".scss":   "scss",
	".sass":   "sass",
	".less":   "less",
	".vue":    "vue",
	".svelte": "svelte",
	// Data & Config
	".json":    "json",
	".yaml":    "yaml",
	".yml":     "yaml",
	".toml":    "toml",
	".xml":     "xml",
	".sql":     "sql",
	".graphql": "graphql",
	".gql":     "graphql",
	".proto":   "protobuf",
	// Systems & Functional / Other
	".zig":  "zig",
	".lua":  "lua",
	".dart": "dart",
	".r":    "r",
	".jl":   "julia",
	".ex":   "elixir",
	".exs":  "elixir",
	".erl":  "erlang",
	".hrl":  "erlang",
	".clj":  "clojure",
	".cljs": "clojure",
	".hs":   "haskell",
	".lhs":  "haskell",
	".ml":   "ocaml",
	".mli":  "ocaml",
	".nix":  "nix",
}

// ignoredDirectories is the set of directory names ignored during code scanning.
var ignoredDirectories = map[string]bool{
	".git":             true,
	".svn":             true,
	".hg":              true,
	"node_modules":     true,
	"vendor":           true,
	"bower_components": true,
	"target":           true,
	"dist":             true,
	"build":            true,
	"bin":              true,
	"obj":              true,
	"out":              true,
	".next":            true,
	".nuxt":            true,
	"__pycache__":      true,
	".venv":            true,
	"venv":             true,
	"env":              true,
	".pytest_cache":    true,
	".mypy_cache":      true,
	".idea":            true,
	".vscode":          true,
	".zed":             true,
	".vs":              true,
	".terraform":       true,
	".serverless":      true,
	"coverage":         true,
	".turbo":           true,
}

// ignoredFiles contains exact file names or suffixes that should be skipped.
var ignoredFiles = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"Cargo.lock":        true,
	"go.sum":            true,
	"composer.lock":     true,
	"Gemfile.lock":      true,
	"poetry.lock":       true,
	"mix.lock":          true,
	".DS_Store":         true,
	"Thumbs.db":         true,
}

// IsIgnoredDirectory reports whether the directory name is in the default ignored list.
func IsIgnoredDirectory(name string) bool {
	return ignoredDirectories[name]
}

// IsIgnoredFile reports whether the file name is in the default ignored list.
func IsIgnoredFile(name string) bool {
	return ignoredFiles[name]
}

// DetectLanguage returns the programming language for a given file path based on extension.
func DetectLanguage(path string) (string, string) {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := SupportedExtensions[ext]; ok {
		return lang, ext
	}
	return "", ext
}

// IsBinaryFile checks if a file is binary by inspecting the first 1024 bytes for null bytes.
func IsBinaryFile(path string) bool {
	result, _ := IsBinaryFileContext(context.Background(), path)
	return result
}

func IsBinaryFileContext(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	return bytes.Contains(buf[:n], []byte{0}), nil
}

// parseGitignore reads simple ignore rules from a .gitignore file.
func parseGitignore(gitIgnorePath string) []string {
	data, err := os.ReadFile(gitIgnorePath)
	if err != nil {
		return nil
	}
	var patterns []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Clean trailing slash for folder patterns
		line = strings.TrimSuffix(line, "/")
		patterns = append(patterns, line)
	}
	return patterns
}

func matchesGitignore(relPath string, patterns []string) bool {
	normRel := filepath.ToSlash(relPath)
	base := filepath.Base(relPath)
	for _, p := range patterns {
		p = filepath.ToSlash(p)
		// Check exact base match (e.g. "dist" or "*.log")
		if matched, _ := filepath.Match(p, base); matched {
			return true
		}
		// Check path prefix or glob
		if matched, _ := filepath.Match(p, normRel); matched {
			return true
		}
		// Check directory prefix
		if strings.HasPrefix(normRel, p+"/") || normRel == p {
			return true
		}
	}
	return false
}

// shouldSkipDir reports whether a directory inside a code collection should be
// pruned during the walk. isRoot must be true for the collection root itself —
// the user explicitly pointed seek at that directory, so name-based rules
// (hidden directories, ignore-listed names) do not apply to it.
func shouldSkipDir(info os.FileInfo, relPath string, gitignorePatterns []string, isRoot bool) bool {
	if isRoot {
		return false
	}
	baseName := info.Name()
	if IsIgnoredDirectory(baseName) || (len(baseName) > 1 && strings.HasPrefix(baseName, ".") && baseName != ".") {
		return true
	}
	if len(gitignorePatterns) > 0 && matchesGitignore(relPath, gitignorePatterns) {
		return true
	}
	return false
}

func isIgnoredFile(baseName string, relPath string, gitignorePatterns []string) bool {
	if IsIgnoredFile(baseName) {
		return true
	}

	lowerBase := strings.ToLower(baseName)
	if strings.HasSuffix(lowerBase, ".min.js") ||
		strings.HasSuffix(lowerBase, ".min.css") ||
		strings.HasSuffix(lowerBase, ".map") ||
		strings.HasSuffix(lowerBase, ".bundle.js") {
		return true
	}

	if len(gitignorePatterns) > 0 && matchesGitignore(relPath, gitignorePatterns) {
		return true
	}

	return false
}

func processCodeFile(path, relPath string, info os.FileInfo, pattern string) (*CodeFileInfo, error) {
	return processCodeFileContext(context.Background(), path, relPath, info, pattern)
}

func processCodeFileContext(ctx context.Context, path, relPath string, info os.FileInfo, pattern string) (*CodeFileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lang, ext := DetectLanguage(path)
	if lang == "" {
		return nil, nil
	}

	baseName := info.Name()
	if pattern != "" && pattern != "**/*" && pattern != "*" {
		matched, _ := filepath.Match(pattern, baseName)
		if !matched {
			return nil, nil
		}
	}

	// Check max file size (skip gigantic files > 5MB to avoid memory pressure)
	if info.Size() > 5*1024*1024 {
		return nil, nil
	}

	// Null-byte check for binaries
	binary, err := IsBinaryFileContext(ctx, path)
	if err != nil {
		return nil, err
	}
	if binary {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	hash := sha256.Sum256(data)
	normRel := filepath.ToSlash(relPath)

	return &CodeFileInfo{
		Path:         path,
		RelativePath: normRel,
		Title:        normRel,
		Content:      content,
		ContentHash:  hex.EncodeToString(hash[:]),
		Mtime:        float64(info.ModTime().UnixNano()) / 1e9,
		LineCount:    strings.Count(content, "\n") + 1,
		Language:     lang,
		Extension:    ext,
	}, nil
}

// ScanCode scans a directory for source code files.
func ScanCode(dir, pattern string) ([]CodeFileInfo, error) {
	files, _, err := ScanCodeWithWarningsContext(context.Background(), dir, pattern)
	return files, err
}

// ScanCodeWithWarnings is like ScanCode but also returns the paths that were
// skipped due to unreadable entries (permission errors, read failures, etc.).
// Callers can surface these as WARN lines so partially-indexed collections do
// not fail silently.
//
// Note on .gitignore support: only the repository-root .gitignore at dir is
// honoured. Nested .gitignore files, negation rules (!pattern), ** globs and
// leading-slash anchoring are NOT supported; the matcher is a simple
// filepath.Match + path-prefix approximation. See matchesGitignore.
func ScanCodeWithWarnings(dir, pattern string) ([]CodeFileInfo, []string, error) {
	return ScanCodeWithWarningsContext(context.Background(), dir, pattern)
}

func ScanCodeWithWarningsContext(ctx context.Context, dir, pattern string) ([]CodeFileInfo, []string, error) {
	var files []CodeFileInfo
	var skipped []string
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	var gitignorePatterns []string
	if giData := parseGitignore(filepath.Join(absDir, ".gitignore")); len(giData) > 0 {
		gitignorePatterns = giData
	}

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			skipped = append(skipped, path)
			return nil // skip unreadable paths, but record them
		}

		relPath, rErr := filepath.Rel(absDir, path)
		if rErr != nil {
			relPath = path
		}

		// Check directory exclusion. The collection root itself is exempt:
		// `seek add ~/.hermes --code` must index the dot-directory the user
		// explicitly named, while hidden dirs *under* it are still skipped.
		if info.IsDir() {
			if shouldSkipDir(info, relPath, gitignorePatterns, path == absDir) {
				return filepath.SkipDir
			}
			return nil
		}

		if isIgnoredFile(info.Name(), relPath, gitignorePatterns) {
			return nil
		}

		codeFile, err := processCodeFileContext(ctx, path, relPath, info, pattern)
		if err != nil {
			skipped = append(skipped, path)
			return nil
		}
		if codeFile != nil {
			files = append(files, *codeFile)
		}

		return nil
	})

	return files, skipped, err
}
