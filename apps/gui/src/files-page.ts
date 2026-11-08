import { rpc, type Status } from "./bridge";
import { esc, feedback, form } from "./ui";

// Minimal workspace file entry: all reads/writes still go through the local
// Runtime IPC and target workspace policy. The GUI never opens a Relay socket.
export async function filesPage(root: HTMLElement, signal: AbortSignal): Promise<void> {
  const status = await rpc<Status>("status");
  if (signal.aborted) return;
  root.innerHTML = `<h2>文件</h2><p role="status"></p>
    <form id="file" class="stack wide"><label>对方设备<select name="device">${status.peers.map(peer => `<option value="${esc(peer.device_id)}">${esc(peer.name || peer.device_id)} · ${esc(peer.device_id)}</option>`).join("")}</select></label>
    <label>workspace 内路径<input name="path" required placeholder="notes.txt"></label>
    <label>内容<textarea name="content" rows="12" placeholder="读取后可编辑，再覆盖写入"></textarea></label>
    <div class="row"><button type="button" id="read" ${status.peers.length ? "" : "disabled"}>读取</button><button type="submit" ${status.peers.length ? "" : "disabled"}>覆盖写入</button></div></form>`;
  const formNode = root.querySelector<HTMLFormElement>("#file")!;
  root.querySelector<HTMLButtonElement>("#read")!.onclick = async () => {
    const button = root.querySelector<HTMLButtonElement>("#read")!;
    button.disabled = true;
    try {
      const data = new FormData(formNode);
      const result = await rpc<{ content?: string; truncated?: boolean }>("read", { device_id: String(data.get("device")), path: String(data.get("path")) });
      (formNode.elements.namedItem("content") as HTMLTextAreaElement).value = result.content || "";
      feedback(root, result.truncated ? "已读取前缀，内容已截断。" : "已读取。", false);
    } catch (e) { feedback(root, e); } finally { button.disabled = false; }
  };
  form(root, "#file", async data => {
    await rpc("write", { device_id: String(data.get("device")), path: String(data.get("path")), content: String(data.get("content")) });
    feedback(root, "已写入目标 workspace。", false);
  });
}
