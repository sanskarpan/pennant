.PHONY: run test test-race conformance lint frontend-dev loadgen

run:
	go run ./cmd/server/

test:
	go test ./...

test-race:
	go test ./... -race -count=3

conformance:
	go test ./sdk/go/... -run TestConformance -v
	cd sdk/ts && bun test conformance.test.ts

lint:
	golangci-lint run

frontend-dev:
	cd frontend && bun run dev

loadgen:
	go run ./cmd/loadgen/ -clients=$(CLIENTS)
