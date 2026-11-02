# 数据平面：Relay 怎么把一次 exec 跑通

SSOT：连接如何建立、`exec_id` 如何关联、流如何回、cancel / 断线 / 背压。这是 **Relay 服务端的实现契约**，不是产品故事。

消息字段与错误码只在 [protocol.md](protocol.md) 改。落盘与 IP 预算在 [relay.md](relay.md)。策略与 Popen 细节在 [runtime.md](runtime.md)。谁连谁的产品路径在 [architecture.md](architecture.md)。

**一句话：** Relay 是有鉴权的实时消息路由器：管 device 连接、grant 校验、`exec_id` 路由和生命周期错误；**不执行、不解释命令、不持久化 stdout / 文件正文。**

---

## 1. 发动机

```text
Cursor / CLI
    │ remote.exec / xallor-remote exec
    ▼
A 头（MCP 短驻 或 CLI）
    │ IPC
    ▼
A Runtime ──WSS client──► Relay ──WSS device──► B Runtime
    ▲                         │                      │
    │                         │ route by exec_id      │ policy → Popen
    │                         │                      │ stdout/stderr/exit
    └──────── IPC ◄───────────┴◄─────────────────────┘
```

MCP / CLI 不是数据平面。数据平面只有三段 WSS：`A Runtime ↔ Relay ↔ B Runtime`。头只跟本机 Runtime 说话。

Relay **永远不执行命令。** B Runtime 才 Popen。

---

## 2. 连接模型

两条出站长连接，都是 **WSS 文本帧，一帧一条 JSON**。公网 TLS；localhost 可用 `ws://`。

| 连接 | 谁发起 | hello | 绑定 |
| --- | --- | --- | --- |
| `device` | 被控 Runtime（B，以及任何 Node 自己） | `hello_device` + `device_id` + `device_secret` | 该 `device_id` 的入站 |
| `client` | 控制侧 Runtime（A） | `hello_client` + 目标 ID + 对应访问凭据（见协议） | 一条 client 连接只绑一个目标及授权快照 |

v0 **不**在一条 client 连接上多路复用多个目标。多 peer = 多条 client 连接。invoke 因此不必再带 `device_id`；目标以这条连接的 hello 为准。

同一 `device_id`：**一条** device 连接。新 hello 成功则踢旧连接；旧连接上的 inflight 按 §7 失败。

hello 失败：Relay 关连接，不登记。成功：回一条 `hello_ok`（实现可把字段放进现有心跳前的第一条下行）。之后双方按 [protocol.md](protocol.md) 发 `heartbeat`；超时 → 连接死，走 §7。

若 hello 带账号凭据，Relay 先从签发存储解析可信调用身份，再按设备归属选择共享码验证或账号访问授权。已认领设备绑定归属实例、分享实例（所有者无此项）、操作集合和有效期。账号身份与授权快照固定在该连接上，不能通过后续 invoke 改换。

`access.request` 仅从已认证 device 连接进入，以该连接的设备身份而非 JSON 自报 ID 为目标。使用同设备身份协调器，在事务中再次核对设备 UID、密钥哈希、归属实例、账号有效性和管理权限。存储完成后发布新路由绑定再回结果；提交后断线仍须发布已提交的状态。归属改变会关闭旧 client 连接，向其前台 inflight 请求取消并结束原路由；分享撤销/替换由新调用的验证使旧授权失败。

管理写入遇到存储故障时，提交结果可能不确定。Relay 在回复错误之前停止该 device 连接的路由并关闭连接；重连从存储重新加载权威归属，不能沿用旧缓存继续匿名放行。认证失败、参数失败或只读状态查询失败不因此关闭设备。Runtime 将这类写入故障、断线和超时报告为结果未确认，不自动重放。

控制面消息（`grant_rotate` / `revoke` / `inbound_set`）只走 **device** 连接，见 [credentials.md](credentials.md)。本文只覆盖 invoke / 事件。

---

## 3. 请求模型

`exec_id` 的连接级语义见 [protocol.md](protocol.md)。A Runtime 的关联 ID 只在该 client 连接上匹配请求；Relay 为每次准入生成独立的内部调用 ID，下发 B 后只按内部 ID 路由，再把回事件还原为 A 的关联值。两者不能混用，也不同时在信封上双写。不同 client 同值不冲突；同一 client 的在途重复值拒绝。

A 在已 hello 成功的 client 连接上发：

```json
{
  "type": "invoke",
  "exec_id": "ex_01J…",
  "op": "exec",
  "payload": {
    "command": "python train.py",
    "cwd": "project",
    "timeout_ms": 600000
  }
}
```

字段名以 [protocol.md](protocol.md) 为准（`payload` 不是 `args`）。`op` 同样适用于 `read` / `write` / `cancel` 等；**流式闭环以 `exec` 为发动机**，其它 op 也走 inflight，但通常一问一答。

Relay 收到 `invoke`（`op != cancel`）严格按序，失败即停、发 `invoke_nack`、**不**建 inflight：

```text
1. 这条连接是已鉴权的 client；带账号时在路由锁外重新验证凭据、成员启用状态及版本；已认领设备还验证当前归属、分享、到期及本次操作权限
2. 该连接仍存在，目标与 hello 验证得到的设备 UID + 共享授权实例或归属实例匹配当前绑定，否则 unauthorized
3. 客户端 exec_id 非空，且 (client连接,客户端exec_id) 未占用，否则 agent_error
4. 目标有活着且内部 UID 匹配的 device 连接，否则 device_offline
5. 目标 inbound 开，否则 inbound_disabled
6. 该设备 inflight 数 < 并发上限，否则 quota_exceeded（不排队）
7. 连接 context 尚未取消；已断开的请求停止准入
8. 分配随机内部调用ID，原子登记 requests[(client连接,客户端exec_id)] → 内部ID 及 inflight[内部ID]，保留客户端原值、目标、client连接及可信调用身份
9. 从已验证的连接绑定推导稳定 scope，重建 invoke 并转给 B 的 device 连接；客户端身份、凭据及无关字段不透传，payload 内 command 不改写。scope 字段见协议，后台交接的消费与留存见 handoff.md。
```

账号及分享验证以本次数据库读取为判定时点，与随后进入路由锁不是跨模块原子事务。与撤销同时发生且已通过验证的请求可能继续准入；撤销完成后开始验证的新请求会失败。归属变更还会在路由锁内使旧绑定失效。取消与输出帧不查询账号数据库，原连接仍可取消已经登记的任务。成员恢复递增版本，旧连接必须重新握手才能恢复使用；认证等待有期限并随连接断开取消。此阶段不提供撤销或转移即确认后台任务终止的承诺。

B 收到后：校验请求形状 → [runtime.md](runtime.md) 策略。deny / 审批失败 → B 发 `invoke_nack`，Relay 转给 A 并 **删除 inflight**。allow 且 `exec` → Popen，**不再** nack。

`read` / `write` / `info` / `processes`：B 做完用一条结果消息结束（具体形状见 protocol）；同样登记/删除 inflight。本文余下按 `exec` 写。

---

## 4. 路由模型

内存里只这件事决定回流打到谁：

```text
requests[(client_conn, client_exec_id)] = internal_exec_id
inflight[internal_exec_id] = { device_id, client_conn, client_exec_id, identity }
```

B → Relay 的 `stdout` / `stderr` / `exit` / `error` / `invoke_nack`：Relay **不解析 data**。以设备回显的内部 `exec_id` 查 inflight，并核对当前 device 连接；还原 client_exec_id 后转发。找不到 inflight → 丢弃，包括旧调用晚到的输出和终态。不能用客户端同值 ID 查找另一个调用。

A → Relay 的 `cancel`：以当前连接和客户端 ID 查 requests，再查 inflight，否则 `unknown_exec`（不区分「存在但是别人的」）。匹配则以内部 ID 把 cancel invoke 转给该 device 连接。旧连接不能取消另一连接新建的同值请求。目标上的本地执行映射、审批关联和取消均使用它实际收到的内部 ID。

**每个内部调用至多转发一个终态，然后同时删除 inflight 与 requests 两项：**设备替换、归属转移、任一队列失败和 client/device 断线也必须同时回收，不能保留悬空索引。Relay 生成错误回复时仍使用客户端原 ID。

| 终态 | 何时 | A 看到 |
| --- | --- | --- |
| `invoke_nack` | 从未 Popen（Relay 拒 或 B 策略拒） | nack + code |
| `exit` | 已经 Popen，进程 wait 结束（含被 cancel/timeout 杀掉） | `exit` |
| `error` | 通道坏了，等不到 wait | `error` + code |

禁止：先 `exit` 再 `error`；禁止两条 `exit`；禁止 nack 之后再推 stdout。

Relay **没有**协议级状态 `accepted` / `dispatched` / `running`。表项只有两种：有这条 inflight，或没有。过程：

```text
无 inflight
  invoke 校验失败 → nack，仍无
  invoke 通过     → 创建 inflight → 转给 B
有 inflight
  stdout/stderr   → 原样转给 client_conn
  第一条终态      → 转给 A → 立即删除
  A 连接先死      → 向 B 发 cancel → 立即删除（不等 B 的 exit）
删除之后
  任何带该 exec_id 的帧 → 丢弃
  再来的 cancel → unknown_exec（B 已 exit 同此，不必再问 B）
```

`exit.status`：

| status | 含义 | MCP/CLI 对外 code |
| --- | --- | --- |
| `completed` | 正常 wait（`exit_code` 可为非零） | 无错误码；非零只是退出码 |
| `cancelled` | 本 client 的 cancel 杀树后 wait | `cancelled` |
| `timeout` | `timeout_ms` 到点杀树后 wait | `exec_timeout` |

非零退出 **只**走 `exit`，不走 `error`。`error` 不是命令失败。

---

## 5. 流模型

B **禁止** `output = run(cmd); send(output)`。必须：

```text
Popen
  ├── stdout reader ──► 按行或块（≤单帧上限）发 type=stdout
  ├── stderr reader ──► 同上 type=stderr
  └── wait()        ──► 一条 type=exit，然后停
```

管道必须持续读，避免子进程堵死。编码见 [os.md](os.md)。无 stdin、无 PTY。

消息就是 [protocol.md](protocol.md) 的顶层 `type`，**不要**包一层 `{type:event, event:stdout}`：

```json
{"type":"stdout","exec_id":"ex_01J…","data":"step 1...\n"}
{"type":"stderr","exec_id":"ex_01J…","data":"warning...\n"}
{"type":"exit","exec_id":"ex_01J…","exit_code":0,"duration_ms":18342,"truncated":false,"status":"completed"}
```

同一 `exec_id` 在同一条 WSS 上 **FIFO**（实现：该连接的写必须串行，两个 reader 不能并行 `Write`）。stdout 与 stderr **不**保证与进程真实时间线一致，只保证各自内部顺序。`exit` 是该 id 最后一条数据面消息。

**v0 不设 `seq`。** 单连接 WSS 跑在 TCP 上，帧不丢则顺序就是发送顺序；故意丢的数据只靠 `truncated`。序号既不能恢复，也不能当重放——加上去只是让每条消息变胖。以后若做多路/续传再加字段，不改 `type`。

Relay 对 stdout/stderr：**查表转发**，不拼行、不落盘、不理解 ANSI。

A Runtime 收到后经 IPC 推给 MCP/CLI。MCP 若不能 notifications，可在头上攒——**WSS 这一段仍必须是流**。

---

## 6. 取消模型

```text
A 头 → A Runtime → invoke op=cancel, exec_id
                 → Relay：按 client_conn + 客户端ID查到内部ID和inflight
                 → B Runtime：杀进程树（Win Job / Linux 进程组）
                 → wait
                 → exit status=cancelled
                 → Relay 转给 A，删 inflight
```

cancel 在 B 尚未 Popen（仍在审批）时：B 放弃等待，发 `invoke_nack` `cancelled`，Relay 删 inflight。

对未知 / 别人的 exec_id：Relay 立刻 `unknown_exec`，不打扰 B。

幂等：inflight 已删（已经 exit）再 cancel → `unknown_exec`。A 头可当成功忽略。

---

## 7. 断线模型

v0 **不恢复、不重放 stdout**。重连是新连接，旧 `exec_id` 作废。

| 谁断 | Relay | B Runtime | A 看到 |
| --- | --- | --- | --- |
| B device 连接 | 该设备全部 inflight → `error` `device_offline`，删表 | 杀掉无主子进程；退避重连 `hello_device` | 各 exec `device_offline` |
| A client 连接 | 仅该连接的 inflight：向 B 发 cancel，**立刻删表**（不等 B 的 `exit`） | 按 cancel 杀树 | **A 本地 `relay_error`**（连接断了，不是用户点了 cancel） |
| Relay 进程没了 | inflight 内存全没 | 发现 WSS 死：杀无主进程，重连 | WSS 死 → 本地 inflight `relay_error` |
| 新 device 踢旧 device | 同「B device 连接」 | 旧进程当无主杀 | `device_offline` |

B **不准**在重连后继续往新连接推旧 `exec_id`。A 也 **不准** 要求 Relay 补历史帧。

---

## 8. 背压与截断

`yes` 这类洪水必须可活。v0 选择：**截断，不排队，不落盘，不把慢消费者反压成全机卡死。**

数字只在 [protocol.md](protocol.md) §5 改。规则：

1. **单帧**超过上限：丢这一帧，将该 exec 标 `truncated`，继续。
2. **该 exec 已转发的 stdout+stderr 字节**超过上限：之后的 stdout/stderr **丢弃**，标 `truncated`，进程继续跑到 `exit`（`truncated: true`）。
3. **Relay 每条 inflight 的待发送缓冲**超过上限：丢 stdout/stderr，标 `truncated`；**不要**用 TCP 反压把 B 的整条 device 连接堵住（会伤同设备其它 exec）。
4. B 达到本机上限后同样停上送、仍排空管道、最后 `exit`。
5. `exit` / `invoke_nack` / `error` **必须送达**（缓冲不够就丢掉该 exec 的数据帧给终态让路）。

路由状态锁内只允许有界内存入队，网络写入由每条连接的独立 writer 串行完成。先入队 `hello_ok` 再发布在线连接，保证任何 invoke 不会跑到问候帧之前。发送队列同时限制每执行流字节数、连接总字节数和帧数；输出超限标截断，控制/终态保留槽不足时只能关闭故障连接，不能无限占用内存。

终态送达以连接仍可写为前提；单次写入和关闭排空都有期限，逾期转为 §7 的通道失败语义。关闭请求不得在路由锁内等待网络关闭握手。传输失败后连接所有者负责 Drop 和执行回收。

接收端在身份存储等待期间继续读取，通过有界缓冲向连接处理循环交付。输出超出接收缓冲时仅丢弃对应数据并标记该执行流截断，为终态和控制消息保留空间；控制消息超限关闭连接。接收计量在入队/丢弃之前进行。接收断线取消该连接未完成的身份等待，尚未处理的客户端 invoke 不得在断线后启动；设备已接收的终态仍可按原归属完成转发。

没有「暂停 B、等 A 读完再继续」的 v0 协议。**不要用停 drain 当背压**：会堵住 B 整条 device 连接上的其它 exec。以后要做窗口/credit 另开需求。

---

## 9. 服务端进程（Relay 怎么建）

v0 一个 Go 进程即可（与 Runtime/CLI 同一二进制：`xallor-remote relay`）。

| 状态 | 在哪 | 重启后 |
| --- | --- | --- |
| 连接、inflight、待发缓冲 | **仅内存** | 全丢，走 §7 |
| devices / device_grants | 磁盘身份与授权状态，见 [relay.md](relay.md) | 还在；设备需重新 hello 才 online |

建议实现：连接一条 goroutine；`exec_id` 路由一张带锁的 map；不要把 stdout 写成「先收齐再发」。不要在 Relay 里 shell out。不要为 invoke 再开一条 HTTP API。

官方实例另加每 IP 注册/连接预算，见 [relay.md](relay.md)。自托管同一套协议，预算可关。

验收（服务端闭环，双 OS 各一次）：A `exec` 能在结束前看到至少两帧 stdout；`cancel` 得到 `exit.status=cancelled`；B 拔网 A 得 `device_offline` 且 B 无孤儿进程；`yes` 打满后 `truncated=true` 且仍有 `exit`；Relay 重启后 A 得 `relay_error`，重连后新 exec 可跑。
