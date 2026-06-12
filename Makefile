APP=vclaw

.PHONY: run build test fmt

run:
	rtk go run ./cmd/$(APP)

build:
	rtk go build ./...

test:
	rtk go test ./...

fmt:
	rtk gofmt -w ./cmd ./internal