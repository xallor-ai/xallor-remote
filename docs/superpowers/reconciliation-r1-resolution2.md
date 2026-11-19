# R1 resolution writer implementation report

Date: 2026-11-16

`Journal.ReconcileR1` is an internal Runtime-owner-only writer layered on the
strict `ClassifyR1` predicate. It acquires the lifecycle lock, opens an
existing schema-6 journal for writing, validates the namespace, immutable
record digest, revision and resolution links, and appends one CAS-bound
`requested_applied` resolution. The original record bytes, outcome, effects,
and binding are never rewritten. The resolution records
`historical_ready_ack_uncertain: true`, the R1 evaluator capability, and a
settled resource state; generated `active` becomes false only through the
resolution column.

Missing or malformed journals are observational failures and do not create a
profile or database. Missing owner/process identity, baseline restoration,
ready-only or unstable evidence, active workers/reconcilers, unknown resource
debt, stale CAS, wrong namespace, oversized rows, and malformed resolutions
remain unresolved or are rejected without a write. Existing R1 resolutions
are idempotent only when their original digest and revision match the request.

Validation with the pinned Go 1.25.3 toolchain:

```
go test ./internal/runtimeowner -run 'TestReconcileR1' -count=1
go test ./internal/runtimeowner -count=1
```

No IPC, CLI, replay, process control, or production deployment surface is
added by this tranche.
