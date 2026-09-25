.PHONY: build install clean test skill-services skill-services-check

PYTHON_RUN ?= uv run --python 3.11 --no-project

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -tags "fts5 sqlite_fts5" -o bin/seek .

install:
	CGO_ENABLED=1 go install -tags "fts5 sqlite_fts5" .

clean:
	rm -rf bin

test:
	go test -tags "fts5 sqlite_fts5" ./...

skill-services:
	$(PYTHON_RUN) scripts/sync-skill-services.py

skill-services-check:
	$(PYTHON_RUN) scripts/sync-skill-services.py --check
