.PHONY: test race vet build demo bench
test:
	go test ./...
race:
	go test -race ./...
vet:
	go vet ./...
build:
	go build -o bin/stonebridge ./cmd/stonebridge
demo:
	go run ./cmd/stonebridge -dir ./stonebridge-data
bench:
	go test ./internal/lsm -bench=. -benchmem
