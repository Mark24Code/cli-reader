.PHONY: build test vet fmt run sample release clean

build:
	go build -o bin/cli-epub-reader .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run: build
	./bin/cli-epub-reader testdata/demo.epub

sample:
	go run scripts/gen-demo-epub.go

release:
	./scripts/release.sh "$(VERSION)"

clean:
	rm -rf bin
