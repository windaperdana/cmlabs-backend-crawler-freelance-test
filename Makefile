.PHONY: build run test clean

BIN := crawler

build:
	go build -o $(BIN) ./cmd/crawler

run: build
	./$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)
	rm -rf output/
