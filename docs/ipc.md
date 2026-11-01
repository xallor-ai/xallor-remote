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
{"id":"c1","method":"exec","params":{"device_id":"dev_B","command":"whoami"}}
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
| `config.get` / `config.set` | 人侧头 | relay URL、workspace、shell |
| `approval.subscribe` | 人侧头 | 注册为审批 UI；断开即取消 |
| `approval.respond` | 人侧头 | 同一 `id`，`{allow:bool}` |

MCP **可以**调 `status` / `peer.*` / 下面远程方法；**不要**调 grant/inbound/revoke（包里不实现）。Runtime 不做「MCP 指纹鉴权」——防模型靠 tool 面，不靠 IPC 分角色。MCP 连接**不算**审批 UI。

`status.result.build`：version、revision、built_at、dirty、protocol、os、arch、path、sha256、pid。path/hash 在进程启动时采集，表示正在运行的制品，不把升级后的磁盘文件冒充当前进程。远程 info 的 build 不包含本机 path/hash/pid。

`status` 的 peers 仅含 `device_id`、`has_grant`，不返回授权码或 device_secret。`relay` 仅返回 scheme + host，去掉可能携带凭据的 userinfo/path/query/fragment；编辑地址仍走显式 `config.get`。

`doctor` 返回 `{schema:1, observed_at, device_id?, ok, checks:[{layer,state,code?,action?}]}`。固定层次：`ipc`、`runtime_relay`、`peer`、`relay_transport`、`authorization`、`target_online`、`inbound`、`target_runtime`。状态为 `ok` / `fail` / `unknown` / `skipped`；前置失败后的未测项不冒充成功。省略设备时仅测本机服务及其 Relay 连接；目标诊断创建一次独立 client 连接，hello 后只调用只读 `info`，以目标 ID 与成功终态验证目标响应，不执行 shell，不修改配对/授权/入站，不复用正在执行任务的连接。远程探测上限 5 秒。未返回终态只能表示未确认，诊断不承诺后续任务一定成功。

CLI `doctor [--device ID] [--json]` 不调用 ensure，也不创建数据目录；`status --json` 输出上述脱敏状态。doctor 全部已请求检查通过时退出 0，失败或未确认退出非 0；JSON 始终写 stdout，人话错误写 stderr。

### 远程（Runtime 再走 WSS）

| method | 对应 |
| --- | --- |
| `exec` | invoke op=exec |
| `exec.cancel` | 仅取消 **这条 IPC 连接** 发起的 exec（Runtime 再映射到自己的 client 连接） |
| `read` / `write` / `processes` / `info` | 对应 op |

`exec` / `read` 等若本机配了多 peer，`params.device_id` 必填；只有一个 peer 时可省。

---

## 4. 审批

被控机上的高危命令：Runtime 向 **当时连着的人侧头** 发：

```json
{"id":"a9","event":"approval","params":{"exec_id":"ex_…","preview":"rm -rf …"}}
```

头回同一 `id`：`approval.respond` `{allow:true|false}`。

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
