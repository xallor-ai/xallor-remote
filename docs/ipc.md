# 本机 IPC（头 ↔ Runtime）

SSOT：命名管道 / Unix socket 上的报文与 ensure。路径与权限见 [decisions.md](decisions.md)。头关系见 [heads.md](heads.md)。Relay WSS 见 [dataplane.md](dataplane.md)。**IPC 不是 WSS 的同一套 type 表**——本地多了 grant/peer/审批，少了 hello_device。

---

## 1. 传输

| | |
| --- | --- |
| Windows | `\\.\pipe\XallorRemote` |
| Linux / macOS | `$XDG_RUNTIME_DIR/xallor-remote.sock`，否则 `~/.config/xallor-remote/ipc.sock` |
| 权限 | 仅当前用户（Unix `0600`；Windows 管道由 Runtime 使用受保护的当前 TokenUser DACL 创建） |
| 帧 | **一行一条压缩 JSON**（NDJSON）。对象内部换行必须转义，禁止 pretty-print |
| 禁止 | 在 localhost 再开一个随机 TCP 端口当「更好写」 |

同一用户多头可并行连。连不上 = Runtime 没起来，走 §5 ensure，不要改连 Relay。

Windows 的监听器在创建普通 IPC 和 lifecycle 管道时都会构造受保护的显式
单 SID DACL；获取当前用户 SID 或安全描述符失败则拒绝监听，不回退到系统默认
命名管道 ACL。这个边界保证的是本机监听端点的访问权限，不等同于客户端已经
完成 Runtime 服务端身份认证。事务交接另行使用内核 peer PID、进程镜像和持久
事务绑定做认证；普通 head 的服务端身份校验仍须使用公开、可验证的 OS 机制，
不能依赖 Node `net.Socket` 的私有句柄字段。

版本交接进程在旧 owner 退出期间提供固定只读生命周期端点：Windows 为当前
Runtime 管道名加 `-lifecycle`；Unix 为同目录 `txn.sock`（XDG runtime 默认路径使用
`xallor-txn.sock`）。它只处理 `runtime.operation.status` 与只读
`runtime.operation.reconcile`，不接受执行、安装或回滚请求。
只有持有 profile 生命周期锁的已认证事务进程可以监听；关闭时终止并等待所有查询连接。
新 owner 启动后可在普通 IPC 查询同一持久事务；两处返回相同记录，不从端点存在推断成功。
无 IPC 时的 `runtime status` 仍可只读观察 active 记录；unfinished 会阻止自动 bootstrap。
字段及结果语义见 [protocol.md](protocol.md)。

已有 Runtime 时，CLI `runtime install` / `runtime rollback` 在同一条通过 ready
握手的连接上只提交一次，显示事务 ID，随后只查询原 ID。连接未知或握手拒绝不能
退回本地安装。CLI `runtime status --operation <id>` 提供离线只读回执；终态是否
释放事务写锁由 settled 单独说明。明确无 owner 时，完整空选择的首次安装只执行一次
受信源引导；已有完整 Current 的冷升级／回滚先启动原选定 owner，再经 IPC 提交变更。
它们受同一生命周期锁和“锁内再次确认无 owner”的限制；并发出现的其他选择不能被
误报为请求 bundle 已安装。无 unfinished journal 的 legacy pending 回滚由受信
恢复角色执行：先重新验证原 Current，首次初始化失败且无 Current 时仅允许原 Pending
映像清除失败选择；都必须确认无服务、无运行锁及未释放作业。CLI 等待该次恢复子进程
退出后只读查询同一事务，不直接执行回滚状态机。已有 unfinished journal 的显式核对与
恢复尚未完成，继续拒绝自动重放，不应据此宣称整体接管验收已全部完成。

---

## 2. 信封

请求：

```json
{"id":"c1","method":"exec","params":{"device_id":"dev_B","program":{"executable":"whoami","args":[]}}}
```

后续与之关联的事件 / 结果带同一个 `id`：

```json
{"id":"c1","event":"stdout","data":"andy\n"}
{"id":"c1","event":"stderr","data":""}
{"id":"c1","ok":true,"result":{"exit_code":0,"duration_ms":12,"truncated":false,"status":"completed","exec_id":"ex_…"}}
```

失败（没跑起来或通道失败）：

```json
{"id":"c1","ok":false,"code":"device_offline","message":"目标不在线"}
```

约定：

- `id` 由头生成，单条 IPC 连接内唯一。
- 流式方法：0..n 条 `event`，**恰好一条**终态（`ok` true/false）。
- 非流式：没有 `event`，只有一条终态。
- `code` 与 [protocol.md](protocol.md) 同一套。`exec_id` 由 Runtime 生成（头不要自己编 WSS 用的 id）。

执行头只在收到匹配请求 ID 的明确终态、`ok=true`、`status=completed` 和数值退出码 0 时显示成功。缺失/非法 status、缺失/null/字符串退出码、矛盾的事件与终态字段均不能推断成功；没有终态就断开时显示结果未确认，不重放。`ok=true` 仅表示 Runtime 返回了执行结果，非零退出、cancelled、timeout 仍是任务失败或中断。CLI/TUI 共用 IPC 流解析，MCP 按相同确定性规则解释结果。

已收到的 stdout/stderr 不因非零退出、取消、超时或断线丢弃；已知的截断标志与失败状态同时呈现。MCP 中断回执保留已知任务 ID、状态和输出信息；缺失字段写明 unknown，不合成退出码 0。输出文本仍不是恢复或重复执行的凭据。
**单帧：** 默认 64 KiB（stdout 块）。`write` 的 `params.content` 允许单条 JSON 最大 1 MiB（与 [protocol.md](protocol.md) write 上限一致）。

`event` 取值：`stdout` / `stderr` / `approval`（可选）。不要在 IPC 上再包一层 WSS 的 `type=invoke`。

---

## 3. 方法

### 本机（不进 Relay）

| method | 谁可调 | 作用 |
| --- | --- | --- |
| `status` | 任一头 | ID、workspace、inbound、online、版本；build 为运行制品身份 |
| `doctor` | 任一头 | 只读分层诊断；`params.device_id` 可选，不省略时必须为 9 位 ID |
| `grant.issue` / `show` / `rotate` | 人侧头 | 见 [credentials.md](credentials.md) |
| `inbound.set` | 人侧头 | `{enabled:bool}` |
| `revoke` / `reset` | 人侧头 | reset 须 `params.confirm=true` |
| `peer.add` / `list` / `remove` | 任一头 | add 会收编 mcp.json 那套 id+grant |
| `peer.rename` | 任一头 | `{device_id,name}` 修改备注，最多 80 字符；空字符串清除，不改 ID 或授权 |
| `config.get` / `config.set` | 人侧头 | get 返回 `{relay_url,workspace,shell,execution_backend}`，未配置 shell 显示 auto，backend 默认 native_user；set 接受这四项，省略或空值不更新；backend 选择见 [protocol.md](protocol.md)，不支持时整项更新返回 backend_unavailable，错误不改变活动配置 |
| `account.set` | 人侧头 | `{token:string}`；必须明确提供，空字符串表示清除；返回 `{configured:bool}` |
| `account.status` | 人侧头 | 返回 `{configured:bool}`，不返回凭据或宣称已在线认证 |
| `device.access` | 本机管理头 | 归属/分享及 WIP 授权码管理参数、按动作区分的结果见协议；账号凭据由 Runtime 获取，头不得传入 token 或设备密钥 |
| `approval.subscribe` | 人侧头 | 注册为审批 UI；断开即取消 |
| `approval.respond` | 人侧头 | 独立请求 ID；`{approval_id,request_sha256,allow:bool}`，摘要不匹配或重复确认均拒绝 |
| `approval.list` | 人侧头 | 当前待审批的完整不可变 `details`、`request_sha256`、`created`、`expires`；只读，不代替订阅 |
| `audit.list` | 人侧头 | 本机最近 500 条操作元数据 `{entries:[...]}`；读取失败明确报错 |
| `runtime.ensure` | 任一头 | 请求唯一 Runtime owner 确保当前 profile 的已验证版本就绪；不返回可执行路径 |
| `runtime.status` | 任一头 | 读取 Runtime owner 的 ready/pending/unknown 状态、版本、摘要、PID、activation_id 和 launch_id |

MCP 提供 `status` / `peer.*` / 下面远程方法；不提供 grant/inbound/revoke/account.set
或设备归属/分享管理工具。当前 WIP 另有 `remote.device_code_revoke` 和
`remote.device_code_rotate`，只把对应 code action 送到 `device.access`，仍由 Relay
检查本设备 owner 身份。它们不是通用 device.access 工具，也不能输入账号 token。
Runtime 不做「MCP 指纹鉴权」——工具暴露面与 IPC 身份隔离是两件事。
MCP 连接**不算**审批 UI；这些管理工具尚未完成发行包和独立进程验收。

`device.access` 经当前 device 出站连接发送，校验连接 Relay 地址与当前凭据绑定地址一致；请求和响应关联到该连接实例。最多 16 个等待 Relay 结果的请求，每次 5 秒期限包含写入排队；断线/超时返回结果未确认，不自动重放管理动作。响应来自已认证 Relay，成功状态直接回到调用头。`peer.add` 的 grant 可以省略，供账号授权访问；添加 peer 不表示已通过远程授权验证。

`grant.issue`、`grant.rotate`、`inbound.set` 先保存本机期望设置，再等待当前 Relay 确认完整快照。只有确认后返回 `ok:true`；离线、断线、超时或存储结果不确定返回 `relay_error` 并提示尚未确认生效，本机已保存的设置仍保留。签发动作不自动重试，授权码可用 `grant.show` 读取。设置修改、重连初始快照、配置切换及本机撤机/重置共用串行边界，最多 16 个等待者，5 秒期限包含排队和发送；排队失败不修改设置。`status.online` 表示当前连接已完成初始设置确认，不表示后来未确认的修改已经生效。CLI、GUI、TUI 均沿用这一结果，不自行假定 Relay 已更新。

`account.set` 原子持久化到当前 profile 的 `account.json`，绑定当前 Relay URL。成功后递增本机身份代次并关闭旧控制连接，等待中的调用可能收到通道失败，不能自动重放。导入只校验凭据格式，实际有效性在远程握手和新调用时验证；设置失败保留旧配置。目标 `doctor` 使用与实际调用相同的身份凭据，不能匿名探测后冒充账号访问成功。

凭据文件损坏时 Runtime 仍启动本机恢复入口，所有需要该凭据的远程连接明确失败；只有成功重新导入或明确清除后才恢复调用，不自动按匿名方式访问。

`status.result.build`：version、revision、built_at、dirty、protocol、os、arch、path、sha256、pid。path/hash 在进程启动时采集，表示正在运行的制品，不把升级后的磁盘文件冒充当前进程。远程 info 的 build 不包含本机 path/hash/pid。

`status` 的 peers 仅含 `device_id`、`name`、`has_grant`，不返回授权码或 device_secret。`relay` 仅返回 scheme + host，去掉可能携带凭据的 userinfo/path/query/fragment；编辑地址仍走显式 `config.get`。

`doctor` 返回 `{schema:1, observed_at, device_id?, ok, checks:[{layer,state,code?,action?}]}`。固定层次：`ipc`、`runtime_relay`、`peer`、`relay_transport`、`authorization`、`target_online`、`inbound`、`target_runtime`。状态为 `ok` / `fail` / `unknown` / `skipped`；前置失败后的未测项不冒充成功。省略设备时仅测本机服务及其 Relay 连接；目标诊断创建一次独立 client 连接，hello 后只调用只读 `info`，以目标 ID 与成功终态验证目标响应，不执行 shell，不修改配对/授权/入站，不复用正在执行任务的连接。远程探测上限 5 秒。未返回终态只能表示未确认，诊断不承诺后续任务一定成功。

CLI `doctor [--device ID] [--json]` 不调用 ensure，也不创建数据目录；`status --json` 输出上述脱敏状态。doctor 全部已请求检查通过时退出 0，失败或未确认退出非 0；JSON 始终写 stdout，人话错误写 stderr。

### 远程（Runtime 再走 WSS）

| method | 对应 |
| --- | --- |
| `exec` | invoke op=exec；params 为 [protocol.md](protocol.md) 的 script/program 输入，另带 device_id；可带已批准的 `session_id` |
| `exec.cancel` | 仅取消 **这条 IPC 连接** 发起的 exec（Runtime 再映射到自己的 client 连接） |
| `read` / `write` / `processes` / `info` | 对应 op；可带已批准的 `session_id` |
| `handoff.submit` / `handoff.get` | 对应 op，`params.device_id` 必填；可选 `params.session_id` 绑定已批准的逻辑授权，和载荷中 Cursor 会话不同。载荷见 [protocol.md](protocol.md#cursor-交接载荷与回执)。非流式 `result.content` 为 JSON 回执/错误，外层成功仅表示提交或查询已返回 |

`exec` / `read` 等若本机配了多 peer，`params.device_id` 必填；只有一个 peer 时可省。
`session_id` 由 `session.request` 成功结果返回，Runtime 会把它绑定到每个后续 invoke；调用方不能自行扩展其 scope 或有效期。

---

## 4. 审批

被控机上的高危命令：Runtime 向 **当时连着的人侧头** 发：

```json
{"id":"a9","event":"approval","params":{"approval_id":"a9","exec_id":"ex_…","request_sha256":"…64 hex…","created":"…","expires":"…","details":{"kind":"script","script":"Restart-Computer","shell":"powershell","executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","args":["…"],"cwd":"C:\\workspace","runtime":{"pid":1234,"sha256":"…64 hex…"}}}}
```

头以独立请求 ID 发送 `approval.respond`，参数为 `{approval_id:"a9",request_sha256:"…",allow:true|false}`。Runtime 对完整 details 做摘要绑定；详情内容改变、摘要不匹配、过期或重复确认都返回拒绝。审批 ID 与 IPC 请求 ID 不混用；批准不会重新解释或重建命令。

执行审批与逻辑会话审批的事件投递都受同一总审批期限约束；每次写入的两秒预算包含
排队和传输。写入、取消回调和 deadline 清理按连接串行，不能由后一条审批覆盖或清除
前一条的期限；执行取消会中断阻塞投递，不能让不读取事件的头拖住 Runtime 停止。

v0：**发出后 T 秒内没有任何头订阅/应答 → `policy_deny` 或 `approval_timeout`**（与 [runtime.md](runtime.md) 一致）。MCP 连接不算「有 UI」。无头机不要等。

---

## 5. ensure

### Runtime owner contract

MCP、CLI、TUI、GUI 都是本机 Runtime 的头；它们不各自选择、验签或启动 Runtime。`runtime.ensure` 是唯一的自动确保入口，显式 `runtime install` / `runtime rollback` 仍由 Runtime owner 的同一激活状态机执行。

请求参数：

```json
{"id":"r1","method":"runtime.ensure","params":{"required_profile_format":1,"required_protocol":1}}
```

`required_profile_format` 与 `required_protocol` 均为必填整数，缺失、null 或与运行版本不符时拒绝。成功结果中的 `runtime` 至少包含 `state:"ready"`、`version`、`revision`、`protocol`、`profile_format`、`sha256`、`pid` 和不透明的 `activation_id`、`launch_id`。头必须检查完整身份和所需格式，不能只凭 `ok:true` 放行。摘要是 64 位小写十六进制，两个 ID 都是 32 位小写十六进制，PID 是正整数。结果不返回可供头启动程序的路径；这些身份字段是当前观察，不是可复用的执行授权。

`runtime.status` 成功表示查询完成，不代表 Runtime 就绪。其 `result.runtime.state` 为 `ready`、`pending`、`unverified` 或 `unknown`；非 ready 时附 `code`，不返回已确认的 activation/launch ID。`runtime.ensure` 对同样的非 ready 状态返回失败信封。安装记录缺失、来源证明失败、运行映像与选定副本不一致、验证期间记录发生变化时都不能报 ready；pending 不被自动修复。activation ID 来自稳定且已验证的安装选择，不从进程路径或 Linux memfd 名称推断。launch ID 还必须与该服务进程启动时从已认证继承许可中保留的 ID 一致；不能把磁盘上后来写入的同制品新启动 ID 当成本进程身份。此观察不自动结束或重放未知事务。

来源验证失败、profile/protocol 不匹配、pending 恢复、启动失败或清理未确认分别返回结构化错误 `runtime_source_unverified`、`runtime_profile_mismatch`、`runtime_pending_recovery`、`runtime_start_failed`、`runtime_cleanup_uncertain`。头收到未知结果时只查询 `runtime.status`，不重放 ensure，不自行换另一个候选。

Owner 在返回 ready 前必须把已验证的 binary 和原始 manifest 放入自己的 activation snapshot，重查摘要、构建身份、PID 和 IPC readiness，并保留 snapshot 到进程退出和清理确认。`runtime.ensure` 不会因为某个头关闭窗口而杀掉 Runtime；Runtime 的所有权和头的 IPC 会话分离。

头（尤其 MCP）启动：

```text
1. 试连 IPC
2. 通 → 发 runtime.ensure；只有完整、格式匹配的 ready 身份才允许继续
3. 仅在明确确认端点不存在/拒绝连接时，经统一可信引导入口启动一次来源验证过的私有映像；权限错误或超时不等于没有 owner
4. 引导结果不确定时只观察状态，不重放启动、不换候选；pending/来源失败均交付明确错误
5. IPC 可连接后仍需完成 owner 身份和来源验证；它不等于 ready
6. ready 之后：若环境变量带 DEVICE_ID+GRANT，peer.add 收编（幂等）
```

同一 MCP 进程内的并发首次调用共享一次连接和 ready 验证；未验证完成的连接
不交给工具使用。断线后新连接必须重新准入，不重放业务请求。该 MCP 进程最多
调用一次首次引导，成功或失败结果都保留：后续调用可以重新观察已由用户恢复的
IPC，但不能因端点仍缺失而再次启动候选。恢复服务后无需缓存旧 ready 结果。

MCP 对 `runtime.ensure` 及准入后的本机 `status` 观察设置 65 秒应答期限，
覆盖 Runtime 的 60 秒来源观察预算。已连接但未应答不代表 owner 不存在；
超时关闭该连接、报告状态未确认，不触发引导重放。观察完成或连接失败即清除
对应计时器，执行流不继承准入观察期限。

就绪检查与后续业务请求必须使用同一条 IPC 连接；新建连接时重新执行
`runtime.ensure`，不能沿用先前连接的 ready 结果。拒绝或响应身份不完整时关闭
连接，不发送业务请求、不自动切换候选。长驻流在握手完成后清除握手期限，
避免把来源验证的超时当作任务运行期限。诊断 `status` / `runtime.status` 和显式
停止、恢复路径不依赖 ready，以便未就绪时仍可检查与处理。

**v0 不从网上静默下载二进制**（供应链）。发行见 [stack.md](stack.md)。

已有 Runtime：ensure **不杀不重启**。升级和回滚由显式 `runtime install` / `runtime rollback` 执行，流程见 [runtime.md](runtime.md)。

单实例：数据目录锁 + 只听一个 IPC。第二份 `start` 发现锁则报错退出；重复 `ensure` 返回现有进程状态。

显式设置绝对路径 `XALLOR_REMOTE_DATA_DIR` 时，Unix IPC 位于该目录的 `ipc.sock`；Windows 管道名是 `XallorRemote-` 加规范化目录 SHA256 的前 16 位，Go 和 MCP 使用同一算法。读取状态不会创建 profile 或启动 Runtime。

---

## 6. 和数据平面的边界

```text
头  --IPC method=exec-->  Runtime  --WSS invoke-->  Relay  --> B
头  <--IPC event=stdout-- Runtime  <--WSS stdout---  Relay  <-- B
```

IPC 的 `id` 与 WSS 的 `exec_id` 是两层。Runtime 做映射；头只需要 IPC `id` 和返回里的 `exec_id`（给 cancel / 展示用）。

非 GUI WIP 的 `session.request` / `session.resume` 同样由 Runtime 为每次调用生成
独立的 WSS `request_id`，回执必须同时匹配该 ID、`session_id` 和结果消息类型。
不能只按 session 关联：同一 session 的并发请求各有独立的 IPC 终态。pending 等进度
不会占用终态缓冲；每个等待者只接纳一次终态，连接失效不能覆盖已经收到的终态。

连接失效时，所有尚未完成的会话调用返回 `session_unknown`；对已失效连接不能再注册
等待者。开始尝试发送后的传输错误也返回 `session_unknown`，因为错误不能证明 Relay
未收到或未提交。明确在发送前因帧大小拒绝时返回 `too_large`。unknown 不授权头重放
请求，也不表示一次性授权码或恢复凭证仍可用。此内存回执关联不提供跨 Runtime 重启
的持久化结果查询或未知提交结算。

控制连接进入逻辑会话模式后，拒绝、审批入队失败或设置变更撤销授权都不会恢复 v0
grant 回退。旧会话被设置变更撤销后，可在同一连接显式重新 `session.request`，但
同一控制连接只能有一个 pending；必须重新获得接收方批准。旧执行流与新逻辑会话
按原请求分别结算，旧终态不能结束新会话或提前释放仍在运行的作业资源。

## 7. GUI 连接与本机记录

桌面 GUI 启动同安装版本的 `xallor-remote ipc-bridge` 子进程，由 Go 复用 profile 路径和 ensure，透明转发 NDJSON 到一条本机 IPC。子进程不实现策略，不接 Relay。关闭窗口或子进程输入流会关闭该 IPC；只取消该连接的任务，Runtime 继续运行。开发页面的 HTTP 转发仅用于本机开发，要求同源 POST JSON；它直接连接已启动的本机 Runtime，并在同一连接上验证 `runtime.ensure` 就绪身份后开放会话，不查找或启动二进制。运行开发页面前须通过 CLI 启动 Runtime。发行 GUI 不监听 HTTP。

GUI 单次任务上限 60 秒，页面切换保留当前连接和最多 20 条内存任务，最多 8 个并行任务；单任务显示最多 256 Ki 字符，截断明确提示。收到终态才确认结果；断开或超时显示待确认，重新连接不重放命令。IPC 桥单帧上限 2 MiB，积压上限 8 MiB / 4096 帧，溢出关闭连接并明确报错。

`audit.list` 返回 time、exec_id、direction、device_id、op、stage、code、exit_code；stage 为 requested / finished / rejected / unknown。device_id 是执行目标，不代表控制者实名。仅记录 Runtime 观测到的已发起操作及终态，不宣称完整防篡改审计。数据目录 activity.json 保留最后 500 条、最大 512 KiB，不含命令、输出、授权码；损坏或写失败不伪装成空记录。退出码缺失保留 null，禁止补成成功的 0。
