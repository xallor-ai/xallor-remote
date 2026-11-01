# 协议合同（v0）

SSOT：消息语义、错误码、角色。建立连接、inflight、流式、cancel、断线、背压的**过程**只在 [dataplane.md](dataplane.md)。谁能改授权见 [credentials.md](credentials.md)。

JSON 字段名实现时可加前缀，**type、code、角色不得改义**。本文不是加密规格。

---

## 1. 两条连接

| role | 谁发起 | 证明 | 用途 |
| --- | --- | --- | --- |
| `device` | 被控 Runtime | `device_id` + `device_secret` | 上线、收 invoke、回事件、**改授权** |
| `client` | 控制侧 Runtime | 目标 `device_id` + **该目标的 grant** | 发 invoke、收事件；**不能** rotate/revoke/inbound |

同一 Runtime 可以同时有一条 `device` 连接和多条 `client` 连接。**v0：一条 client 连接只绑定一个目标**（`hello_client` 时定死）。多 peer = 多条 client 连接，不在一条 WSS 上多路复用。

同一 `device_id` 只允许一条 `device` 连接：新的踢旧的。

---

## 2. 控制面消息

| type | 谁可发 | 必带 | 含义 |
| --- | --- | --- | --- |
| `hello_device` | device | device_id, secret, os, arch, agent_version, workspace, inbound | 上线 |
| `hello_client` | client | target_device_id, grant | 我要控这台 |
| `hello_ok` | Relay | role | hello 成功，此后可 heartbeat / invoke |
| `heartbeat` | 双向 | — | 保活 |
| `invoke` | client | exec_id, op, payload | op 见下；目标取自该 client 连接的 hello |
| `invoke_nack` | Relay 或目标 | exec_id, code | 未开跑 |
| `grant_rotate` | **仅 device** | device_id, new_grant_hash | 作废旧 grant |
| `revoke` | **仅 device** | device_id | 注销本机身份；踢线 |
| `inbound_set` | **仅 device** | device_id, inbound | 开/关入站 |

client 连接上出现 `revoke` / `grant_rotate` / `inbound_set` → 忽略或 nack `unauthorized`，**不得执行**。

`reset` 不是协议消息：CLI 清空本机数据目录，并发送 `revoke`。

`invoke` 的 `op`：`exec` / `read` / `write` / `processes` / `info` / `cancel`。

- `exec`：`command`（字符串）、`cwd?`、`timeout_ms?`
- `cancel`：`exec_id`。只取消**本条 client 连接**登记的 inflight；对别人的或未知 id 一律 `unknown_exec`（不区分「存在但是别人的」）
- `read`：路径 + `head`/`tail`（行）或 offset/limit（字节）
- `write`：路径 + **完整** `content`。覆盖；无 append、无 patch

---

## 3. 数据面消息

| type | 方向 | 必带 |
| --- | --- | --- |
| `stdout` / `stderr` | device → client | exec_id, data（UTF-8 文本） |
| `exit` | device → client | exec_id, exit_code, duration_ms, truncated, status |
| `error` | 任一跳 | exec_id?, code | 通道失败，**不是** 命令非零退出 |
| `progress` | 可选 | 文件百分比；v0 可不上 |

顶层 `type` 就是 `stdout` / `stderr` / `exit`，不要包 `{type:event,event:stdout}`。**无 `seq` 字段**（理由见 [dataplane.md](dataplane.md)）。

非零退出只走 `exit`（`status=completed`）。每个 `exec_id` 恰好一个终态：`invoke_nack` **或** `exit` **或** `error`，然后 Relay 删 inflight。见 [dataplane.md](dataplane.md)。

`exit.status`：`completed` | `cancelled` | `timeout`。MCP/CLI 把后两者映射为 code `cancelled` / `exec_timeout`。

`exec_id`：控制侧 Runtime 生成 UUID，全程携带。

---

## 4. 错误码

| code | 何时 |
| --- | --- |
| `unauthorized` | grant 错或已吊销；或 client 试图改授权 |
| `unknown_device` | ID 不存在 |
| `unknown_exec` | cancel 的 exec_id 对本连接无效 |
| `device_offline` | 登记过但无 device 连接 |
| `inbound_disabled` | 在线但入站关 / 尚未 issue |
| `policy_deny` | Runtime 策略拒绝 |
| `approval_timeout` | 要确认但没人点 |
| `approval_required` | 仅 nack：还在等确认（可选） |
| `exec_timeout` | `exit.status=timeout`（已杀树并 wait） |
| `cancelled` | 本连接的 cancel：未 Popen 则 nack；已 Popen 则 `exit.status=cancelled` |
| `truncated` | 与 `exit` 同时出现的标志，不是单独终态。read 因默认窗口或上限少给的内容也标 true |
| `relay_error` | 中转故障或重启丢 inflight |
| `agent_error` | Runtime 内部失败 |
| `quota_exceeded` | 官方 Relay 硬顶 |
| `workspace_missing` | workspace 目录不可用 |
| `too_large` | write/read 超过硬上限 |

MCP / CLI 必须用这些 code，再附一句人话。禁止只回 HTTP 502。

---

## 5. 大小（数字只在这里改）

| 项 | v0 |
| --- | --- |
| WSS 单帧 `data` | 64 KiB（解码后）；超则丢该数据帧并标 truncated |
| WSS 读上限 | `MaxWSMessageBytes` = 6 × (write 上限 + 路径上限) + 8 KiB 信封；覆盖 JSON `\u00XX` 最坏转义 |
| 路径 | 最长 4096 字节；超则 `policy_deny` |
| 单次 stdout+stderr | **4 MiB** 后丢后续输出，`exit.truncated=true` |
| Relay 每 inflight 待发缓冲 | **1 MiB**；超则丢数据帧并 truncated，不堵整条 device 连接 |
| read 默认 / 上限 | 64 KiB / 1 MiB；少给则 `exit.truncated=true`，按 rune 边界切 |
| read `offset` | 字节起点，可大于 1 MiB；落在 rune 中间则回退到该 rune 起点 |
| **write 上限** | **1 MiB**（解码后）；整包 marshal 超 WSS 读上限则 `too_large`，不落盘 |
| 每设备并发 exec | **4** |
| 排队 | 不排队，超额 `quota_exceeded` |
| heartbeat | 30s 发一拍；60s 无任何帧 → 连接死 |
