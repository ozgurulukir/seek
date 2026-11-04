# search-benchmark-game harness, vendored for the seek benchmark.
# Upstream: quickwit-stuff/search-benchmark-game (MIT), pinned at
# 3c124e2823e27e2c425769ffac24c71085065502 — see README.md for provenance,
# the patch list, and full usage instructions.

CORPUS := $(shell pwd)/corpus.json
WIKI_SRC = "https://www.dropbox.com/s/wwnfnu441w1ec9p/wiki-articles.json.bz2"
PUBLISHED_SRC = "https://tantivy-search.github.io/bench/results.json"
PUBLISHED_FILE ?= results-published.json
QUERIES ?= queries.txt
ENGINES ?= seek
COMMANDS ?= TOP_10 TOP_100 TOP_1000 TOP_100_COUNT COUNT
PORT ?= 8079
SEEK ?= $(shell pwd)/../../bin/seek
SMOKE_DIR ?= $(shell pwd)/smoke

export CORPUS ENGINES COMMANDS SEEK

.PHONY: corpus fetch-published queries index bench smoke smoke-clean

# One-time, ~20 GB download + transform → corpus.json (~6.5M articles).
corpus:
	@curl -# -L $(WIKI_SRC) | bunzip2 -c | python3 corpus_transform.py > $(CORPUS)

# The published results.json also carries the exact query set (962 queries);
# derive queries.txt from it so our run answers the identical queries.
fetch-published:
	@curl -# -L $(PUBLISHED_SRC) -o $(PUBLISHED_FILE)

queries: fetch-published
	@$(SEEK) bench report --emit-queries $(QUERIES) --results $(PUBLISHED_FILE)

# Build each engine's index once (streams $(CORPUS) through the engine).
index:
	@for engine in $(ENGINES); do \
		$(MAKE) --no-print-directory -C engines/$$engine index ; \
	done

# Run the harness: starts each engine via `make serve`, 10-minute warmup per
# command, 10 measured passes, writes results.json in this directory.
bench:
	@rm -f results.json
	@python3 src/client.py $(QUERIES) $(ENGINES)

# End-to-end smoke run on a tiny fixture corpus (see testdata/). Builds the
# seek engine's index from the fixture, runs the harness with tiny warmup,
# then validates the shape of the produced results.json.
smoke:
	@test -x $(SEEK) || { echo "build seek first: make build (at the repo root)"; exit 1; }
	@rm -rf $(SMOKE_DIR) && mkdir -p $(SMOKE_DIR)
	@CORPUS=$(shell pwd)/testdata/smoke-corpus.jsonl INDEX_DIR=$(SMOKE_DIR)/index $(MAKE) --no-print-directory -C engines/seek index SEEK=$(SEEK)
	@cd $(SMOKE_DIR) && INDEX_DIR=$(SMOKE_DIR)/index COMMANDS='TOP_10 COUNT' WARMUP_TIME=1 NUM_ITER=2 python3 ../src/client.py ../testdata/smoke-queries.txt seek
	@python3 -c "import json,sys; d=json.load(sys.stdin); print('smoke OK:', {c: len(e['seek']) for c, e in d['results'].items()})" < $(SMOKE_DIR)/results.json

smoke-clean:
	@rm -rf $(SMOKE_DIR)
