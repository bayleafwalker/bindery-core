# Agent Entrypoints

This page contains copy/paste prompts (“entrypoints”) for working with the agent consistently across contributors.

The binding rules for agents working in this repository are in
[`../../AGENTS.md`](../../AGENTS.md) — read that first. The prompts below are
conveniences, not a substitute for it.

## General Entrypoint

Use this to start a new task with minimal ambiguity.

Prompt:

```text
You are working in the bindery-core repo.

Note which subsystem you are touching: the Kubernetes operator
(api/, controllers/, main.go) or the external runtime
(internal/externalruntime, internal/relay, pkg/, cmd/bindery-*).
They share a Go module but no runtime code path. See docs/README.md.

Goal:
- [Describe the outcome you want in 1–3 bullets]

Constraints:
- Keep changes minimal and focused.
- Prefer updating existing docs/code over adding new structures.
- If anything is underspecified, ask clarifying questions.

Deliverables:
- [List exact files to create/update]

Validation:
- Operator changes: run `make verify` and report results.
- External-runtime changes: run `make verify-external-runtime` and report results.
```

## “Do Not Be Clever” Mode (Strict)

Use this when you want the agent to **strictly** follow prior outputs/specs.

Prompt:

```text
Prompt I — Strict-Mode Reminder

IMPORTANT:

Do NOT invent new fields or structures not present in prior specs.

Follow semver strictly.

Do NOT change naming conventions.

Preserve capability IDs exactly.

If anything is underspecified, STOP and ask clarifying questions instead of assuming.
```
