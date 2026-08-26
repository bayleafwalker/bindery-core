# /services

This folder is intended for **runtime services** that run the platform (binaries, APIs, daemons).

Typical contents:
- Go services (HTTP/gRPC) with their own `cmd/<service>/` entrypoints
- Shared service libraries and adapters

This folder currently holds no Go code. Current canonical sources in this repo:

- Controller manager: `main.go` (controller-runtime)
- Go entrypoints under `cmd/`:
  - `bindery-external-runtime` — external-runtime HTTP control plane
  - `bindery-udp-relay` — external-runtime UDP relay
  - `bindery-redaction-scan` — redaction scanner (`make redaction`)
  - `bindery-load-test` — operator load generator, not run by CI
  - `engine-module-server`, `engine-module-client` — engine gRPC demo pair

Notes:
- Prefer adding new service entrypoints under `cmd/` and keeping shared code in packages.
