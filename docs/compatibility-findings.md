# Compatibility and provenance findings

This document records research input and does not change the immutable pack.

- `CnCNet/cncnet-docker-tunnel` is archived. The pack's legacy container remains
  an unchanged comparison candidate only when the operator explicitly accepts
  its old runtime and protocol limitations.
- CnCNet points new deployments at `cncnet-docker-dotnetcore-tunnel`; that is a
  separate provider candidate, not a silent replacement for the requested
  legacy baseline.
- The observed XNA client revision is
  `e6e367bbe04c1a0dc1e34a8fed2856ea3ab7e8c4` on `develop`.
- The observed YRpp submodule revision is
  `ef1c565ade4a9233177a7949034ed7bd245259f3`.
- The spawner superproject and legacy tunnel revision still require a local
  clone or operator-approved remote fetch before an adapter release can claim
  fully pinned provenance.

