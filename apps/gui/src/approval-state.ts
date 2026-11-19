// Keep the complete owner-provided operation visible and bind a response to
// exactly the digest shown with that card. Never use a shortened preview.
export type Approval = { id: string; exec_id: string; request_sha256: string; created: string; expires: string; details: Record<string, unknown> };
export function approvalDetails(value: Approval): string {
  if (!value || typeof value.id !== "string" || !value.id ||
      typeof value.exec_id !== "string" || !value.exec_id ||
      typeof value.request_sha256 !== "string" || !/^[a-f0-9]{64}$/.test(value.request_sha256) ||
      typeof value.created !== "string" || typeof value.expires !== "string" ||
      !value.details || typeof value.details !== "object" || Array.isArray(value.details)) {
    throw new Error("待审批操作缺少完整确认信息，请重新读取。");
  }
  const details = value.details, runtime = details.runtime as Record<string, unknown> | undefined;
  if ((details.kind !== "script" && details.kind !== "program") ||
      typeof details.cwd !== "string" || !details.cwd ||
      typeof details.executable !== "string" || !details.executable ||
      (details.kind === "script" && typeof details.script !== "string") ||
      (details.args !== undefined && (!Array.isArray(details.args) || !details.args.every(arg => typeof arg === "string"))) ||
      !runtime || !Number.isSafeInteger(runtime.pid) || (runtime.pid as number) <= 0 ||
      typeof runtime.sha256 !== "string" || !/^[a-f0-9]{64}$/.test(runtime.sha256)) {
    throw new Error("待审批操作缺少完整执行身份，请重新读取。");
  }
  return JSON.stringify(value.details, null, 2);
}
export function approvalResponse(value: Approval, allow: boolean) {
  approvalDetails(value);
  return { approval_id: value.id, request_sha256: value.request_sha256, allow };
}
