type Status = {
  device_id?: string;
  workspace?: string;
  relay?: string;
  inbound?: boolean;
  has_grant?: boolean;
  online?: boolean;
  version?: string;
};

type Config = {
  relay_url?: string;
  workspace?: string;
};

function inTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(cmd, args);
}

async function http<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const j = (await res.json()) as { ok?: boolean; result?: T; message?: string };
  if (!res.ok || j.ok === false) throw new Error(j.message || "失败。");
  return j.result as T;
}

export async function status(): Promise<Status> {
  if (inTauri()) return invoke<Status>("status");
  return http<Status>("/__ipc", { method: "status" });
}

export async function grantShow(): Promise<string> {
  if (inTauri()) return invoke<string>("grant_show");
  const r = await http<{ grant?: string }>("/__ipc", { method: "grant.show" });
  return r.grant || "";
}

export async function grantIssue(): Promise<string> {
  if (inTauri()) return invoke<string>("grant_issue");
  const r = await http<{ grant?: string }>("/__ipc", { method: "grant.issue" });
  return r.grant || "";
}

export async function grantRotate(): Promise<string> {
  if (inTauri()) return invoke<string>("grant_rotate");
  const r = await http<{ grant?: string }>("/__ipc", { method: "grant.rotate" });
  return r.grant || "";
}

export async function inboundSet(enabled: boolean): Promise<void> {
  if (inTauri()) {
    await invoke("inbound_set", { enabled });
    return;
  }
  await http("/__ipc", { method: "inbound.set", params: { enabled } });
}

export async function peerList(): Promise<string[]> {
  if (inTauri()) return invoke<string[]>("peer_list");
  const r = await http<{ peers?: string[] }>("/__ipc", { method: "peer.list" });
  return r.peers || [];
}

export async function peerAdd(deviceId: string, grant: string): Promise<void> {
  if (inTauri()) {
    await invoke("peer_add", { device_id: deviceId, grant });
    return;
  }
  await http("/__ipc", { method: "peer.add", params: { device_id: deviceId, grant } });
}

export async function peerRemove(deviceId: string): Promise<void> {
  if (inTauri()) {
    await invoke("peer_remove", { device_id: deviceId });
    return;
  }
  await http("/__ipc", { method: "peer.remove", params: { device_id: deviceId } });
}

export async function configGet(): Promise<Config> {
  if (inTauri()) return invoke<Config>("config_get");
  return http<Config>("/__ipc", { method: "config.get" });
}

export async function configSetWorkspace(workspace: string): Promise<void> {
  if (inTauri()) {
    await invoke("config_set_workspace", { workspace });
    return;
  }
  await http("/__ipc", { method: "config.set", params: { workspace } });
}

export async function execCommand(
  command: string,
  deviceId: string,
  onChunk: (s: string) => void,
): Promise<void> {
  if (inTauri()) {
    const { listen } = await import("@tauri-apps/api/event");
    const un = await listen<string>("exec-out", (e) => onChunk(e.payload));
    try {
      await invoke("exec_cmd", { command, device_id: deviceId });
    } finally {
      un();
    }
    return;
  }
  const res = await fetch("/__ipc/exec", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ command, device_id: deviceId }),
  });
  if (!res.ok || !res.body) {
    const j = (await res.json().catch(() => ({}))) as { message?: string };
    throw new Error(j.message || "失败。");
  }
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    onChunk(dec.decode(value));
  }
}
