# MCP 头

依赖门禁：MCP SDK 固定为 `@modelcontextprotocol/server@2.4.0`，Zod 固定为 `4.6.7`，提交配套 lockfile。CI 使用 `npm ci`、`npm test`、`npm run build` 和 `npm audit --omit=dev --audit-level=high`；生产依赖出现 high/critical 漏洞时阻止合并和发行。升级不改变工具参数、结果或 Runtime IPC 语义。

SSOT：mcp 配置与 tools。ensure 见 [ipc.md](ipc.md) / [model.md](model.md)。环境变量名见 [decisions.md](decisions.md)。官方 SDK 见 [stack.md](stack.md)。

---

## 1. 它是什么

Cursor 等 spawn 的短驻 stdio 进程：全局安装后的 `xallor-remote-mcp`（或已发布后的 `npx -y xallor-remote-mcp`）。包内带本平台 Runtime（`vendor/`），ensure 时优先用；禁止静默联网下载。经 IPC 发任务；不持有到 Relay 的长期 WSS；不签发、不轮换、不吊销 grant，也不开关入站。用官方 MCP SDK，不要自实现协议。

被控端也可只装同一包：`xallor-remote start` / `grant issue` / `inbound on`（`bin` 入口转发到内置二进制）。

---

## 2. 配置

先安装：`npm install -g xallor-remote-mcp-*.tgz`（或上架后的包名）。**未发布到 registry 时不要用 `npx -y`**（会 404）。

单台覆盖（Runtime 收编进 peers）。Relay URL 可省略，默认官方 hosted `wss://api.xallor.com/remote`。

```json
{
  "mcpServers": {
    "xallor-remote": {
      "command": "xallor-remote-mcp",
      "env": {
        "XALLOR_REMOTE_DEVICE_ID": "774758105",
        "XALLOR_REMOTE_DEVICE_GRANT": "xr_grant_…"
      }
    }
  }
}
```

`XALLOR_REMOTE_DEVICE_ID` 填对方的 **9 位设备 ID**（`xallor-remote status` 里那串数字），不要填操作系统用户名。对方若从用户名 ID 升上来，以对方当前 `status` 为准，旧用户名配不上。

自托管时再加 `XALLOR_REMOTE_RELAY_URL`。多台用 `xallor-remote peer add`。不要写 IP / SSH。覆盖内置二进制时设 `XALLOR_REMOTE_BIN`。

可选：`XALLOR_REMOTE_LOG_LEVEL`。Runtime 对 `wss://` 使用系统根证书验证服务端；私有 CA 须先配置系统信任。没有跳过 TLS 校验的环境变量；本机明文开发使用 loopback `ws://`。

缺 grant 要在第一条 tool call 报错。不要把 mcp.json 提交进 git。启动时不要往 stderr 打版本横幅（Cursor 会当成 error）。

---

## 3. Tools（名字冻结）

| Tool | 作用 |
| --- | --- |
| `remote.devices` | peers + 在线状态 |
| `remote.device_info` | 单台；只配一台可省 id；返回目标 `build`、`shell`、`execution_modes`、`execution_backend` 与 `operation_policy` |
| `remote.exec` | **流式**；script / program 二选一，cwd 默认对方 workspace；输入见 [protocol.md](protocol.md) |
| `remote.exec_cancel` | exec_id，幂等 |
| `remote.read` / `remote.write` | 路径原样传递 |
| `remote.processes` | 只读 |

不提供签发 / 轮换 / 吊销 grant、不开入站。`remote.exec_cancel` 只能取消本 MCP 会话经 Runtime 发起的 exec（最终仍受 protocol：按 client 连接隔离）。

`write`：整文件覆盖、≤1 MiB、原子替换，见 [protocol.md](protocol.md) / [runtime.md](runtime.md)。`read` 的 `head`/`tail` 按行，`offset`/`limit` 按字节；默认 64 KiB。截断时 `truncated=true`，MCP 附一句「不是全文」。`exec` 结果含 `status`、`exit_code`、`duration_ms`、`truncated`、`exec_id`。非零退出、取消、超时、结果未确认 → `isError: true`；中断仍保留已接收的输出。确定性规则见 [ipc.md](ipc.md)。

---

## 4. 流式

一条 `remote.exec` 从 invoke 活到 exit，不另开 start/wait。客户端带了 `progressToken` 才发 progress（首帧 `exec_id`，随后 stdout/stderr）；没有 token 就不装能看见中间输出。运行中取消走宿主 cancellation，落到 Runtime `exec.cancel`。`remote.exec_cancel` 不是 stdio 并行主路径。

内部事件见 [protocol.md](protocol.md)。转发过程见 [dataplane.md](dataplane.md)。**Runtime↔Relay 必须按数据平面逐帧流**，禁止等 B `run()` 完再整包。
