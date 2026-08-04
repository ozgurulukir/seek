.PHONY: build install clean test

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -tags "fts5" -o bin/seek .

install:
	CGO_ENABLED=1 go install -tags "fts5" .

clean:
	rm -rf bin

test:
	go test -tags fts5 ./...
