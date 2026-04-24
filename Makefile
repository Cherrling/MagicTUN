.PHONY: build test clean run

BIN := bin/magictun

build:
	go build -o $(BIN) ./cmd/magictun/

test:
	go test ./... -v -count=1 -timeout 60s

test-race:
	go test ./... -v -count=1 -timeout 60s -race

clean:
	rm -rf bin/

fmt:
	go fmt ./...

vet:
	go vet ./...

run: build
	$(BIN) $(ARGS)
