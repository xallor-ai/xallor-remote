# 操作系统差异

SSOT：Windows / Linux / macOS 行为。协议与 tool 名字相同；差异只在 Runtime。Relay 不解释路径。

---

## 1. 矩阵

| 能力 | Windows x64 | Linux x64 glibc | macOS |
| --- | --- | --- | --- |
| Runtime + MCP + CLI | v0 Must | v0 Must | v0.1 |
| TUI | 可选 | v0.1 Must（无 GUI） | 可选 |
| GUI | v0.1 Should | 可选 | v0.1 |
| 开机启动 | Should | Should | Should |
| SYSTEM/root 服务 | 不做 v0 | 不做 v0 | 不做 v0 |
| ARM | 以后 | 以后 | 以后 |
| WSL 当 Linux 目标 | 用 Linux 包 | — | — |
| 截屏 / 键鼠 | 不做 | 不做 | 不做 |

双 OS 都过才算支持。v0 无 GUI Linux 走 CLI，不依赖 TUI。

---

## 2. Shell

MCP **原样** 传 `command`。Windows 默认 `powershell.exe -NoProfile -NonInteractive -Command`（可配 pwsh/cmd）。Linux 默认 `bash -lc`，否则 `sh -c`。不翻译 bash↔PowerShell。cwd 相对 workspace。

---

## 3. 路径

Workspace 默认：`%USERPROFILE%\XallorRemote\workspace` / `~/XallorRemote/workspace`。打开限制用 `os.Root`：`..`、盘符跳转、junction / 逃出根的链接一律 `policy_deny`。写入全程在 Root 内完成，禁止先截断再当唯一检查。Windows 大小写不敏感（先规范化）；Linux 敏感。

---

## 4. 编码

上送永远 UTF-8。Windows 按控制台代码页解码再转（中文 stderr 不得乱码）。Linux 优先 UTF-8。二进制管道 v0 不承诺。

---

## 5. 进程列表

Windows：当前用户可见进程，pid + name。Linux：`/proc` 或 `ps`，pid + comm/cmdline 截断。不要求系统服务全家桶。

---

## 6. 默认 denylist（策略在 Runtime）

Windows：`%USERPROFILE%\.ssh`、凭据/DPAPI、浏览器 Profile、`\\.\`；`Format-Volume`、盘根递归删、`shutdown` / `Stop-Computer` / `Restart-Computer`。

Linux：`~/.ssh`、`~/.gnupg`、`/etc/shadow`、`/dev/sd*`；`rm -rf /`、`mkfs`、`dd of=/dev`、`shutdown`/`reboot`、开头 `sudo`。

错误码都是 `policy_deny`。

---

## 7. 安装形态

主路径：**一个 npm 包** `xallor-remote-mcp`（内嵌本平台 Runtime）。控制端配 mcp.json；被控端用同包的 `xallor-remote` 子命令。

备选：GitHub Releases 的单个 `xallor-remote` exe/二进制（无 Node）。Windows 注意防火墙出站与杀软。Linux amd64（deb 可选）；不依赖桌面；写明最低 glibc（如 Ubuntu 22.04+）。macOS：公证成本高，故 v0.1。

官方中转默认 `wss://api.xallor.com/remote`（与 Tab 的 `/api` 同域分流）。

---

## 8. 验收（每 Must OS）

按 [PRD.md](PRD.md) 四类。最低集：

**Identity：** ensure/start 有 ID、默认 workspace 已打印、无授权码、入站关；再 ensure 同一 ID。

**Transport：** 断网 offline、插回 online；换自托管 URL 仍能 exec。

**Authorization：** issue 后对端三项即可流式 whoami；错码 `unauthorized`；入站关 `inbound_disabled`；**从控制端不能 rotate/revoke**；本机 rotate 后旧配置失败。

**Execution：** 中文不乱码；结束前至少两帧 stdout；workspace 外 write 失败；write 超 1 MiB `too_large`；高危 `policy_deny`；cancel 自己的 `sleep`/`Start-Sleep` 得 `exit.status=cancelled`；无 DISPLAY 用 CLI 走完 peer/流/cancel。服务端闭环见 [dataplane.md](dataplane.md) 文末验收。

Linux 无头：审批类高危必须是 deny，不能挂起。
