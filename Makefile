.PHONY: test vet lint redaction

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	helm lint charts/bindery-external-runtime

redaction:
	printf '%s\n' '{"account_id":"public","handle":"safe"}' | go run ./cmd/bindery-redaction-scan

