APP=vclaw

.PHONY: run build test fmt release-check

run:
	rtk go run ./cmd/$(APP)

build:
	rtk go build ./...

test:
	rtk go test ./...

fmt:
	rtk gofmt -w ./cmd ./internal

release-check:
	powershell -ExecutionPolicy Bypass -File ./scripts/ops/release-check.ps1

