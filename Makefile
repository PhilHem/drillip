BINARY := drillip

.PHONY: build test lint fmt clean

build:
	go build -ldflags="-s -w" -o $(BINARY) .

test:
	go test -v -count=1 ./...

lint:
	golangci-lint run

fmt:
	gofumpt -w .

clean:
	rm -f $(BINARY)
