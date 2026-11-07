# 本机 IPC（头 ↔ Runtime）

SSOT：命名管道 / Unix socket 上的报文与 ensure。路径与权限见 [decisions.md](decisions.md)。头关系见 [heads.md](heads.md)。Relay WSS 见 [dataplane.md](dataplane.md)。**IPC 不是 WSS 的同一套 type 表**——本地多了 grant/peer/审批，少了 hello_device。

---

## 1. 传输

| | |
| --- | --- |
| Windows | `\\.\pipe\XallorRemote` |
| Linux / macOS | `$XDG_RUNTIME_DIR/xallor-remote.sock`，否则 `~/.config/xallor-remote/ipc.sock` |
| 权限 | 仅当前用户（Unix `0600`；Windows 管道 DACL 限本用户） |
| 帧 | **一行一条压缩 JSON**（NDJSON）。对象内部换行必须转义，禁止 pretty-print |
| 禁止 | 在 localhost 再开一个随机 TCP 端口当「更好写」 |

同一用户多头可并行连。连不上 = Runtime 没起来，走 §5 ensure，不要改连 Relay。

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
| `config.get` / `config.set` | 人侧头 | get 返回 `{relay_url,workspace,shell}`，未配置 shell 显示 auto；set 接受这三项，省略或空值不更新，shell=auto 恢复自动选择；错误不改变活动配置 |
| `account.set` | 人侧头 | `{token:string}`；必须明确提供，空字符串表示清除；返回 `{configured:bool}` |
| `account.status` | 人侧头 | 返回 `{configured:bool}`，不返回凭据或宣称已在线认证 |
| `device.access` | 人侧头 | 归属/分享管理参数见协议；凭据由 Runtime 获取，头不得传入 token 或设备密钥 |
| `approval.subscribe` | 人侧头 | 注册为审批 UI；断开即取消 |
| `approval.respond` | 人侧头 | 独立请求 ID；`{approval_id,allow:bool}`，同一审批最多确认一次 |
| `approval.list` | 人侧头 | 当前待审批 `{pending:[{id,exec_id,preview,created}]}`；只读，不代替订阅 |
| `audit.list` | 人侧头 | 本机最近 500 条操作元数据 `{entries:[...]}`；读取失败明确报错 |

MCP **可以**调 `status` / `peer.*` / 下面远程方法；**不要**调 grant/inbound/revoke/account.set/device.access（包里不实现）。Runtime 不做「MCP 指纹鉴权」——防模型靠 tool 面，不靠 IPC 分角色。MCP 连接**不算**审批 UI。

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
| `exec` | invoke op=exec；params 为 [protocol.md](protocol.md) 的 script/program 输入，另带 device_id |
| `exec.cancel` | 仅取消 **这条 IPC 连接** 发起的 exec（Runtime 再映射到自己的 client 连接） |
| `read` / `write` / `processes` / `info` | 对应 op |
| `handoff.submit` / `handoff.get` | 对应 op，`params.device_id` 必填，载荷见 [protocol.md](protocol.md#cursor-交接载荷与回执)。非流式 `result.content` 为 JSON 回执/错误，外层成功仅表示提交或查询已返回 |

`exec` / `read` 等若本机配了多 peer，`params.device_id` 必填；只有一个 peer 时可省。

---

## 4. 审批

被控机上的高危命令：Runtime 向 **当时连着的人侧头** 发：

```json
{"id":"a9","event":"approval","params":{"exec_id":"ex_…","preview":"rm -rf …"}}
```

头以独立请求 ID 发送 `approval.respond`，参数为 `{approval_id:"a9",allow:true|false}`。审批 ID 与 IPC 请求 ID 不混用；重复确认返回 unknown_exec。

v0：**发出后 T 秒内没有任何头订阅/应答 → `policy_deny` 或 `approval_timeout`**（与 [runtime.md](runtime.md) 一致）。MCP 连接不算「有 UI」。无头机不要等。

---

## 5. ensure

头（尤其 MCP）启动：

```text
1. 试连 IPC
2. 通 → 发 status，版本不匹配则报错退出（见 [stack.md](stack.md)）
3. 不通 → 按顺序找 `xallor-remote`：`XALLOR_REMOTE_BIN` → 已校验的托管制品 → npm 包内 `vendor/<platform>/` → `PATH` → 数据目录 `bin/`
4. 以隐藏窗口/脱离终端方式拉起 `xallor-remote start`；托管制品状态不完整或校验失败时拒绝回退到其它版本
5. 轮询 IPC，默认 10s，失败给人话（找不到二进制、权限、杀软）
6. 通了之后：若环境变量带 DEVICE_ID+GRANT，peer.add 收编（幂等）
```

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

## 7. GUI 连接与本机记录

桌面 GUI 启动同安装版本的 `xallor-remote ipc-bridge` 子进程，由 Go 复用 profile 路径和 ensure，透明转发 NDJSON 到一条本机 IPC。子进程不实现策略，不接 Relay。关闭窗口或子进程输入流会关闭该 IPC；只取消该连接的任务，Runtime 继续运行。开发页面的 HTTP 转发仅用于本机开发，要求同源 POST JSON；发行 GUI 不监听 HTTP。

GUI 单次任务上限 60 秒，页面切换保留当前连接和最多 20 条内存任务，最多 8 个并行任务；单任务显示最多 256 Ki 字符，截断明确提示。收到终态才确认结果；断开或超时显示待确认，重新连接不重放命令。IPC 桥单帧上限 2 MiB，积压上限 8 MiB / 4096 帧，溢出关闭连接并明确报错。

`audit.list` 返回 time、exec_id、direction、device_id、op、stage、code、exit_code；stage 为 requested / finished / rejected / unknown。device_id 是执行目标，不代表控制者实名。仅记录 Runtime 观测到的已发起操作及终态，不宣称完整防篡改审计。数据目录 activity.json 保留最后 500 条、最大 512 KiB，不含命令、输出、授权码；损坏或写失败不伪装成空记录。退出码缺失保留 null，禁止补成成功的 0。
