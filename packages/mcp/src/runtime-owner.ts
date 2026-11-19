import type { PackageBuild } from "./buildinfo.js";
import { failText, type Frame } from "./ipc.js";

type OwnerIpc = { call(method: string, params: Record<string, unknown>, timeoutMs?: number): Promise<Frame> };
export const OWNER_OBSERVATION_TIMEOUT_MS = 65_000;

// Connection and bootstrap are separate observations. This helper owns no
// executable path and invokes the supplied bootstrap at most once.
export async function connectOwner(ipc: { connect(timeoutMs: number): Promise<void> }, bootstrap: () => Promise<void>): Promise<void> {
  try {
    await ipc.connect(400);
  } catch (error) {
    const code = error && typeof error === "object" && "code" in error ? error.code : undefined;
    if (code !== "ENOENT" && code !== "ECONNREFUSED") throw error;
    await bootstrap();
    await ipc.connect(8000);
  }
}

export async function ensureOwner(ipc: OwnerIpc, expected: Pick<PackageBuild, "protocol" | "profile_format">): Promise<void> {
  const result = await ipc.call("runtime.ensure", {
    required_profile_format: expected.profile_format,
    required_protocol: expected.protocol,
  }, OWNER_OBSERVATION_TIMEOUT_MS);
  if (result.ok !== true || result.event) throw new Error(failText(result));
  const value = result.result?.runtime;
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Runtime 就绪响应缺少有效身份，请检查配套安装。");
  }
  const runtime = value as Record<string, unknown>;
  if (runtime.state !== "ready" || (runtime.code !== undefined && runtime.code !== "") ||
      runtime.protocol !== expected.protocol || runtime.profile_format !== expected.profile_format ||
      !Number.isSafeInteger(runtime.pid) || (runtime.pid as number) <= 0 ||
      typeof runtime.version !== "string" || !/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$/.test(runtime.version) ||
      typeof runtime.revision !== "string" || !/^[a-f0-9]{40}$/.test(runtime.revision) ||
      typeof runtime.sha256 !== "string" || !/^[a-f0-9]{64}$/.test(runtime.sha256) ||
      typeof runtime.activation_id !== "string" || !/^[a-f0-9]{32}$/.test(runtime.activation_id) ||
      typeof runtime.launch_id !== "string" || !/^[a-f0-9]{32}$/.test(runtime.launch_id)) {
    throw new Error("Runtime 就绪响应缺少有效身份，请检查配套安装。");
  }
}
