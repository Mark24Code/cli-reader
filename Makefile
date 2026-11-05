.PHONY: build test vet fmt run sample clean

build:
	go build -o bin/claude-epub .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run: build
	./bin/claude-epub testdata/demo.epub

sample:
	go run scripts/gen-demo-epub.go

clean:
	rm -rf bin
