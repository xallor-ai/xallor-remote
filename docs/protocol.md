# 协议合同（v0）

SSOT：消息语义、错误码、角色。建立连接、inflight、流式、cancel、断线、背压的**过程**只在 [dataplane.md](dataplane.md)。谁能改授权见 [credentials.md](credentials.md)。

JSON 字段名实现时可加前缀，**type、code、角色不得改义**。本文不是加密规格。

本机 Runtime 生命周期能力使用独立的 `runtime_owner_protocol`，当前值为整数 `1`，
由 `version --json` 和运行身份中的 build 信息返回。它不改变 Relay `protocol`
或持久化 `profile_format`，也不替代来源证明。安装、恢复基线、回滚和无 owner
启动在执行受保护的版本探测时要求精确匹配；缺字段、零或其他值均在 Stop、
新 pending 和 Start 之前拒绝，不尝试旧协议降级。就绪信封见 [ipc.md](ipc.md)。

本机选择记录的 `pending_launch_id` 为每次新启动随机生成的 32 位小写十六进制，
提交时复制为 `current_launch_id` 并清空 pending 字段。私有启动信封的 `launch_id`
必须与该次选择对应；即使 activation ID、程序摘要不变，恢复也生成新的启动标识，
旧许可在 pending 阶段及最终提交后均不能恢复有效。该标识不替代来源证明，不是
远程执行幂等键或授权码。历史未完成选择可以缺少此字段，但普通启动不会自动恢复它；
显式恢复会撤销或生成新标识，再允许启动。服务在持有运行锁时、身份/IPC 初始化前检查。

本机只读 `runtime.operation.status` 使用现有 IPC 信封，参数仅含
`operation_id`（32 位小写十六进制）；参数 JSON 至多 256 字节，未知字段和尾随数据拒绝。
只读 `runtime.operation.reconcile` 仅接受 `operation_id`、`namespace_sha256`、
`expected_revision`、`expected_record_sha256` 四个字段，执行 namespace 与 record
revision/digest 的 CAS 观察，返回相同的原始事务记录并附
`disposition:unresolved` 与 blocker `native_owner_observation_required`。当前没有
公共 resolution writer；该方法不会创建 profile/数据库、追加 resolution、运行
install/rollback 或重放请求。未知字段、超限参数、错误 namespace、未知 operation
或过期 CAS 均以有界错误拒绝；请求 JSON 上限为 512 字节，revision 为正数且不超过
1,000,000,000，operation、namespace 和 record digest 必须分别是 32/64/64 位小写十六进制。
成功返回 `result.operation`，其中 `request` 含 `id`、`kind`（install/rollback）和
`intent_sha256`；记录另含 `namespace_sha256`，`outcome` 为 pending、uncertain、succeeded、failed 或 rejected，
`effects` 至多 32 项，每项含 `name`、`state`、`before`、`target` 和可选 `target_sha256`。
`before` 是派发前的选择观测；选择写入（prepare/commit/clear）另含必填 `expected`，
仅表示计划写入的选择。返回后独立读取的 `observed` 不能由 expected 替代。
选择观测含 `state_sha256`（对 Go State 按声明字段顺序紧凑 JSON 编码的 SHA-256，包含完整 manifest），
以及 `current`、`previous`、`pending` 三个对象；每个对象含可选 `sha256`、`activation_id`、
`launch_id`，空引用为 `{}`。Previous 不保留 launch ID。历史选择可缺 activation/launch，
但 start、prepare、commit、ready 的 target 必须含完整 SHA、activation 和 launch。
target 与对应选择引用严格关联；start 恢复 Current 时使用 Pending 的 launch ID。
stop 的 target 为空，另含必填 `stop` 意图：`mode`（peer/no_peer）、32 位小写十六进制
`attempt_id`，peer 另含 `peer_pid`、`image_sha256`、`selection` 和
`identity_scope=process_image_and_launch`。selection 必须精确对应 before 的 Current 或
Pending，且与实际 IPC 内核进程和保留映像观察器绑定。普通 status 的 build 新增
`activation_id`、`launch_id`，由服务从已认证启动许可锁存；Pending 时仍返回，且不代表 ready。
no_peer 只含 `absence_basis`：fresh 或 explicit_legacy_recovery，不含 PID、映像或 selection。
fresh 要求原选择全空；legacy recovery 要求已有 Pending、经认证的恢复入口及保留的运行锁。
公共 stop 的 public_observation/process_image 和 absent_profile 仅用于即时观察，不得进入日志。

停止结果另含 `stop_observed`，必须关联同一 attempt_id，字段和值为：
- `dispatch`：not_sent / may_have_been_sent / acknowledged；
- `process`：exit_observed / still_running_or_unknown / not_observed（no_peer 仅此值）；
- `runtime_resource`：released_under_lock / held_by_recovery / unknown；
- `jobs`：absent / empty / reserved_only / unreleased_or_unknown；
- `observers`：released / unknown；
- `result`：released_peer / quiescent_no_peer / not_dispatched / unknown。

confirmed 必须由本次保留观察器确认退出（peer）或获准静默（no_peer），并确认运行锁、
同一次 jobs.lock 下账本读取、连接/进程/映像观察器关闭。恢复运行锁不属于观察器，持续保留到
旧启动许可撤销或新 Pending 落盘。已发送后丢失确认仍为 unknown，即使另观察到退出；
只有明确未派发且观察器释放的失败可为 not_applied。保存意图失败不能发送 stop，结果保存
失败不能自动重放或恢复。任务账本只读；这些证据不证明逃逸子进程、崩溃宿主或历史债务已释放。
序列化 PID、摘要和 attempt_id 不可重新构造原生观察器，也不是重放或协调授权。
pending 效果不得带 observed；confirmed/not_applied/quiescent_failure 必须有匹配的 observed。
观测读取失败或与预期不符保留 unknown；unknown 可含实际不匹配观测，不能触发恢复或重放。
成功 commit/ready 必须关联同一 activation/launch 以及完整提交后选择，不能仅比较程序 SHA。
单条记录上限 128 KiB；这些字段均是观测证据，不提供执行或自动协调授权。
效果名称为 prepare/stop/start/commit/ready/clear；状态为 pending/confirmed/not_applied/
unknown/quiescent_failure。摘要均为 64 位小写十六进制；不返回 bundle 路径、凭据或执行授权。
pending 仅说明事务未完成，不证明协调进程仍然存活；uncertain 阻止自动重放和新的准入。
查询成功不等于安装成功。rejected 仅说明未派发生命周期副作用，不声称从未执行来源检查。
quiescent_failure 表示失败启动的自有进程和映像资源已确认释放，不表示进程从未运行。
未知 ID 返回下表的 `runtime_operation_unknown`；查询不创建 profile、修复记录或恢复执行。

`namespace_sha256` 是配置中绝对日志目录 `<profile>/runtime/lifecycle` 的关联摘要：
对 `xallor-runtime-lifecycle-v2`、一个 NUL 字节、`ToSlash(Clean(absoluteJournalDir))`
拼接后的 UTF-8 字节计算 SHA-256，输出 64 位小写十六进制。拒绝相对目录；不自动解析
符号链接、大小写别名或迁移位置。SQLite 生命周期日志 schema 8 在同一事务中创建
库级 namespace 元数据；每条记录也保留同一摘要。打开库、读取记录及后续保存均须
匹配，schema 1/2/3/4 或缺失/不匹配元数据不会自动补写或升级。namespace 摘要域仍为 v2；
schema 8 保留选择证据、预捕获停止身份、停止结果证据、已解析请求绑定及显式转换目的；resolution 单独追加并以原记录摘要与 revision 做 CAS。schema 1–7 均拒绝且不静默迁移。它用于配置命名空间关联，
不是 MAC、机器身份、来源证明或执行许可，也不防御同一用户主动篡改本地存储。

首个生命周期副作用前必须一次性持久化 `binding`：`baseline` 为原始选择证据，
`requested` 固定已认证候选的 SHA 与 activation（此时尚无 launch），`manifest` 保存
来源 build/version/revision 与 artifact 元数据，`manifest_sha256` 是认证私有快照中的
原始 manifest 字节摘要，禁止重读 bundle 路径或序列化解析对象代替。绑定不可重写。
rollback 的目标必须是原始 Previous（有 Pending 时为 Current）；仅失败首次启动恢复
允许 `clear_only:true`，此时 Current/Previous 都为空且 Pending 存在，不伪造候选。
无副作用的拒绝记录可无 binding；只要存在副作用就必须有合法 binding。

每个 effect 的 `purpose` 必须为 `requested`、`baseline_prepare`、`restore` 或 `cancel`。
原始基线准备写入与请求转换分开记录；恢复启动使用本次 Pending launch，不复用旧 launch。
成功要求最终确认的 commit/ready 同属 requested，且目标与原始绑定的候选一致；rollback
的明确 clear-only 也可成功。restore/cancel 只证明请求失败后基线恢复或取消，不能作为
请求成功。原请求、绑定与已有 effects 保留，供后续显式追加式 reconciliation 使用；
R4 reconciliation 只允许 Runtime owner 对原始 Failed/Rejected，或带最终 stop/not_applied + StopObserved.NotDispatched 的 Uncertain 记录做核对；不得含 start/commit/ready、unknown 或 pending effect。必须同时持有生命周期准入、确认 worker 已停止，并以 bounded jobs/runtime/observers evidence 证明资源 settled。成功只追加 `resolution`（`disposition:resolved`、`request_result:not_applied`），不改写原 `record`，不执行重放或 force-clear；证据缺失返回 unresolved/busy 并保留 active。重复请求返回同一 resolution，过期 revision/digest CAS 拒绝。

R1 当前是 Runtime owner 内部的严格观察与追加式 resolution writer，不是公共 IPC/CLI 能力，也不提供重放或协调授权。它只能在原始 `binding` 不可变、`requested` 目的的 confirmed `commit` 精确匹配绑定候选，而历史 `ready` 确实缺失或为 unknown 时继续观察。owner-only evidence 必须同时包含同 namespace 和当前 record 摘要、内核 IPC peer PID、owner 认证的同 activation/launch、实际加载映像摘要、原始 manifest 来源证明、精确稳定的当前 selection，以及 jobs/runtime/source/probe/observers 五类已 settled debt；原始 worker/reconciler 必须不活跃。baseline restore/cancel、同 SHA 的错误 activation/launch/source、任一 unknown debt、错误 namespace/digest 或不稳定 ready 均为 unresolved。`Journal.ReconcileR1` 在生命周期锁内以原始 record digest/revision 做 CAS，仅追加 `resolution`，保留原 record/outcome/effects，并标记 `disposition:resolved`、`request_result:requested_applied`、`resource_state:settled`、`historical_ready_ack_uncertain:true` 与 `evidence_evaluator:runtimeowner/reconciliation-r1/v1`；缺失证据、活动 worker/reconciler、过期 CAS、错误 namespace 或 malformed/oversized journal 均拒绝写入。重复请求只在原始 digest/revision 匹配时幂等返回同一 resolution。

schema 8 的每个 operation 最多保留 32 条资源义务；activation 只有经过 owner 内部 `SelectionReceipt` 与 `TransferSelectedResource` 才能从 `allocated/release_required` 转为 `retained/retain_selected`。receipt 不超过 4096 字节，绑定 operation、namespace、resource/attempt/generation、资源 revision、原记录摘要/revision、requested purpose、activation/launch、image/source snapshot/manifest 摘要及完整 selection。转换要求同一 admitted transaction 的 requested commit/ready 均 confirmed，且 owner 证据匹配；重复或过期转换拒绝。保留状态不等于释放状态。Finish/Complete 及 R4/R1 读取时必须重新验证 receipt 与原 binding/effects 的链接，缺失或错误 receipt 仍是未结资源债务。状态接口只有在 lifecycle 锁可获取且资源账本校验通过时才能返回 `settled=true`；新操作准入在持锁期间逐个校验资源账本关联的历史 operation，不能因其 outcome 已终结或资源标为 closed 而跳过损坏、孤立或未结记录。正常 install/rollback 路径已接入原生就绪观察与内部 transfer；没有公共 transfer IPC/CLI 入口，历史 ready 不确定的 R1 不得凭当前 ready 自行生成此 receipt。

新候选在创建 activation 目录前记录不可变 allocation/attempt/launch 身份；实际启动沿用该 launch。就绪观察前记录 observer 义务并持久化 pending ready effect，再在同一 IPC 连接上核对内核 peer PID、加载映像、activation/launch、原始 manifest 摘要和稳定 selection。该 observer 义务覆盖此次连接、进程/映像观察器及 rooted manifest 读取句柄；只有其精确清理回执与持久化关闭均成功才能确认 ready。正常失败候选只凭原创建回执删除后关闭义务；清理不确定保留文件与债务。ready 已确认但 transfer 失败不能自动恢复或重放。rollback 复用历史 activation，不伪造新的创建义务，也不关闭其它 operation 的未知债务。
本阶段不提供重放或资源全部回收证明。任何来源快照清理不确定仍使
整个操作 uncertain，即使 requested commit/ready 均已确认。

版本 probe 使用独立的 `preflight/probe` 资源义务，在原生分配或执行前记录
attempt、activation、映像与原始来源快照摘要，不填入服务 launch ID。
prepared probe 的精确清理回执覆盖受监督的进程及后代、输出读取和受保护映像；
stdout/stderr 各最多保留 65536 字节，执行期限 30 秒。超限、版本不兼容或非零
退出不等于清理不确定；只有完整清理回执才能关闭义务。缺失、重复、错身份或
任一原生/持久化清理失败均保留债务，不能继续 Stop、发布 pending 或启动服务。
Linux 进程组监督不声明拦截主动 setsid 逃逸。

已记录的 source 义务仅在隔离副本、原始二进制及原始目录句柄全部清理后关闭。
源自身的副本成员关闭错误单独累计，即使目录已删除仍须记录 Unknown；consumer
在其它资源上的失败不混入源清理结论。记录 source intent 前的失败保留操作级
清理不确定，不伪造尚不存在的资源回执。

managed stop 在准备 IPC/进程/映像观察器前记录一个 observer 义务，并预分配
与 StopIntent 相同的 AttemptID。准备失败也须返回原始组的精确清理回执；缺失、
重复、错身份或隐藏的映像捕获清理不确定不能关闭该义务。owner 在已有事务锁内
完成该组的持久化关闭后，才记录 confirmed/not_applied 停止结果；原有
StopObservation 继续独立证明 Runtime/job 释放，不新增重复账本。

bootstrap 与新事务准入使用同一历史资源检查：即使 operation 已是终态，未知、
损坏或孤立资源仍阻止启动；读取缺失日志不得创建数据库。R4 已解析请求仅接受
原始 revision/digest 的幂等重试，过期或解析后 revision 拒绝；resolution 严格
解码，资源债务仍在所有成功返回前检查。核对只能打开已有数据库，不能因检查
和打开之间文件消失而初始化新的日志。

已准入 worker 使用 worker-scope 资源组归集 admission proof 与查询端点的收尾。
查询端点的监听器、最多 8 个活动连接及超额连接的关闭错误统一保留；所有查询
协程退出且组结账完成后，才记录该次 backend 的终态。验证失败仍发生在准入前，
不因未验证的 parent 创建日志。ACK 投递失败不能取消或重放已准入的 backend；
Unix 专用 ACK 写入返回 EPIPE，不改变进程全局 SIGPIPE 策略。

launcher 的 Attempt.Done 必须等待原 worker 进程退出结果、ACK 观察协程及全部自有
映像/管道/log 资源的首次关闭结果。完整 ACK JSON 不等待 EOF；超时只取消观察，
不杀 worker。原 worker 进程退出后，后代持有 stdout 不能阻止观察结束。非零退出和
关闭不确定仍使启动门禁保持不确定。上述正常收尾组不提供 pre-auth quarantine、
崩溃后 worker 身份、日志自身最终句柄或 bootstrap attempt 的历史回收授权。

来源证明工具使用同一原生进程监督机制。工具根进程退出不等于其后代、输出读取
和句柄已回收；清理不确定保留为独立错误并计入 source 资源组的 unknown 状态。
证明拒绝或服务不可用且清理已确认时，仍是普通来源拒绝，不伪造清理不确定。
验证器的任意输出和错误细节不得转发到用户日志；固定签发策略不因此改变。

查询结果同时包含 `settled` 和 `observer`（查询服务进程的完整 build 身份，含 PID、
路径、SHA 和本机 owner 协议）。终态记录写入后、事务进程尚持有生命周期锁时，
settled 为 false；头须继续只读观察，不能提前报告成功或启动下一事务。
observer 仅是待验证输入：头在同连接获取内核 peer PID，持有原进程退出观察器，
再验证其实际映像及稳定保留 activation 的原始来源证明；Current/Previous/Pending
引用可覆盖替换窗口。缺少验证、查询者退出、选择变化或请求意图不匹配时，均不能接受
成功结果。头还须将记录的 namespace 与提交时保留的目标 namespace 核对，不能因查询期间环境变化而换绑。首次提交的 ready 握手不能为后续新查询连接提供身份担保。

已就绪 owner 的 `runtime.install` 参数含 `operation_id`、`namespace_sha256` 和绝对且规范化的 `bundle`；
`runtime.rollback` 只接受 `operation_id`、`namespace_sha256`。两者的 namespace 均必填，服务端在创建 worker 前与自身配置目录核对，不符返回 `runtime_profile_mismatch`。事务 ID 同上，bundle 最多 32768 字节，
参数 JSON 最多 128 KiB；禁止未知字段、尾随值和回滚携带 bundle。
请求先在当前 owner 内串行进入交接，然后由认证后的事务进程持久准入。
成功回执为 `{accepted:true,operation_id}`，仅说明事务已接收，不代表安装完成。
收到失败、断线或超时后只查询原事务 ID，不重发原请求或更换 ID 隐式重试。
事务进程创建后，即使确认回执丢失，owner 仍保留启动门禁至该进程退出且资源释放确认；
释放不确定则保留门禁。明确的后续新请求必须等待前一事务可安全结束。

---

## 1. 两条连接

TLS 证书校验失败时不会建立 WebSocket。反代传输策略或浏览器 Origin 不符合入口策略时，在 Upgrade 前返回 HTTP 403，不发送 `hello_ok` 或协议 `error`；这不是新增协议错误码。客户端不得据此推断设备或 grant 状态。入口配置见 [relay.md](relay.md) §5。

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
| `access.request` | **仅 device** | request_id, account_token, payload | 管理本连接证明的设备归属、分享及逻辑授权码 |
| `access.result` | Relay → device | request_id, code? / payload?, device_context? | 成功为对应动作的结果及设备上下文，失败为错误码 |
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

未来 OIDC/SSO provider token 必须先经过独立 issuer/audience/JWKS/claim 验证，再由管理面把外部 `issuer+subject` 映射到本地 principal/membership；token 自报的 tenant、principal、role 或 device scope 不被接受。当前 Runtime 只实现 provider-neutral 的离线验证原语，不执行 issuer discovery、JWKS 网络获取、浏览器登录或 logout；未完成本地映射和每次成员/设备访问复验时，不得把 OIDC token 当作 `account_token`、grant 或设备授权。

本地映射服务使用版本化 issuer 配置和 `(issuer,subject)` 唯一键。issuer、映射和 membership 必须同时 active、未过期，且 tenant/principal/membership/version 与本地当前成员一致；配置、轮换和撤销只接受严格递增版本，任何缺失、过期、撤销或版本不匹配都 fail-closed。`target info.oidc_mapping_contract` 与 `xallor-remote relay oidc` 只描述该边界；MCP/GUI 仍通过本机 Runtime IPC，未实现真实 IdP discovery、浏览器流程或 JWKS 网络获取。

`access.request` 的 `request_id` 非空且不超过 128 字节，`payload` 不超过 16 KiB；未知 action 或非法参数返回 `agent_error`。payload 为以下之一：

| action | 参数 | 作用 |
| --- | --- | --- |
| `claim` | 无 | 将未认领设备绑定到当前账号的空间及主体 |
| `status` | 无 | 查看归属及分享；已认领时仅所有者可查询 |
| `share` | membership_id, operations, ttl_seconds | 对有效成员签发或替换有期限的操作授权 |
| `unshare` | share_id | 删除本设备的指定分享 |
| `transfer` | membership_id | 转移到有效成员所属的空间及主体，删除所有旧分享 |
| `code.revoke` | code_id | 所有者撤销逻辑授权码及其派生授权；WIP |
| `code.rotate` | code_id, new_code_id, new_secret | 所有者轮换逻辑授权码；WIP，不自动重试 |

`operations` 仅接受 `info`、`read`、`write`、`exec`、`processes`、`handoff.submit`、`handoff.get` 的非空集合；cancel 始终只属于原连接，不是可授予他人的权限。分享期限为 60 至 7776000 秒，最多 128 份未过期分享，超限 `quota_exceeded`。成功快照为 `{device_uid,ownership_id?,owner_tenant?,owner_principal?,shares:[{id,membership_id,operations,expires_at}]}`，时间为 Unix 秒。角色或权限不符返回 `unauthorized`。设备身份取自实际 device 连接，忽略客户端自报目标；client 连接不能调用此入口。

授权码动作使用独立成功结果，不返回上述归属快照：`code.revoke` 为
`{code_id,state:"revoked"}`，ID 必须匹配请求。`code.rotate` 的 Relay payload 为
`{previous_code_id,code:{code_id,device_uid,ownership_id,type,generation,issued_at,expires_at?,scope,require_recipient_approval,state},intent_sha256}`。
previous_code_id 匹配请求 code_id，内部 code_id 匹配 new_code_id；generation 为
2 至有符号 64 位整数上限。device_uid/ownership_id 非空且与有效 device_context 一致。
type 为 one_time/expiring/long_lived；scope 是上述操作的非空、无重复集合；approval
字段必须是明确的 bool。issued_at 和限时码 expires_at 为 RFC3339，后者晚于 issued_at；
其他类型无 expires_at。state 保留持久化的 active/revoked/consumed/expired 值。

intent_sha256 使用现有 rotation intent v1：旧 ID、新 ID 与新 secret 的 SHA-256
共同参与摘要。Runtime 按原请求重算并校验后，只向 IPC/CLI/MCP 返回
`{previous_code_id,code:{...}}`，剥除 intent_sha256；任何成功或失败回执都不回显 secret。
回执来自同一已授权写事务中的原替代码快照，只有 commit 确认后发布；不追踪后续轮换
得到的后代，不进行第二次事务外读取。自然到期但持久 state 仍为 active 时，保留原
expires_at，不能把回执成功或 active 值解释为现在可用、已批准会话或重新签发。

两种成功回执仍必须携带有效 device_context；Runtime 先同步上下文，再校验结果。
缺失上下文会断开连接；结果类型、ID、意图摘要、设备归属或元数据不符时返回结果未确认，
不得按已回滚处理或自动重放。旧的含 secret 回执形状不再接受。同意图重试仍需重新授权。

`reset` 不是协议消息：CLI 清空本机数据目录，并发送 `revoke`。

`invoke` 的 `op`：`exec` / `read` / `write` / `processes` / `info` / `cancel` / `handoff.submit` / `handoff.get`。

Relay 在通过准入后重新构造发往 device 的非 cancel `invoke`，保留 `op,payload`，为 `exec_id` 分配新的内部调用 ID，添加服务端推导的 `scope`，不转发客户端的身份或凭据字段。`scope` 为 `{authority,device_uid,ownership_id?,grant_id?,principal_id?,tenant_id?}`；所有 ID 为 32 位小写十六进制。`ownership_id` 与 `grant_id` 恰有一个：已认领设备使用归属实例并要求主体/租户；未认领使用共享授权实例，账号主体/租户可同时存在或同时缺省。authority 来自当前 Relay 的持久身份。客户端自报 scope 被覆盖，目标仅信任已认证 Relay 连接。

scope 是稳定执行命名空间，不是访问凭据或权限快照。凭据续签、成员代次或重新签发同主体分享不改变它；新的设备 UID、归属实例或未认领共享授权实例会改变它。每次新调用仍独立完成现有认证和操作授权。handoff 收到缺失或非法 scope 返回 `unauthorized`，不回退设备级查询。原连接的 cancel 仍按 inflight 归属路由，不需要客户端传 scope。

`info` 的 JSON 结果包含 `build`：version、revision、built_at（可省略）、dirty、protocol（当前为 1）、profile_format（本机持久化格式）、os、arch；用于区分运行制品与代码版本。未知 revision 为 `unknown`，不猜测提交。Relay health 返回同形构建身份。`execution_backend` 是目标 Runtime 当前真正使用的进程边界：`name` 目前为 `native_user`，`privilege` 为 `same_user`，`sandbox` 为 `none`；Windows 的 `process_tree` 为 `job_object`，Linux 为 `process_group`。`available=false` 或 `code=backend_unavailable` 时不得把执行当作可用，也不能把进程树回收边界当成低权限或 OS 沙箱。`device_lifecycle` 是 enrollment/claim/transfer/revoke/reset 的静态状态机契约；它不授予 client 管理权限，未知结果必须按契约停止自动重试并重新查询。

`info.execution_backends` 返回目标注册表的 capability 列表；当前只有 `native_user` 可用，`low_privilege`、`cgroup`、`container` 都是明确 `available:false,code:backend_unavailable` 的预留项。未知 backend 同样拒绝，不按名称猜测能力。目标本机配置字段 `execution_backend` 省略时为 `native_user`；配置 unsupported backend 时整个更新拒绝，持久化配置中的 unsupported backend 阻止 Runtime 启动，不回退默认值。

`exec.backend` 可选，最多 64 字节并计入输入总长；省略或空字符串使用目标本机选择。显式值必须与目标配置匹配且 provider 可用。目标在审批、可执行文件解析、占用预留与 process start 前检查，失败返回唯一 `invoke_nack/backend_unavailable`，不执行 native fallback。控制侧不以自己的 OS 判断对方是否支持 backend。当前 backend 选择范围是 `exec`；read/write/processes/handoff 不因此获得隔离。已启动或 cleanup/release 不确定的情况仍是未知失败终态，不能因为出现 backend 错误而降为“未执行”或成功。

`info.shell` 为 `{configured,selected?,available:[],code?}`：configured 为目标配置名或 auto，selected 是当前能找到的解释器名，available 是本系统受支持且可找到的名字；不能解析选择时返回 code，不伪造 selected。只探测可执行文件，不包含路径、版本或远程凭据。可选解释器及执行语义见 [os.md](os.md)。

- `exec`：`script: {text, shell?}` 或 `program: {executable, args?}` 二选一，另有 `cwd?`、`timeout_ms?`。`text` / `executable` 必须非空；args 为字符串数组，省略时无参数。script.shell 省略时使用目标配置，显式 auto 使用目标自动选择。cwd、程序路径及 PATH 均属于目标设备；文本、参数不得含 NUL 或非法 Unicode，未知字段与非法形状返回 `invalid_execution`。可选字段应省略，不使用 null。CLI、IPC 和 MCP 使用同一输入形状，IPC/MCP 另带路由字段 device_id。脚本不跨语言翻译，程序参数不拼接成脚本。具体目标系统约束见 [os.md](os.md)。
- `cancel`：client 只提供 `exec_id`，仅取消**本条 client 连接**登记的 inflight；对别人的或未知 id 一律 `unknown_exec`（不区分「存在但是别人的」）。Relay 重建普通取消帧，不转发 client 自报的 code。

Relay → device 的 `invoke/op=cancel` 可带 `code=unauthorized`，专门表示原调用授权已撤销；
code 为空仍表示普通取消。Runtime 仅从已认证 device 数据通道处理该通知，其他 code
不触发取消。运行中的前台 exec 保留首个取消原因：授权撤销对应 `authorization_lost`，
用户取消对应 `cancelled`，并仍须完成真实进程清理才返回终态；已开始的用户取消不会被
后到的撤销重写。尚未启动时沿用现有 nack 语义。该字段不是 client 的授权声明。
- `read`：路径 + `head`/`tail`（行）或 offset/limit（字节）
- `write`：路径 + **完整** `content`。覆盖；无 append、无 patch

`info.operation_policy` 返回 `{version,sandbox,operations}`，是 CLI `policy --json` 和 MCP `remote.device_info` 当前读取的同一静态操作矩阵；GUI 后续通过本机 IPC 接入后复用该字段。每行说明 Relay 动态授权、目标边界、审批、终态和重试规则；它不返回当前调用者的租户/成员授权，也不把 `sandbox:none` 包装成 OS 隔离。`info.execution_modes` 返回 `["script","program"]`，供 Agent 选择输入方式。示例：

`info.entitlement_contract` 返回稳定的 feature/unit/scope 说明，不返回余额或当前订阅。权益 decision 必须在 Relay/管理面基于当前 tenant、principal、membership version 和 expiry fail-closed 完成；传输硬顶、IP 保护、本机 job ledger 和协议大小上限保持独立。

`info.usage_event_contract` 返回隐私最小化 usage event 字段、幂等和 unknown 规则。它与本机 `activity.json` 分离：activity 是 bounded 操作元数据历史，不是防篡改审计或账单；usage event 也不等于账单，只有独立管理面在确认终态后才能进一步计量。

`info.relay_registry` 与 Relay `/health.registry` 返回单 Relay sticky routing contract；health 是进程/build liveness，不是 device readiness。Relay 只允许每个 device UID 一条 active device connection，旧连接/旧 route 的迟到事件必须丢弃；多地域 HA 不属于 v0。

`info.relay_failover` 与 `/health.failover` 只返回 provider-neutral 四态和 no-replay contract。`healthy` 不等于 target ready，`unknown` 不等于 stopped/completed；网络分区、旧 route 或缺少终态时必须保留 unknown，不自动切换 ownership，也不重放有副作用的 invoke/handoff。

`info.plane_contract` 与 `/health.plane` 返回 control/data plane 分离契约。管理凭据、租户/设备管理和持久状态不进入数据面载荷；MCP/GUI 只经本机 Runtime IPC，Relay 不提供管理 HTTP API。数据面仍必须每次通过实时身份/访问复验，缺少管理确认或终态时不得猜测成功。
该契约 version 2 新增 `time`、`retention` 与 `aggregation`。At 为规范 `time.RFC3339Nano` UTC `Z` 字符串，最长 30 字节，范围闭区间 `1970-01-01T00:00:00Z` 至 `9999-12-31T23:59:59.999999999Z`；最多 9 位小数，尾部多余零、省略时区、偏移形式及逗号小数均拒绝。Append 省略 At 时生成 UTC 时间，持久化 At 不能省略。这里是表示/范围校验，不以时间戳证明真实终态或业务可信性。

同一 ledger 最多保留 5000 个事件及 5000 个淘汰键 SHA-256 tombstone，版本化 envelope 总长最多 4 MiB。保留事件相同 key/event 重复为 no-op；淘汰键的任何重用均 `ErrConflict`。tombstone 满后，需要淘汰新键的追加返回 `ErrRetentionFull`；不丢旧键、不自动开启新幂等窗口。completed 汇总仅限保留事件的精确 tenant/membership scope，int64 溢出返回 `0,ErrOverflow`。这些错误属于本地 usage Go API，不引入新的远程 invoke 错误码。

执行请求的可选 `backend` 由目标 Runtime 解析；当前只有 `native_user` 可用。未知或未实现后端在启动前返回 `backend_unavailable`，不回退为 native_user。该字段不表示 OS 沙箱或权限降级。

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
| `runtime_operation_unknown` | 本机生命周期查询未找到指定事务；不表示请求已被拒绝或未执行，不能据此重放安装或回滚 |
| `invalid_execution` | 执行输入形状、文本或参数无效 |
| `program_unavailable` | 目标无法安全找到指定程序；不回退到 Shell |
| `backend_unavailable` | 请求与目标本机 backend 不匹配或 provider 不可用；启动前拒绝，不回退 native_user |
| `unsupported_program` | Windows program 模式选择了批处理文件或 cmd.exe，须改为显式脚本调用 |
| `unsupported_shell` | 目标系统不支持配置的解释器名 |
| `shell_unavailable` | 目标无法安全找到所选解释器；不会换壳重试 |
| `quota_exceeded` | Relay 或目标 Runtime 的资源上限已满（如分享数、在途请求或本机作业容量） |
| `entitlement_unavailable` | 权益提供方不可用；必须 fail closed，不回退为允许 |
| `entitlement_missing` | 当前 tenant/membership 没有所需 feature |
| `entitlement_expired` | 当前 membership 权益已过期 |
| `entitlement_inactive` | 当前 membership 已停用 |
| `entitlement_exceeded` | 请求的 feature 数值超过权益额度 |
| `resource_uncertain` | 本机作业清理或持久释放未确认；拒绝把重试当成 at-most-once，也不自动重放副作用 |
| `resource_unavailable` | 本机作业账本不可用；执行未获准，不返回成功终态 |
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

`exit` 和 `error` 是互斥终态，示例最后两行属于不同调用。可写输出通道上，执行或本地拒绝恰有一个终态且位于最后；管道关闭或 CLI 被终止时可能没有完整终态，调用者必须视为结果未确认。不得把 EOF 当成功。命令产生的 JSON/换行只放入 data，不作为事件解析；输出边界是块，不承诺一帧一行命令输出或 stdout/stderr 之间的真实时间顺序。

`exec_id` 仅在已观测时出现，可能随空 stdout 事件先到达；这不是目标进程已启动的证明。exit 字段沿用本协议终态语义，缺失的可选元数据省略，不伪造 0/false；completed 缺少有效整数退出码时转为 result_unknown。错误保留已知协议 code，不根据一般传输错误推断命令是否开跑。发送失败也可能已部分发送，所以返回 result_unknown。

本地语义输入校验失败为 invalid_execution/too_large；Runtime 启动或连接失败为 runtime_unavailable。当命令已定位到 exec 且选择 JSONL 时，未知 flag、缺少选项值、值类型错误等参数解析失败也返回唯一的 `{"type":"error","code":"invalid_execution","message":"…"}`，进程退出 1；不带 exec_id/exit_code/duration_ms，不启动 Runtime 或发送执行请求。stderr 保留人话诊断。`--help` 仍成功显示普通帮助，不产生 error 事件；尚未定位到 exec 的根命令路由错误及其它命令保留普通错误通道。

JSONL 选择在 `--` 前识别，允许出现在错误选项之前或之后；作为其它选项值或 `--` 后参数的字面 `--jsonl` 不选择格式。重复设置时最后一个显式布尔值生效，`--jsonl=false` 关闭格式；非法的显式布尔值本身也作为 JSONL 参数错误返回。进程退出状态仅在 completed 且 exit_code=0 时为 0，其余为非零；它不是远端原始退出码透传，远端值读取 exit_code。远端 stderr 在 JSONL 中只作为标准输出上的 stderr 事件。输出写入失败立即停止并关闭 IPC，不补写成功或重试执行。此修复不改变 Request、IPC、MCP 或 WSS 消息。

---

## 5. 大小（数字只在这里改）

| 项 | v0 |
| --- | --- |
| WSS 单帧 `data` | 64 KiB（解码后）；超则丢该数据帧并标 truncated |
| WSS 读上限 | `MaxWSMessageBytes` = 6 × (write 上限 + 路径上限) + 8 KiB 信封；覆盖 JSON `\u00XX` 最坏转义 |
| Runtime IPC / MCP 单条 JSON | `MaxIPCMessageBytes` = 6 × (write 上限 + 路径上限) + 8 KiB 信封，不含末尾换行；请求/回复同限额，未收到换行也须在超限时关闭连接。输出仍逐块传输，不累计事件副本；GUI 桥另有更小的本机展示预算 |
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
| read `head` / `tail` | 最多 1024 行；每个选中行按窗口预算保留前缀，超出返回 `truncated=true` |
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

## 逻辑授权会话（非 GUI WIP）

当前 v0 `grant` 仍是可轮换/吊销的共享 bearer credential。一次性、限时、长期授权码以及
`session.request` / `session.approval.*` / `session.result` 是正在实现的逻辑会话协议，不能
与 v0 grant 的稳定能力混写。新路径由 Relay 负责 reservation、最终消费和 session authorization，
Runtime 负责本地接收方批准；批准请求不携带明文授权码，unknown 不自动重兑。

目标连接的授权代次由 Relay 生成并持久化，不接受客户端传入。每次目标重新认证注册，
先停止旧 slot 路由，再在同一事务切换代次、撤销旧 session/resume、把旧 reserved
attempt 标为 unknown；新连接只有在事务成功且 greeting 入队后才能发布。
reservation 和 session 保留批准所针对的 device UID/代次，Reserve、Finalize、
ConsumeResume 都在事务内核对；迟到的旧连接 Drop 只按其捕获的代次失效，不影响新代次。
队列拒绝、身份写入失败或设备删除同样触发此失效边界，不能依赖已被清空的 conn。
批准发布还须匹配原 pending 对象、client 状态和原目标 slot/connection；最终 invoke
路由复核 session 的目标代次。该机制限定单个活跃 Relay authority，不宣称多活 fencing。

Relay schema 12 撤销历史上无法证明目标代次的 active session/resume，保留已消费码；
legacy reserved 转为 unknown，已有多条 unknown 历史不合并、不释放。一次性码的新
reservation 在事务内检查 reserved/unknown 独占占位，避免未知尝试被重新兑换。
当前 schema 重开数据库本身不轮换代次；重新注册与下述授权设置变化触发对应代次迁移。迁移只在本地副本验证，
没有执行生产数据库升级。

同一目标连接上的有效设置变更也构成授权边界：关闭入站时，在更新设置的同一事务中
退休现有 session epoch，撤销旧 session/resume 并使旧 pending reservation 进入 unknown；
重新开启时创建新代次。共享访问绑定实际改变同样轮换；同值设置，以及已认领设备上
不参与授权的 grant 同步，不轮换。内部设备快照带有事务返回的 epoch 状态，不新增
客户端协议字段，也不因缺少 target 行而隐式开启会话功能。

Typed 授权码的撤销/轮换已在 WIP 中接入域规则、存储事务、管理协议、Runtime IPC、
CLI `device code revoke/rotate` 和对应 MCP 工具，以及前台路由失效通知与续租约束。
顶层 CLI `rotate`/`revoke` 仍是 v0 grant/设备动作，两者不能混用；上述 WIP 不代表
正式发布或完整跨平台包验收通过。管理动作及同意图回执重试均复验当前 actor/device；
事务顺序和未知结果边界见 [dataplane.md](dataplane.md)。
显式撤销会原子作废该 code ID/generation 的派生 session/resume；消费和 unknown 历史
不回退。轮换保留策略和原截止时间，只对仍有资格的旧码生成新 ID/代次与回执；已使用
或仍有 reserved/unknown 的一次性码不能借轮换再获得一次资格。schema 13 保存稳定
轮换意图回执，相同意图只读取原替代码当前元数据，不重新签发或复活。自然到期仍不
自动结束已有 session。不能把这些存储状态当作任务已经停止或完整管理功能已交付。

Resume 消费提交后、active 结果发布前，Relay 在同一设备管理 gate 内复读持久 session，
核对原 code ID/generation、UID/目标代次、scope、期限及 active 状态，再发布并绑定连接。
同 code 撤销/轮换先完成时不得返回旧 active 或占住新连接的 session slot；同设备无关
code 的管理不阻断合法恢复。无法发布仍返回 unknown，不退还已消费 proof、不重放消费。

前台 `exec` 的续租现在还检查该任务最初获准的逻辑 session，而不借用控制连接后来
采用的新 session。明确结束/撤销/过期或失去 exec scope 时拒绝续租，旧 route 永不复活；
SQL 暂态错误不授予新租约，也不刷新旧期限。返回时长不得越过基础授权预算或 session
截止，毫秒换算向下取整。到期状态的尽力持久化发生在拒绝回复和匹配解绑之后，失败
不冒充 durable end，也不阻止同连接重新取得批准。旧输出/终态和 transport 占用仍
独立结算。此检查受租约期限约束，尚不能替代管理操作的 per-code 发布 fence，也不覆盖
后台 handoff 的 session 传播或宣称即时零窗口撤销。

后台 handoff 的 session 委托在 WIP 中绑定原批准的 ID、code/generation、目标代次和
scope；签名期限取委托年龄上限与原 session 截止时间的较早者。续租时复验持久会话，
当前更短截止时间同样限制租约，不能通过延长或移除存储中的截止时间延长原委托。
所有身份/权限读取完成后重新计算剩余期限；跨过截止时拒绝续租，不返回新的完整租期。
这属于委托模块的授权边界，不代表独立 Runtime 后台任务的回执、进程清理或发行验收完成。

设置写入前停止新准入/批准发布，但保留既有控制连接和执行流；入站关闭后旧执行不能
重新获得续租，重新开启不复活旧 lease。Relay 只在目标终态或真实路由丢失后释放
transport 占用，不把授权撤销当作 Runtime 进程清理证明。写入错误或提交结果未知时
保持路由停止；若提交已发生，新 epoch 可能仍在数据库中 active，不能声称已回滚或
全部 inactive。重新注册必须建立新代次，旧同意不会恢复。

`session.request` 的 `code_generation` 必须与当前授权码一致，`session_scope` 必须是
授权码允许范围的非空子集；Relay 在创建 reservation 的同一事务内检查，越界或旧代次
返回 `unauthorized`，不向接收方请求批准。最终批准只持久化本次批准的 scope 子集，
不能复制授权码的全部权限；active `session.result` 的 generation/scope/expiry 来自
事务生成的授权记录。resume 延续同一记录，不扩权。

resume 消费事务还必须读取当前设备入站、凭据/成员及分享授权，并与原绑定比较；
连接建立时的身份快照不能替代此复验。消费前再次检查凭据和分享的到期时刻。
已撤销/到期、成员版本变化、分享替换或关闭入站返回 `unauthorized`，不消费凭证。
确认回滚的 SQLite 冲突重试须重新读取这些事实；未知提交结果不得据此自动重放。

消费提交成功后，resume 的 active 回执须在路由锁内复核原控制连接、原目标连接、
入站与有效期，并与会话绑定一起按序入队。连接已失效或回执入队失败，不返回新凭证，
尝试撤销未交付授权并返回 `session_unknown`；不会退还已消费凭证。撤销失败不能报告
清理成功；回执已入队也不等于已送达。

Relay schema 11 会撤销旧 WIP 数据库中无法证明原始批准范围的 active session 及
active resume 凭证，并保留授权码消费记录；需要重新授权，不能推断或恢复原始同意。

当前代码已有 WIP envelope、状态机、SQLite 事务、Relay wire 和 Runtime IPC 切片，但尚未完成
正式 CLI/MCP 发布契约、稳定 reconnect、生产授权码类型发布或完整跨平台端到端验收。
