SHELL := /usr/bin/env bash

.PHONY: help test test-race docker-build test-sample-game test-integration test-e2e envtest controller-gen manifests verify-crds tidy tidy-sample-game fmt vet verify verify-external-runtime lint-charts redaction proto kind-demo kind-down run-controller run-controller-with-metrics

SAMPLE_GAME_DIR := examples/booklet-bindery-sample

help:
	@echo "Targets:"
	@echo "  make test           Run Go tests"
	@echo "  make test-sample-game Run sample game unit tests"
	@echo "  make test-integration Run envtest integration tests"
	@echo "  make test-e2e        Run Kind-based e2e smoke test"
	@echo "  make manifests      Regenerate CRD manifests from api/ markers"
	@echo "  make verify-crds    Check CRD manifests match the Go API types"
	@echo "  make test-race      Run Go tests with the race detector"
	@echo "  make vet            Run go vet"
	@echo "  make lint-charts    Helm lint the external-runtime chart"
	@echo "  make redaction      Smoke the redaction scanner"
	@echo "  make docker-build   Build both container images locally"
	@echo "  make verify-external-runtime  race tests + vet + chart lint"
	@echo "  make tidy           Run go mod tidy"
	@echo "  make tidy-sample-game Run go mod tidy for sample game"
	@echo "  make fmt            Run gofmt on the repo"
	@echo "  make verify         Run fmt, tidy, and test (CI pre-check)"
	@echo "  make proto          Regenerate protobuf stubs (requires protoc + plugins)"
	@echo "  make kind-demo      Create Kind cluster + install sample game"
	@echo "  make kind-down      Tear down Kind cluster"
	@echo "  make run-controller Run controller manager locally (no metrics)"
	@echo "  make run-controller-with-metrics Run controller manager locally (metrics on :8080)"

test:
	go test ./...

# The external-runtime packages were developed under `go test -race`; keep that
# guarantee available now that they share a module with the operator.
test-race:
	go test -race ./...

vet:
	go vet ./...

lint-charts:
	helm lint helm/bindery-core
	helm lint charts/bindery-external-runtime

# The operator and the external runtime are separate artifacts built from
# separate Dockerfiles; see the image table in README.md.
docker-build:
	docker build -f Dockerfile -t bindery-core:dev .
	docker build -f Dockerfile.external-runtime -t bindery-external-runtime:dev .

# The release-blocking redaction oracle, run over the public DTO shapes that
# actually cross the boundary rather than one hand-written line.
redaction:
	go run ./hack/redaction-corpus | go run ./cmd/bindery-redaction-scan

# The checks the external-runtime line ran in its own CI, kept as one target.
verify-external-runtime: test-race vet lint-charts
	@echo "External runtime verification passed!"

test-sample-game:
	cd "$(SAMPLE_GAME_DIR)" && go test ./...

ENVTEST_K8S_VERSION ?= 1.31.0

# Absolute path to Go-installed binaries (respects GOBIN if set).
GO_BIN_DIR := $(shell sh -c 'gobin=$$(go env GOBIN); if [ -n "$$gobin" ]; then echo "$$gobin"; else echo "$$(go env GOPATH)/bin"; fi')
SETUP_ENVTEST := $(GO_BIN_DIR)/setup-envtest

envtest:
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Pinned to the version recorded in the existing generated manifests
# (controller-gen.kubebuilder.io/version in k8s/crds/realms.bindery.platform.yaml).
CONTROLLER_TOOLS_VERSION ?= v0.20.0
CONTROLLER_GEN := $(GO_BIN_DIR)/controller-gen

controller-gen:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

# Regenerates CRDs from the api/ markers into k8s/crds/, using the repo's
# <plural>.<group>.yaml naming rather than controller-gen's <group>_<plural>.yaml.
#
# NOTE: the checked-in manifests for kinds other than Realm and ShardAutoscaler
# are hand-written and currently carry MORE validation than the Go markers
# produce (enums, minLength, semver patterns, richer status schemas). Running
# this target will drop those constraints. Review the diff before committing;
# see docs/standards/kubernetes/crds.md.
manifests: controller-gen
	@tmp=$$(mktemp -d); \
	"$(CONTROLLER_GEN)" crd paths=./api/... output:crd:artifacts:config=$$tmp; \
	for f in $$tmp/*.yaml; do \
		base=$$(basename "$$f" .yaml); \
		group=$${base%%_*}; \
		plural=$${base#*_}; \
		cp "$$f" "k8s/crds/$$plural.$$group.yaml"; \
		cp "$$f" "helm/bindery-core/crds/$$plural.$$group.yaml"; \
	done; \
	rm -rf "$$tmp"; \
	echo "CRD manifests regenerated into k8s/crds/ and helm/bindery-core/crds/"

# Invoked through bash so the gate still runs if the executable bit is lost
# (this repo sets core.fileMode=false, so chmod alone does not reach git).
verify-crds: controller-gen
	CONTROLLER_GEN="$(CONTROLLER_GEN)" bash ./hack/verify-crds.sh

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

verify: fmt tidy tidy-sample-game test test-sample-game verify-crds
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
	go run . --metrics-bind-address=0 --health-probe-bind-address=0

run-controller-with-metrics:
	go run .

# -timeout must exceed the test's own 12m context budget. Go's default is 10m,
# which fires first and replaces the test's diagnostic failure with a panic
# stack, hiding why the run actually failed.
test-e2e:
	BINDERY_E2E=1 go test -v ./e2e -run TestE2ESmoke_BinderySample -count=1 -timeout 20m
