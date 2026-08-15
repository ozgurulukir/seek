package source_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ozgurulukir/seek/internal/source"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path     string
		wantLang string
		wantExt  string
	}{
		{"main.go", "go", ".go"},
		{"src/lib.rs", "rust", ".rs"},
		{"app/index.ts", "typescript", ".ts"},
		{"components/Button.tsx", "typescript", ".tsx"},
		{"scripts/script.py", "python", ".py"},
		{"server.js", "javascript", ".js"},
		{"main.cpp", "cpp", ".cpp"},
		{"header.hpp", "cpp", ".hpp"},
		{"code.c", "c", ".c"},
		{"Program.cs", "csharp", ".cs"},
		{"App.java", "java", ".java"},
		{"Main.kt", "kotlin", ".kt"},
		{"run.sh", "shell", ".sh"},
		{"deploy.ps1", "powershell", ".ps1"},
		{"style.css", "css", ".css"},
		{"style.scss", "scss", ".scss"},
		{"index.html", "html", ".html"},
		{"config.yaml", "yaml", ".yaml"},
		{"config.toml", "toml", ".toml"},
		{"data.json", "json", ".json"},
		{"schema.sql", "sql", ".sql"},
		{"unknown.xyz", "", ".xyz"},
	}

	for _, tt := range tests {
		lang, ext := source.DetectLanguage(tt.path)
		if lang != tt.wantLang || ext != tt.wantExt {
			t.Errorf("DetectLanguage(%q) = (%q, %q), want (%q, %q)",
				tt.path, lang, ext, tt.wantLang, tt.wantExt)
		}
	}
}

func TestScanCode(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Regular source file
	mainGo := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Subdirectory source file
	subDir := filepath.Join(tmpDir, "pkg", "utils")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	utilPy := filepath.Join(subDir, "util.py")
	if err := os.WriteFile(utilPy, []byte("def hello():\n    print('hello')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Ignored directory file (node_modules)
	nmDir := filepath.Join(tmpDir, "node_modules", "package")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, "index.js"), []byte("module.exports = {}"), 0644); err != nil {
		t.Fatal(err)
	}

	// 4. Ignored lockfile
	if err := os.WriteFile(filepath.Join(tmpDir, "package-lock.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// 5. Binary file with null bytes
	binFile := filepath.Join(tmpDir, "binary.go")
	if err := os.WriteFile(binFile, []byte("package main\x00\x00binary"), 0644); err != nil {
		t.Fatal(err)
	}

	// 6. Minified js file
	if err := os.WriteFile(filepath.Join(tmpDir, "bundle.min.js"), []byte("console.log(1);"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := source.ScanCode(tmpDir, "")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(files), files)
	}

	fileMap := make(map[string]source.CodeFileInfo)
	for _, f := range files {
		fileMap[f.RelativePath] = f
	}

	if _, ok := fileMap["main.go"]; !ok {
		t.Errorf("expected main.go in results")
	}
	if _, ok := fileMap["pkg/utils/util.py"]; !ok {
		t.Errorf("expected pkg/utils/util.py in results")
	}
}

func TestScanCode_Gitignore(t *testing.T) {
	tmpDir := t.TempDir()

	// Write .gitignore
	gitignore := filepath.Join(tmpDir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("ignored_dir/\n*.temp.ts\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Active file
	if err := os.WriteFile(filepath.Join(tmpDir, "index.ts"), []byte("console.log('hi');\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ignored file pattern
	if err := os.WriteFile(filepath.Join(tmpDir, "draft.temp.ts"), []byte("console.log('draft');\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ignored folder
	ignDir := filepath.Join(tmpDir, "ignored_dir")
	if err := os.MkdirAll(ignDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignDir, "app.ts"), []byte("console.log('ignored');\n"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := source.ScanCode(tmpDir, "")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].RelativePath != "index.ts" {
		t.Errorf("expected index.ts, got %s", files[0].RelativePath)
	}
}
