.PHONY: run test build verify
run:
	go run ./cmd/server
test:
	go test -race ./...
build:
	mkdir -p build
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -o build/server ./cmd/server
verify:
	bash scripts/verify.sh
