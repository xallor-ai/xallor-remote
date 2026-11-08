# 产品决策（已拍板）

品牌、实现选型、v0.6 安全契约只在这里写结论。范围仍见 [PRD.md](PRD.md)。

---

## 1. 品牌与名字

| 用途 | 取值 |
| --- | --- |
| 产品名 | **XallorRemote** |
| CLI / 单二进制 | `xallor-remote` |
| npm MCP 包 | `xallor-remote-mcp` |
| mcp.json server 键 | `xallor-remote` |
| 环境变量前缀 | `XALLOR_REMOTE_` |
| 授权码前缀 | `xr_grant_` |
| 本机数据 | Windows `%APPDATA%\XallorRemote\`；Linux `~/.config/xallor-remote/` |
| 默认 workspace | Windows `%USERPROFILE%\XallorRemote\workspace`；Linux `~/XallorRemote/workspace` |
| MCP tools | 仍用 `remote.*`（对模型是能力名，不是品牌名） |

不要用 `rdr`、`rdr-agent`、`Remote Device Runtime`、`XalloRemote` 当对外名字。

---

## 2. Relay：官方 hosted 为默认，自托管始终能换

主路径是「无公网 IP、无 SSH」。若 v0 只做自托管，用户仍要先搞一台可达的 VPS，五分钟故事不成立。

**拍板：**

- 客户端 **默认** `XALLOR_REMOTE_RELAY_URL=wss://api.xallor.com/remote`。
- 和 Tab 同属 Xallor、同一台 `api.xallor.com`：`/api` 是账号/壁纸 API；`/remote` 是 Remote 中转（WSS）。不要为 invoke 另开 HTTP。
- 身份与设备授权基础可与 Relay 同进程运行，当前实现边界以 [platform.md](platform.md) 为准；托管注册/登录、订阅与组织管理界面另行产品化。连接数与流量硬顶是传输保护，超限 `quota_exceeded`，不是会员资源额度。
- **自托管一等公民**：本机 `xallor-remote relay --listen 127.0.0.1:8443 --data <dir>`；公网配置内置 TLS 或可信 HTTPS 反代（见 [relay.md](relay.md) §5），改 URL 即切走。数据不锁死在云上。
- Relay 行为契约（连接、inflight、流、cancel、断线、背压）只在 [dataplane.md](dataplane.md)，本文不重复。
- 官方域名未上线前，dogfood 用自托管 URL；**产品默认仍按官方 hosted 写**，不要改回「v0 只能自托管」。
- 后续在现有身份授权基础上增加 hosted 登录入口、设备清单与订阅。未认领设备保留共享码入口；认领后不能用共享码绕过账号访问权，见 [credentials.md](credentials.md)。

---

## 3. 本机 IPC

**拍板：按操作系统用该平台惯用本地 IPC，产品层同一套 API，禁止对 localhost 开一个随便端口。** 报文见 [ipc.md](ipc.md)。

| OS | 机制 |
| --- | --- |
| Windows | Named pipe `\\.\pipe\XallorRemote` |
| Linux / macOS | Unix socket：优先 `$XDG_RUNTIME_DIR/xallor-remote.sock`，否则 `~/.config/xallor-remote/ipc.sock` |

权限：仅当前用户。MCP / CLI / TUI / GUI 都连这个口，不各自实现一套。

---

## 4. 技术栈

无头 Linux 必须是「一个二进制」，不能要求先装 Node 才能被控。MCP 又必须进 npm。

**拍板：**

| 部分 | 选型 | 理由 |
| --- | --- | --- |
| Runtime + Relay + CLI + TUI | **Go** | 交叉编译、单文件、systemd 友好 |
| MCP 头 | **TypeScript**（`xallor-remote-mcp`） | Cursor 生态、ensure 时拉起 Go Runtime |
| GUI v0.1 | **Tauri 2** | 小体积、调本机 Runtime；不做 Electron 远控壳 |

TUI 做进同一 Go 二进制（`xallor-remote tui`），不另发一套 Node TUI。库、发行、复用清单见 [stack.md](stack.md)。头与 Runtime 关系见 [heads.md](heads.md)。

---

## 5. Workspace

拒绝启动会挡住「先上线、再 issue」；没有根目录又会让 exec 落在盘符根上。

**拍板：永远有一个显式默认沙箱，启动时打印；不许静默使用 `/` 或 `C:\`。**

- 首次启动若未指定 `--workspace`，创建并使用默认目录（见 §1）。
- 每次 `start` / `status` 打印当前 workspace。
- `--workspace` 或 `config.json` 可改；改完只影响后续 invoke。
- 目录被删：重建默认或报 `workspace_missing`，不要退回用户主目录根。
- 路径穿越仍 `policy_deny`。

---

## 6. 安全契约（v0.6）

细节在 [credentials.md](credentials.md) / [protocol.md](protocol.md) / [runtime.md](runtime.md) / [relay.md](relay.md)。这里只锁结论：

1. Grant = bearer capability，不绑控制端。
2. rotate/revoke/inbound **仅被控机本机**，经 device 连接。
3. Relay 的身份持久化与载荷不落盘边界见 [relay.md](relay.md)；转发路径不承担命令审计。
4. Policy 是 workspace + denylist 减伤，不是命令沙箱；无头审批 → deny。
5. write：整文件、1 MiB、原子覆盖。
6. Hosted Relay 看得见飞行载荷；不落盘；有每 IP 预算。
7. cancel 只能打本 client 连接上的 exec_id。
