# Release checklist

An external-runtime release is not publishable until the following are
attached to its Sprintctl item:

- semantic version, exact commit tag, immutable image digest, SBOM, and build
  provenance attestation;
- OCI Helm chart version and digest;
- `go test -race ./...`, `go vet ./...`, Helm lint, and redaction receipts;
- a restart drill resolving identity, session, placement, execution, and
  evidence-set IDs from the persisted state file;
- positive and negative calibration receipts for every consequential gate,
  including gate version and implementation hash;
- an allocator implementation revision and configuration digest in the
  placement fixture;
- research-pack provenance and wave acceptance references;
- operator approval for the local-inference exception, public data terms,
  network boundary, and baseline tunnel revision.
