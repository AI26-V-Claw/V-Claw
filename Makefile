APP=vclaw
INTENT_EVAL=go run ./cmd/intent-eval

.PHONY: run build test fmt intent-eval intent-eval-list intent-eval-g3 intent-eval-read intent-eval-send intent-eval-delete intent-eval-write intent-eval-shell intent-eval-ambiguous

run:
	rtk go run ./cmd/$(APP)

build:
	rtk go build ./...

test:
	rtk go test ./...

fmt:
	rtk gofmt -w ./cmd ./internal

intent-eval:
	$(INTENT_EVAL)

intent-eval-list:
	$(INTENT_EVAL) -list-scenarios

intent-eval-g3:
	$(INTENT_EVAL) -scenario g3_full

intent-eval-read:
	$(INTENT_EVAL) -scenario read_info

intent-eval-send:
	$(INTENT_EVAL) -scenario send

intent-eval-delete:
	$(INTENT_EVAL) -scenario delete

intent-eval-write:
	$(INTENT_EVAL) -scenario write

intent-eval-shell:
	$(INTENT_EVAL) -scenario shell

intent-eval-ambiguous:
	$(INTENT_EVAL) -scenario ambiguous
