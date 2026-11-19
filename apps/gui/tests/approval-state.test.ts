import { test } from "node:test";
import assert from "node:assert/strict";
import { approvalDetails, approvalResponse, type Approval } from "../src/approval-state";
import { esc } from "../src/ui";
const request: Approval = { id: "a1", exec_id: "e1", request_sha256: "a".repeat(64), created: "now", expires: "later",
  details: { kind: "script", executable: "/bin/sh", script: "echo <script>alert(1)</script>" + "x".repeat(4096), cwd: "/work", runtime: { pid: 42, sha256: "b".repeat(64) } } };
// Exact operation and identity survive rendering; hostile text remains inert.
test("should display complete bound approval details as escaped text", () => {
  assert.deepEqual(JSON.parse(approvalDetails(request)), request.details);
  const html = esc(approvalDetails(request));
  assert.ok(html.includes("x".repeat(4096)));
  assert.ok(!html.includes("<script>"));
  assert.ok(html.includes("&lt;script&gt;"));
});
// A stale card keeps its original digest so Runtime rejects it rather than
// silently approving a newer operation with the same ID.
test("should submit the exact displayed digest for allow and deny", () => {
  const updated = { ...request, request_sha256: "c".repeat(64) };
  for (const allow of [true, false]) {
    assert.deepEqual(approvalResponse(request, allow), { approval_id: "a1", request_sha256: "a".repeat(64), allow });
    assert.notEqual(approvalResponse(request, allow).request_sha256, updated.request_sha256);
  }
});
// Legacy preview-only frames must not expose approval controls.
test("should reject incomplete or legacy approval frames", () => {
  for (const value of [{ id: "a1", preview: "echo" }, { ...request, request_sha256: "" }, { ...request, details: undefined }, { ...request, details: {} }]) {
    assert.throws(() => approvalResponse(value as Approval, true));
  }
});
