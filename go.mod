module github.com/ozgurulukir/seek

go 1.24.0

require (
	github.com/alecthomas/kong v1.6.0
	github.com/blevesearch/snowballstem v0.9.0
	github.com/blevesearch/vellum v1.2.0
	github.com/coder/hnsw v0.6.1
	github.com/gen2brain/go-fitz v1.28.2
	github.com/klauspost/compress v1.19.2
	github.com/mattn/go-sqlite3 v1.14.24
	github.com/viterin/vek v0.4.3
	golang.org/x/term v0.40.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/bits-and-blooms/bitset v1.24.2 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/chewxy/math32 v1.10.1 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/google/renameio v1.0.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/viterin/partial v1.1.0 // indirect
	golang.org/x/exp v0.0.0-20240506185415-9bf2ced13842 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/google/renameio => ./third_party/renameio
