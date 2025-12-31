# Engine gRPC Contract (v1)

This project includes a versioned gRPC Protobuf contract that defines the baseline RPC surface area for a generic game engine module.

## Location

- Protobuf IDL: `contracts/proto/game/engine/v1/engine.proto`
- Generated Go stubs:
  - `contracts/proto/game/engine/v1/engine.pb.go`
  - `contracts/proto/game/engine/v1/engine_grpc.pb.go`

## Package

- Protobuf package: `game.engine.v1`
- Go package: `github.com/bayleafwalker/bindery-core/contracts/proto/game/engine/v1`

## Service

Service name: `EngineModule`

RPCs:

- `InitializeWorld` — initialize or reset engine-managed world state
- `ApplyCommand` — apply a command to the world (extensible via `Command.payload` oneof)
- `Tick` — advance simulation time
- `GetStateSnapshot` — fetch a point-in-time view of `WorldState`

## Core messages

Required core message types are included:

- `Command` (and concrete examples like `MoveCommand`, plus `OpaqueCommand` for forward-compatible extensions)
- `Entity` + `Component` (component payload uses a `oneof` for extensibility)
- `WorldState`
- `TickRequest`

## Forward compatibility rules

The contract is designed to be forward compatible:

- Field numbers must never be reused.
- Deprecated fields should be moved into `reserved` ranges.
- Extensible data is modeled using `oneof` unions and `bytes` payloads for opaque/custom cases.

## Regenerating Go code

Prereqs:

- `protoc` installed
- Go-based protoc plugins installed:
  - `protoc-gen-go`
  - `protoc-gen-go-grpc`

Commands:

- Install plugins:
  - `go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.1`
  - `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1`
- Generate (from repo root):
  - `PATH="$PATH:$(go env GOPATH)/bin" protoc -I . --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative contracts/proto/game/engine/v1/engine.proto`

## Minimal server skeleton

A minimal (non-functional) Go server skeleton is provided:

- `cmd/engine-module-server/main.go`

Run:

- `go run ./cmd/engine-module-server --listen :50051`

The skeleton server returns placeholder `ok` results (with `placeholder=true` metadata) until you replace the method bodies with real engine logic.

## Minimal client smoke test

A tiny client is included to verify the generated stubs and service wiring end-to-end.

- Client: `cmd/engine-module-client/main.go`

Example (in two terminals):

- Terminal A: `go run ./cmd/engine-module-server --listen :50051`
- Terminal B: `go run ./cmd/engine-module-client --target 127.0.0.1:50051 --world world-1`

Expected result with the current skeleton server: the client prints an `ok` response (tick 0, zero entities).
