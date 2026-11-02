# 协议合同（v0）

SSOT：消息语义、错误码、角色。建立连接、inflight、流式、cancel、断线、背压的**过程**只在 [dataplane.md](dataplane.md)。谁能改授权见 [credentials.md](credentials.md)。

JSON 字段名实现时可加前缀，**type、code、角色不得改义**。本文不是加密规格。

---

## 1. 两条连接

| role | 谁发起 | 证明 | 用途 |
| --- | --- | --- | --- |
| `device` | 被控 Runtime | `device_id` + `device_secret` | 上线、收 invoke、回事件、**改授权** |
| `client` | 控制侧 Runtime | 目标 `device_id` + 对应访问凭据（见下） | 发 invoke、收事件；不能管理设备授权 |

同一 Runtime 可以同时有一条 `device` 连接和多条 `client` 连接。**v0：一条 client 连接只绑定一个目标**（`hello_client` 时定死）。多 peer = 多条 client 连接，不在一条 WSS 上多路复用。

同一 `device_id` 只允许一条 `device` 连接：新的踢旧的。

---

## 2. 控制面消息

| type | 谁可发 | 必带 | 含义 |
| --- | --- | --- | --- |
| `hello_device` | device | device_id, secret, os, arch, agent_version, workspace, inbound | 上线 |
| `hello_client` | client | target_device_id, grant?, account_token? | 未认领设备验证共享码，已认领设备验证账号访问权 |
| `hello_ok` | Relay | role | hello 成功，此后可 heartbeat / invoke |
| `heartbeat` | 双向 | — | 保活 |
| `invoke` | client | exec_id, op, payload | op 见下；目标取自该 client 连接的 hello |
| `invoke_nack` | Relay 或目标 | exec_id, code | 未开跑 |
| `grant_rotate` | **仅 device** | device_id, new_grant_hash | 作废旧 grant |
| `revoke` | **仅 device** | device_id | 注销本机身份；踢线 |
| `inbound_set` | **仅 device** | device_id, inbound | 开/关入站 |
| `access.request` | **仅 device** | request_id, account_token, payload | 管理本连接证明的设备归属与分享 |
| `access.result` | Relay → device | request_id, code? / payload? | 管理结果；成功为归属快照，失败为错误码 |

client 连接上出现 `revoke` / `grant_rotate` / `inbound_set` → 忽略或 nack `unauthorized`，**不得执行**。

`account_token` 用于 client hello 和 device 的授权管理请求，非空时必须通过签发方校验，失败返回 `unauthorized`，存储故障返回 `relay_error`，不能降级为无账号访问。未认领设备必须出示目标 grant；已认领设备忽略 grant，只允许所有者或明确获授权且有效的成员。主体、空间、成员版本和到期时间由 Relay 从凭据推导，不接受客户端自报身份。凭据不转发给其他设备，也不写入调用记录。账号凭据有效期由本机运维设置为 1 分钟至 90 天，默认 24 小时。

`access.request` 的 `request_id` 非空且不超过 128 字节，`payload` 不超过 16 KiB；未知 action 或非法参数返回 `agent_error`。payload 为以下之一：

| action | 参数 | 作用 |
| --- | --- | --- |
| `claim` | 无 | 将未认领设备绑定到当前账号的空间及主体 |
| `status` | 无 | 查看归属及分享；已认领时仅所有者可查询 |
| `share` | membership_id, operations, ttl_seconds | 对有效成员签发或替换有期限的操作授权 |
| `unshare` | share_id | 删除本设备的指定分享 |
| `transfer` | membership_id | 转移到有效成员所属的空间及主体，删除所有旧分享 |

`operations` 仅接受 `info`、`read`、`write`、`exec`、`processes`、`handoff.submit`、`handoff.get` 的非空集合；cancel 始终只属于原连接，不是可授予他人的权限。分享期限为 60 至 7776000 秒，最多 128 份未过期分享，超限 `quota_exceeded`。成功快照为 `{device_uid,ownership_id?,owner_tenant?,owner_principal?,shares:[{id,membership_id,operations,expires_at}]}`，时间为 Unix 秒。角色或权限不符返回 `unauthorized`。设备身份取自实际 device 连接，忽略客户端自报目标；client 连接不能调用此入口。

`reset` 不是协议消息：CLI 清空本机数据目录，并发送 `revoke`。

`invoke` 的 `op`：`exec` / `read` / `write` / `processes` / `info` / `cancel` / `handoff.submit` / `handoff.get`。

`info` 的 JSON 结果包含 `build`：version、revision、built_at（可省略）、dirty、protocol（当前为 1）、os、arch；用于区分运行制品与代码版本。未知 revision 为 `unknown`，不猜测提交。Relay health 返回同形构建身份。

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
| Relay 每连接待发队列 | 最多 256 条等待帧，保留 8 个非输出槽；总内存计量上限 `4 × MaxRelayBufferBytes + MaxWSMessageBytes`（含正在写的帧） |
| Relay 单次写入 / 关闭排空 | 各最多 5 秒；超时关闭连接并按断线规则回收，不无限等待 |
| Relay 身份操作 | 5 秒 context 期限，包含同设备协调器和数据库连接池等待；断线触发取消，驱动中的短 busy wait 返回后结束操作 |
| Relay 接收缓冲 | 64 帧，输出保留 8 个控制槽；排队字节计量上限 `MaxWSMessageBytes + MaxRelayBufferBytes`，另有一条正在解码及一条正在处理的消息 |
| read 默认 / 上限 | 64 KiB / 1 MiB；少给则 `exit.truncated=true`，按 rune 边界切 |
| read `offset` | 字节起点，可大于 1 MiB；落在 rune 中间则回退到该 rune 起点 |
| **write 上限** | **1 MiB**（解码后）；整包 marshal 超 WSS 读上限则 `too_large`，不落盘 |
| 每设备并发 exec | **4** |
| 排队 | 不排队，超额 `quota_exceeded` |
| heartbeat | 30s 发一拍；60s 无任何帧 → 连接死 |

## Cursor 交接载荷与回执

这是应用层试点，沿用 `invoke → stdout → exit`，不新增 Relay 状态机。`handoff.submit` 的 payload 为 `{request:{...}}`，`handoff.get` 为 `{request_id}`；IPC 另带路由字段 `device_id`。payload 最大 64 KiB。结果 stdout 拼接为 `{receipt:{...}}` 或 `{error:"人话"}`，外层 exit 只确认本次提交/查询。

请求示例（路径、SHA、会话 ID 必须替换为目标真实值）：

```json
{"request_id":"review-001","workspace":"C:\\XallorRemote\\workspace\\repo","revision":"0123456789012345678901234567890123456789","agent":"cursor","session_id":"12345678-1234-1234-1234-123456789abc","mode":"ask","prompt":"Review the selected revision.","attachments":[],"trust_workspace":true,"timeout_seconds":120}
```

`request_id` 为 1–64 位字母数字、下划线、连字符，首位字母数字；revision 为 40 位小写十六进制 Git SHA；session_id 为 UUID；agent 仅 cursor；mode 必须为 ask / plan / agent。workspace 为绝对路径，最长 1024 字节，必须解析到目标 Runtime workspace 内的干净 Git 仓库根且 HEAD 匹配。prompt 为 1–16 KiB UTF-8。attachments 最多 8 个 `{path,sha256}`：相对路径最长 1024 字节，解析后仍在仓库内，常规文件最大 1 MiB，SHA256 必须匹配。timeout_seconds 为 1–600，包含预检；预检本身最多 45 秒，并服从更短的请求总超时。预检超时回执 code 为 `preflight_timeout_<phase>`，phase 标识 workspace、git_root、git_revision、git_status、attachments、adapter_config、cli_help 或 cli_version；取消为 `preflight_interrupted`。

回执字段：`request_id,fingerprint,workspace,revision,agent,session_id,mode,state,code?,created_at,updated_at,cli_version?,result?,truncated`。fingerprint 为规范化请求 JSON 的 SHA256；时间为 UTC RFC3339Nano。state：received / accepted / running / completed / failed / unknown，语义与恢复步骤见 [handoff.md](handoff.md)。回执 code 为适配器诊断，**不是** WSS invoke_nack code。

每 Runtime 最多 2 个在途交接，每会话最多 1 个；最多保留 128 个回执，达到上限拒绝新 ID，不自动淘汰。单回执磁盘上限 512 KiB；CLI 单行最大 1 MiB、累计输出最大 8 MiB；结果最多 64 KiB UTF-8，截断时显式标记。启动配置仅本机设置，最多 4 个固定前置参数，每项最长 2048 字节。
