# Release checklist

An external-runtime release is not publishable until the following are
attached to its Sprintctl item:

- semantic version, exact commit tag, immutable image digest, SBOM, and build
  provenance attestation;
- OCI Helm chart version and digest;
- `go test -race ./...`, `go vet ./...`, Helm lint, and redaction receipts;
- research-pack provenance and wave acceptance references;
- operator approval for the local-inference exception, public data terms,
  network boundary, and baseline tunnel revision.

