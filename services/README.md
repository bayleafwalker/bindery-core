# /services

This folder is intended for **runtime services** that run the platform (binaries, APIs, daemons).

Typical contents:
- Go services (HTTP/gRPC) with their own `cmd/<service>/` entrypoints
- Shared service libraries and adapters

Current canonical sources in this repo:
- Go entrypoints: `cmd/`
- Controller manager: `main.go` (controller-runtime)

Notes:
- Prefer adding new service entrypoints under `cmd/` and keeping shared code in packages.
