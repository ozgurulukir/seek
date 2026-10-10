.PHONY: build install clean test release skill-services skill-services-check

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

# Cut a release: bump plugin.json, promote CHANGELOG [Unreleased], tag, push.
# Usage: make release VERSION=X.Y.Z  (add RELEASE_ARGS=--dry-run to preview)
release:
	@./scripts/release.sh $(VERSION) $(RELEASE_ARGS)

skill-services:
	$(PYTHON_RUN) scripts/sync-skill-services.py

skill-services-check:
	$(PYTHON_RUN) scripts/sync-skill-services.py --check
