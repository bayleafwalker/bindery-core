SHELL := /usr/bin/env bash

.PHONY: help test test-sample-game test-integration test-e2e envtest tidy tidy-sample-game fmt verify proto kind-demo kind-down run-controller

SAMPLE_GAME_DIR := examples/booklet-bindery-sample

help:
	@echo "Targets:"
	@echo "  make test           Run Go tests"
	@echo "  make test-sample-game Run sample game unit tests"
	@echo "  make test-integration Run envtest integration tests"
	@echo "  make test-e2e        Run Kind-based e2e smoke test"
	@echo "  make tidy           Run go mod tidy"
	@echo "  make tidy-sample-game Run go mod tidy for sample game"
	@echo "  make fmt            Run gofmt on the repo"
	@echo "  make verify         Run fmt, tidy, and test (CI pre-check)"
	@echo "  make proto          Regenerate protobuf stubs (requires protoc + plugins)"
	@echo "  make kind-demo      Create Kind cluster + install sample game"
	@echo "  make kind-down      Tear down Kind cluster"
	@echo "  make run-controller Run controller manager locally"

test:
	go test ./...

test-sample-game:
	cd "$(SAMPLE_GAME_DIR)" && go test ./...

ENVTEST_K8S_VERSION ?= 1.31.0

# Absolute path to Go-installed binaries (respects GOBIN if set).
GO_BIN_DIR := $(shell sh -c 'gobin=$$(go env GOBIN); if [ -n "$$gobin" ]; then echo "$$gobin"; else echo "$$(go env GOPATH)/bin"; fi')
SETUP_ENVTEST := $(GO_BIN_DIR)/setup-envtest

envtest:
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

test-integration: envtest
	BINDERY_INTEGRATION=1 KUBEBUILDER_ASSETS="$$("$(SETUP_ENVTEST)" use -p path $(ENVTEST_K8S_VERSION))" go test ./... -run Integration

tidy:
	go mod tidy

tidy-sample-game:
	cd "$(SAMPLE_GAME_DIR)" && go mod tidy

fmt:
	find . -name '*.go' \
		-not -path './vendor/*' \
		-not -path './.cache/*' \
		-not -path './.gocache/*' \
		-print0 | xargs -0 gofmt -w

verify: fmt tidy tidy-sample-game test test-sample-game
	@git diff --exit-code go.mod go.sum || (echo "Error: go.mod/go.sum are not tidy"; exit 1)
	@git diff --exit-code "$(SAMPLE_GAME_DIR)/go.mod" "$(SAMPLE_GAME_DIR)/go.sum" || (echo "Error: sample game go.mod/go.sum are not tidy"; exit 1)
	@echo "Verification passed!"

proto:
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc -I . \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		contracts/proto/game/engine/v1/engine.proto

kind-demo:
	./k8s/dev/kind-demo.sh

kind-down:
	./k8s/dev/kind-down.sh

run-controller:
	go run .

test-e2e:
	BINDERY_E2E=1 go test ./e2e -run TestE2ESmoke_BinderySample -count=1
