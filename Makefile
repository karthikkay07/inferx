.PHONY: build test proto tidy run/gateway

PROTO_DIR := proto
GEN_DIR   := gen

proto:
	protoc \
		--go_out=$(GEN_DIR) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=paths=source_relative \
		--proto_path=$(PROTO_DIR) \
		$(PROTO_DIR)/inferx/v1/inferx.proto

build:
	go build ./cmd/...

test:
	go test ./...

tidy:
	go mod tidy

run/gateway:
	go run ./cmd/gateway
