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
| `hello_device` | device | device_id, secret, os, arch, agent_version, workspace | 证明设备身份；新设备默认关闭入站 |
| `hello_client` | client | target_device_id, grant?, account_token? | 未认领设备验证共享码，已认领设备验证账号访问权 |
| `hello_ok` | Relay | role；client 另必带 client_auth | hello 成功，client 验证认证确认后可 heartbeat / invoke |
| `heartbeat` | 双向 | — | 保活 |
| `invoke` | client | exec_id, op, payload | op 见下；目标取自该 client 连接的 hello |
| `invoke_nack` | Relay 或目标 | exec_id, code | 未开跑 |
| `device.settings` | **仅 device** | request_id, inbound；grant_hash 可省略 | 原子替换本连接设备的完整共享访问设置 |
| `device.settings.result` | Relay → device | request_id, code? | 持久化并应用后的确认；无 code 表示成功 |
| `revoke` | **仅 device** | device_id | 注销本机身份；踢线 |
| `access.request` | **仅 device** | request_id, account_token, payload | 管理本连接证明的设备归属与分享 |
| `access.result` | Relay → device | request_id, code? / payload? | 管理结果；成功为归属快照，失败为错误码 |
| `execution.renew` | **仅 device** | request_id, delegation | 复验本设备收到的后台执行委托 |
| `execution.route.renew` | **仅 device** | request_id, exec_id | 复验本设备仍存活的前台 exec 路由 |
| `execution.lease` | Relay → device | request_id, code? / lease_ms? | 短期执行许可；不确认任务结束或释放额度 |

client 连接上出现 `revoke` / `device.settings` → 忽略或 nack `unauthorized`，**不得执行**。

每条 client 连接的 `hello_ok` 必须携带 `client_auth:{mode:"shared"|"account"}`，由 Relay 在对应认证成功后生成。请求中提供 `account_token` 时必须确认 `account`，未提供时必须确认 `shared`；有效共享码不能替代失败的账号认证。Runtime 和诊断探针必须检查 type、client role、无错误 code 及匹配的 mode，缺失、未知或不匹配的确认一律拒绝，不缓存连接、不发后续 invoke，也不报告授权正常。所有 client 使用这一个明确合同，不接受只有 role 的普通成功回执。确认不携带原始凭据，不替代后续每次调用的身份及访问权限复验。

`device.settings` 的 `request_id` 非空且最多 128 字节；`inbound` 必须明确提供。`grant_hash` 为 64 位小写十六进制 SHA-256；省略或空字符串表示没有授权码，仅允许入站关闭，也会清除现有共享码。整个快照在一个存储事务中替换，不能分别应用开关与授权码；设备身份由实际连接推导。非法快照返回 `device.settings.result` + `agent_error`，不改变状态；存储结果不确定时停止旧路由并关闭连接，不发送成功确认。确认绑定请求 ID 和连接实例，不是可跨连接重放的操作凭据。

每条 device 连接在 `hello_ok` 后必须先提交完整设置；Relay 排入成功确认后才允许新调用路由到该连接，不能依赖上次连接的入站值提前执行。Runtime 在初始确认后才报告本机连接在线。重新连接同步本机保存的期望快照，不重新签发授权码。`revoke` 的注销语义独立于设置确认。

成功的 `device.settings.result` 和 `access.result` 必须携带 `device_context:{authority,device_uid,ownership_id?,grant_id?,inbound}`，由 Relay 的已提交设备状态生成，仅返回 device 连接。authority/device_uid 为 32 位小写十六进制；ownership_id/grant_id 非空时格式相同且互斥，未签发共享授权的设备允许两者均空。Runtime 在初始快照有效前不上线，后续先应用快照再交付管理回执或处理后续 invoke。handoff 的 scope 须匹配其 authority、设备实例和归属/共享授权实例，且 inbound 为 true。快照不是主体权限凭据，不替代每次调用的账号与操作授权。

Relay 下发 `handoff.submit` 时附带 `delegation`，客户端自报值被丢弃。它是固定用于后台交接复验的 HMAC-SHA256 委托，绑定签发方、设备显示号码/内部实例、原始共享/归属实例、账号凭据与成员版本及分享快照，不包含明文凭据、命令或 prompt。目标不得仅凭委托启动：须通过当前已认证 device 连接完成首次 `execution.renew`。委托至多 4096 字节，有效期 11 分钟且不续发，原始凭据或分享先到期时仍提前失效。续期请求 ID 非空且至多 128 字节，回复匹配请求与连接实例，其他设备和 client 角色不能使用它。

前台 `exec` 在启动进程前通过 `execution.route.renew` 获取同样的短期许可。`exec_id` 是 Relay 生成的路由 ID，非空且至多 128 字节；`request_id` 使用同一限制，不能携带 `delegation`。只允许当前目标 device 连接为尚未取消/结束的 exec 路由续期。原始调用绑定保留在 Relay 内存中，续期不能自报或替换账号、分享及操作；存储复验后再次检查相同路由和双方连接。前台不受后台委托的年龄上限限制，`timeout_ms=0` 继续表示不设执行期限；连接断开仍取消、不恢复或重放旧命令。首次拒绝返回 `invoke_nack/unauthorized`，首次交换故障或过晚返回 `invoke_nack/relay_error`，原调用取消/超时分别保留 `cancelled`/`exec_timeout`。运行中授权丢失在进程清理和占用释放确认后返回 `exit.status=authorization_lost`，保留实际退出码、耗时、截断标记和已输出内容；清理失败仍使用原错误，不伪造成功退出。

`execution.lease` 成功含正数 `lease_ms`，至多 20000 毫秒，且不超过委托、原始凭据或分享的剩余有效期；拒绝返回 `unauthorized`，存储/链路故障为 `relay_error` 或没有回复。Runtime 在发出请求前记下单调时间，从该时点计算到期，网络耗时不得加到许可期限上；正常每 5 秒复验，单次最多 5 秒且不能越过现有许可到期。明确拒绝取消作业；故障不续期，到期取消，不能迟到恢复。凭据续签/重新分享不替换已运行任务绑定的原始委托。委托仅驻内存，不进入回执；无首次许可为 `unknown/execution_authorization_unconfirmed`，运行中丢失许可为 `unknown/execution_authorization_lost`，回收或落盘未确认仍优先使用原有诊断。这是执行授权期限，不是商业资源租约、计费回执或 OS 沙箱。

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

Relay 在通过准入后重新构造发往 device 的非 cancel `invoke`，保留 `op,payload`，为 `exec_id` 分配新的内部调用 ID，添加服务端推导的 `scope`，不转发客户端的身份或凭据字段。`scope` 为 `{authority,device_uid,ownership_id?,grant_id?,principal_id?,tenant_id?}`；所有 ID 为 32 位小写十六进制。`ownership_id` 与 `grant_id` 恰有一个：已认领设备使用归属实例并要求主体/租户；未认领使用共享授权实例，账号主体/租户可同时存在或同时缺省。authority 来自当前 Relay 的持久身份。客户端自报 scope 被覆盖，目标仅信任已认证 Relay 连接。

scope 是稳定执行命名空间，不是访问凭据或权限快照。凭据续签、成员代次或重新签发同主体分享不改变它；新的设备 UID、归属实例或未认领共享授权实例会改变它。每次新调用仍独立完成现有认证和操作授权。handoff 收到缺失或非法 scope 返回 `unauthorized`，不回退设备级查询。原连接的 cancel 仍按 inflight 归属路由，不需要客户端传 scope。

`info` 的 JSON 结果包含 `build`：version、revision、built_at（可省略）、dirty、protocol（当前为 1）、profile_format（本机持久化格式）、os、arch；用于区分运行制品与代码版本。未知 revision 为 `unknown`，不猜测提交。Relay health 返回同形构建身份。

`info.shell` 为 `{configured,selected?,available:[],code?}`：configured 为目标配置名或 auto，selected 是当前能找到的解释器名，available 是本系统受支持且可找到的名字；不能解析选择时返回 code，不伪造 selected。只探测可执行文件，不包含路径、版本或远程凭据。可选解释器及执行语义见 [os.md](os.md)。

- `exec`：`script: {text, shell?}` 或 `program: {executable, args?}` 二选一，另有 `cwd?`、`timeout_ms?`。`text` / `executable` 必须非空；args 为字符串数组，省略时无参数。script.shell 省略时使用目标配置，显式 auto 使用目标自动选择。cwd、程序路径及 PATH 均属于目标设备；文本、参数不得含 NUL 或非法 Unicode，未知字段与非法形状返回 `invalid_execution`。可选字段应省略，不使用 null。CLI、IPC 和 MCP 使用同一输入形状，IPC/MCP 另带路由字段 device_id。脚本不跨语言翻译，程序参数不拼接成脚本。具体目标系统约束见 [os.md](os.md)。
- `cancel`：`exec_id`。只取消**本条 client 连接**登记的 inflight；对别人的或未知 id 一律 `unknown_exec`（不区分「存在但是别人的」）
- `read`：路径 + `head`/`tail`（行）或 offset/limit（字节）
- `write`：路径 + **完整** `content`。覆盖；无 append、无 patch

`info.execution_modes` 返回 `["script","program"]`，供 Agent 选择输入方式。示例：

```json
{"script":{"text":"Get-Location","shell":"powershell"},"timeout_ms":30000}
{"program":{"executable":"git","args":["status","--short"]},"cwd":"repo"}
```

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

`exit.status`：`completed` | `cancelled` | `timeout` | `authorization_lost`。后三者均为已启动且已停止的非成功结果，CLI 返回非零、JSONL 保留 `exit` 与原状态，MCP 返回 `isError=true` 并保留输出与终态信息。`authorization_lost` 区别于用户取消、执行超时和启动前拒绝，不代表尚未执行，也不允许自动重试。

`exec_id`：每条连接上的请求关联值。控制侧 Runtime 为新操作生成随机 ID；Relay 为每次准入另生成独立的随机内部 ID，以该值下发 device，并在回事件时还原调用者原值。目标只回显它收到的 ID。不同 client 连接可以使用相同值；同一连接同时重复使用返回 `agent_error`，不替换原调用。cancel 按原 client 连接与其关联 ID 解析，内部 ID 不能作为控制侧取消凭据。

稳定主体 `scope`、handoff 的 `request_id` 和本次传输的 `exec_id` 各司其职：续签后查询/去重保留前两者，新的提交/查询仍使用新的内部调用 ID。路由清理后旧结果丢弃，不因后续请求复用控制侧 ID 而重新关联。控制端应为每个新操作生成新 ID；同一连接主动复用已结束 ID 时，cancel 指向该 ID 当前的在途调用，不区分客户端滞留的旧取消意图。

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
| `result_unknown` | 控制端未收到有效终态、响应标识不匹配或终态字段矛盾；不表示未执行，不自动重试 |
| `runtime_unavailable` | 执行头无法启动或连接本机 Runtime，本次尚未发送执行请求 |
| `invalid_execution` | 执行输入形状、文本或参数无效 |
| `program_unavailable` | 目标无法安全找到指定程序；不回退到 Shell |
| `unsupported_program` | Windows program 模式选择了批处理文件或 cmd.exe，须改为显式脚本调用 |
| `unsupported_shell` | 目标系统不支持配置的解释器名 |
| `shell_unavailable` | 目标无法安全找到所选解释器；不会换壳重试 |
| `quota_exceeded` | Relay 或目标 Runtime 的资源上限已满（如分享数、在途请求或本机作业容量） |
| `workspace_missing` | workspace 目录不可用 |
| `too_large` | write/read、执行输入或目标命令行超过硬上限 |

MCP / CLI 必须用这些 code，再附一句人话。禁止只回 HTTP 502。

### CLI 执行事件输出

`exec --jsonl` 在标准输出逐行发送 JSON；文本模式和 JSONL 共用同一 IPC 结果解析器。这是客户端展示合同，不新增 WSS 消息或调度状态。事件形状：

```json
{"type":"stdout","exec_id":"ex_…","data":"first\nsecond\n"}
{"type":"stderr","exec_id":"ex_…","data":"diagnostic\n"}
{"type":"exit","exec_id":"ex_…","status":"completed","exit_code":7,"duration_ms":12,"truncated":false}
{"type":"error","exec_id":"ex_…","code":"relay_error","message":"目标连接已断开。"}
```

`exit` 和 `error` 是互斥终态，示例最后两行属于不同调用。可写输出通道上恰有一个终态且位于最后；管道关闭、CLI 被终止或参数语法解析失败时可能没有完整终态，调用者必须视为结果未确认。不得把 EOF 当成功。命令产生的 JSON/换行只放入 data，不作为事件解析；输出边界是块，不承诺一帧一行命令输出或 stdout/stderr 之间的真实时间顺序。

`exec_id` 仅在已观测时出现，可能随空 stdout 事件先到达；这不是目标进程已启动的证明。exit 字段沿用本协议终态语义，缺失的可选元数据省略，不伪造 0/false；completed 缺少有效整数退出码时转为 result_unknown。错误保留已知协议 code，不根据一般传输错误推断命令是否开跑。发送失败也可能已部分发送，所以返回 result_unknown。

本地语义输入校验失败为 invalid_execution/too_large；Runtime 启动或连接失败为 runtime_unavailable。非法选项、help 等 Cobra 语法/帮助流程不属于执行事件流，仍走普通诊断/帮助输出。进程退出状态仅在 completed 且 exit_code=0 时为 0，其余为非零；它不是远端原始退出码透传，远端值读取 exit_code。诊断可写入 CLI stderr，但远端 stderr 在 JSONL 中只作为标准输出上的 stderr 事件。输出写入失败立即停止并关闭 IPC，不补写成功或重试执行。

---

## 5. 大小（数字只在这里改）

| 项 | v0 |
| --- | --- |
| WSS 单帧 `data` | 64 KiB（解码后）；超则丢该数据帧并标 truncated |
| WSS 读上限 | `MaxWSMessageBytes` = 6 × (write 上限 + 路径上限) + 8 KiB 信封；覆盖 JSON `\u00XX` 最坏转义 |
| 路径 | 最长 4096 字节；超则 `policy_deny` |
| 单次 stdout+stderr | **4 MiB** 后丢后续输出，`exit.truncated=true` |
| Relay 每 inflight 待发缓冲 | **1 MiB**；超则丢数据帧并 truncated，不堵整条 device 连接 |
| Relay 每连接待发队列 | 最多 256 条等待帧，保留 32 个非输出槽；总内存计量上限 `4 MiB + MaxWSMessageBytes`（含正在写的帧），不随作业容量或会员额度扩大 |
| Relay 单次写入 / 关闭排空 | 各最多 5 秒；超时关闭连接并按断线规则回收，不无限等待 |
| Relay 身份操作 | 5 秒 context 期限，包含同设备协调器和数据库连接池等待；断线触发取消，驱动中的短 busy wait 返回后结束操作 |
| Relay 接收缓冲 | 64 帧，输出保留 32 个控制槽；排队字节计量上限 `MaxWSMessageBytes + MaxRelayBufferBytes`，另有一条正在解码及一条正在处理的消息 |
| Relay 控制字节预留 | 接收/待发队列各从输出可用预算中预留 **128 KiB**；控制消息本身仍服从队列总上限，超载仍会断开 |
| read 默认 / 上限 | 64 KiB / 1 MiB；少给则 `exit.truncated=true`，按 rune 边界切 |
| read `offset` | 字节起点，可大于 1 MiB；落在 rune 中间则回退到该 rune 起点 |
| **write 上限** | **1 MiB**（解码后）；整包 marshal 超 WSS 读上限则 `too_large`，不落盘 |
| Relay 每设备在途请求 | **16**，所有非 cancel 操作合计，跨 client 连接共享；cancel 不占新名额。此项是传输预算，不是会员或本机作业额度 |
| Runtime 本机作业占用 | 前台 exec 与后台 handoff 合计 **4**；其中 handoff 最多 **2**，不确定占用也计入；独立于 Relay inflight |
| exec 输入 | 解码后的 cwd、脚本文本及 shell 名，或 cwd、程序名及全部参数，合计最多 **64 KiB UTF-8**；最多 **256** 个程序参数；超则 `too_large` |
| exec JSON | 最多 `6 × 64 KiB + 8 KiB`，包含 IPC 路由字段；超则 `too_large` |
| exec timeout_ms | 整数 **0–3600000**；0 或省略不设执行期限；非法值 `invalid_execution` |
| Windows 命令行 | 解释器启动命令预估长度必须小于 **32767 UTF-16 单元**；原生程序经参数转义后连同结尾 NUL 不得超过此上限，否则启动前返回 `too_large`；OS 的实际启动限制仍有效 |
| 本机作业账本 | jobs.json 最多 **16 KiB**；内部关联键最多 **128 字节**；仅保存仍占用或不确定记录 |
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
