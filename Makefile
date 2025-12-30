SHELL := /usr/bin/env bash

.PHONY: help test test-integration envtest tidy fmt proto kind-demo kind-down run-controller

help:
	@echo "Targets:"
	@echo "  make test           Run Go tests"
	@echo "  make test-integration Run envtest integration tests"
	@echo "  make tidy           Run go mod tidy"
	@echo "  make fmt            Run gofmt on the repo"
	@echo "  make proto          Regenerate protobuf stubs (requires protoc + plugins)"
	@echo "  make kind-demo      Create Kind cluster + apply CRDs/examples"
	@echo "  make kind-down      Tear down Kind cluster"
	@echo "  make run-controller Run controller manager locally"

test:
	go test ./...

ENVTEST_K8S_VERSION ?= 1.31.0

envtest:
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

test-integration: envtest
	ANVIL_INTEGRATION=1 KUBEBUILDER_ASSETS="$$(setup-envtest use -p path $(ENVTEST_K8S_VERSION))" go test ./... -run Integration

tidy:
	go mod tidy

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './vendor/*')

proto:
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc -I . \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/game/engine/v1/engine.proto

kind-demo:
	./k8s/dev/kind-demo.sh

kind-down:
	./k8s/dev/kind-down.sh

run-controller:
	go run .
