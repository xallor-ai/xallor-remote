# Observation-only reconciliation IPC report

This tranche adds `runtime.operation.reconcile` as a read-only Runtime-owner
observation endpoint. The request is strictly limited to `operation_id`,
`namespace_sha256`, `expected_revision`, and `expected_record_sha256`. The owner
checks the profile namespace and current record CAS coordinates, then returns
the same operation observation as `runtime.operation.status` with
`disposition: unresolved` and blocker
`native_owner_observation_required`.

The endpoint does not create a profile or journal database, acquire a write
transaction, append a resolution, invoke install/rollback, or replay a
request. Unknown fields, oversized/malformed parameters, unknown operations,
wrong namespace, and stale revision/digest are bounded refusals. The CLI
`runtime reconcile` command derives the namespace locally, sends the four
fields over the existing IPC endpoint, and prints the JSON result without
exposing journal paths or bootstrapping an owner.

Validation run with the pinned Go 1.25.3 toolchain:

```text
go test -p 1 ./internal/runtimeowner ./internal/noderun ./internal/cli
ok   github.com/xallor-ai/xallor-remote/internal/runtimeowner
ok   github.com/xallor-ai/xallor-remote/internal/noderun
ok   github.com/xallor-ai/xallor-remote/internal/cli
```

The focused regression test covers strict unknown-field rejection, namespace
mismatch, stale CAS, unknown operation, unresolved disposition, and unchanged
record state. This endpoint intentionally does not claim R1 resolution-writer
usage or provide owner-native evidence collection.
