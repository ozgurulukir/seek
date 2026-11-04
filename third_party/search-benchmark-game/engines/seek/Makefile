# seek engine for the search-benchmark-game harness. The harness starts
# `make serve` in this directory and pipes the query protocol over
# stdin/stdout; `make index` streams the corpus into a dedicated SQLite
# database. Both delegate to the `seek bench` command surface (cmd/bench.go).
SEEK ?= seek
INDEX_DIR ?= $(CURDIR)/index
CORPUS ?= $(CURDIR)/../../corpus.json

.PHONY: index serve clean

index:
	@mkdir -p $(INDEX_DIR)
	@$(SEEK) bench index --db $(INDEX_DIR)/bench.db --collection benchmark-game < $(CORPUS)

serve:
	@$(SEEK) bench serve --db $(INDEX_DIR)/bench.db

clean:
	@rm -rf $(INDEX_DIR)
