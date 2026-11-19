# R1 reconciliation implementation report

Date: 2026-11-16

## Scope

This tranche adds a strict, typed, Runtime-owner-only R1 present-state classifier in `internal/runtimeowner/reconciliation_r1.go`. It accepts only an uncertain original operation with an immutable non-clear binding, a confirmed `requested` commit for the exact bound candidate, and a missing or unknown historical `ready` acknowledgement. Evidence must include the retained namespace and record digest, owner admission, no active worker or reconciler, authenticated kernel peer/process identity, exact activation and launch, loaded image and source-manifest proof, stable exact current selection, and settled jobs/runtime/source/probe/observer resource debt.

`ClassifyR1` is a pure predicate. It does not append a journal resolution, mutate record/outcome/effects, replay an effect, start or stop a process, or expose IPC/CLI authority. Native process, probe and observer capture is not implemented in this tranche. Release source acquisition has a grouped owner recorder boundary: intent precedes private quarantine allocation, and the receipt closes only after quarantine cleanup and original binary/root closure all succeed; uncertain closure records unknown. Input handles acquired before intent retain operation-level cleanup uncertainty without claiming pre-intent ledger coverage. R1 classification grants no cleanup authority. Activation selection still needs an authoritative selected receipt. The resolution schema now reserves `requested_applied`, `historical_ready_ack_uncertain`, and `evidence_evaluator` fields for a later owner writer; R4 `not_applied` behavior remains unchanged.

## Validation

The focused R1 tests cover acceptance of complete requested-present evidence; rejection of baseline restore evidence and ready-only evidence; and refusal for wrong peer PID, image, launch, source, namespace, stale record digest, unstable ready, active worker/reconciler, unknown or missing debt, and unauthenticated owner evidence.

Executed with the pinned Go 1.25.3 toolchain available in the workspace visualization directory:

```
go test ./internal/runtimeowner -run 'TestClassifyR1' -count=1
# ok

go test ./internal/runtimeowner -count=1
# ok
```

No endpoint, native observation, automatic reconciliation, or production deployment claim is made by this report.
