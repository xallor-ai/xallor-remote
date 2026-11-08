# Relay

SSOT：职责边界、落盘、官方预算、运维。**连接 / inflight / 流式 / cancel / 断线 / 背压**只在 [dataplane.md](dataplane.md)。消息与限额见 [protocol.md](protocol.md)。目标命令/文件策略不在这里执行。授权角色见 [credentials.md](credentials.md)。

---

## 1. 做 / 不做

**Relay 是有鉴权的实时消息路由器**：device 连接登记、连接认证与设备访问授权、按 `exec_id` 转发、生命周期错误。不是执行服务器。

做：登记 device / client 连接；调用身份与设备授权服务；按 `exec_id` 转发；online/offline 与踢线；**接受 device 侧的** rotate/revoke/inbound；官方实例连接/注册预算。

不做：解释命令、读目标盘、**持久化** stdout/文件正文/命令及其片段、目标命令/文件策略判定、P2P、远程桌面、账号登录（v0）、为 invoke 另开 HTTP API。

**飞行中的 invoke 与 stdout，官方 Relay 在 TLS 终止后看得见。** 这是 v0 产品事实。自托管可换。E2E 以后再说。

控制面与数据面 v0 可同进程，逻辑上分开。Hosted Relay = **transport / rendezvous**，不是用户数据平台。

路由表与转发算法见 [dataplane.md](dataplane.md) §4–§8。这里不重复。

---

## 2. 连接（摘要）

同一 `device_id`：**一条** device 连接（新的踢旧的）。**多条** client 连接，v0 一条 client 只绑一个目标。细节与踢线后果见 [dataplane.md](dataplane.md)。

`device.settings` / `revoke`：**只处理来自对应 device 连接的消息**。client 连接上的同类消息丢弃或 `unauthorized`。

---

## 3. 落盘

v0 用 **SQLite** 单文件（`--data` 目录内），驱动见 [stack.md](stack.md)。必须持久：

| 集合 | 内容 |
| --- | --- |
| `devices` | uid（内部身份主键）, id（唯一显示号码）, secret_hash, inbound, last_seen |
| `device_grants` | id（授权实例主键）, device_uid（设备外键）, token_hash；当前共享授权的唯一事实来源 |
| `authority` | 本 Relay 数据域的稳定签发方 ID |
| `execution_signing_key` | 本数据域后台执行委托的随机 32 字节 HMAC 密钥；只由 Relay 启动组装加载，不对客户端输出 |
| `principals` / `tenants` / `memberships` | 用户或服务主体、个人或组织空间、成员角色/启用状态/版本 |
| `principal_credentials` | 凭据 ID、token_hash、成员关系、到期时间、撤销状态；不存明文 token |
| `device_ownership` | 设备 UID、唯一归属实例 ID、所属租户、所有者主体 |
| `device_shares` | 分享 ID、归属实例外键、接收者成员关系、固定操作位集合、到期时间 |

禁止持久：grant/secret 明文、stdout、文件正文、路径、命令及其片段或载荷摘要。

设备与授权绑定模型由 `internal/access` 管理；`DeviceStore` 提供事务存储。注册创建随机内部 UID 或校验现有身份，禁止覆盖已登记设备的密钥；更新和删除必须同时匹配内部 UID 与设备密钥哈希。显示号码可复用，内部 UID 不可复用。不存在、身份不匹配与存储故障分别处理。数据库故障返回 `relay_error`，不得确认注册成功；授权修改必须提交后才更新内存，持久化失败时关闭设备连接并保留原状态。

当前每台设备有至多一个共享授权实例。授权 ID 由存储生成，调用者不能选择；授权码哈希不变时保留 ID，以支持重连时幂等同步。换码或清空后重新签发生成新 ID，即使恢复原来的码值也不能恢复旧连接的权限。设备与授权修改在同一事务中提交；删除设备通过外键级联删除授权，所有池连接启用外键。共享授权仅证明持有码，不等于用户、租户或付款主体；租户关系与权益模型见 [platform.md](platform.md)。

设备身份操作通过可取消、按设备回收的协调器串行化；数据库操作不持有路由锁。在线/离线判断只访问内存；带账号身份的新调用另行校验凭据和成员有效性，过程见数据面。身份等待使用连接取消信号和协议规定的期限，数据库连接池等待也可取消。授权写入失败立即停止向该连接路由，不能等网络关闭排空后才失效；已提交的变更即使随后断线，也必须反映到同一身份的缓存，不能覆盖新的设备连接。

SQLite 的每条连接配置短 busy wait（50ms），避免长时间原生忙等遮蔽取消。仅对 `SQLITE_BUSY` 且事务明确回滚成功的身份操作进行有取消信号的重试；提交错误和其它错误不重试。这是数据库事务处理，不会重放远程命令。

SQLite 使用 `user_version` 管理结构版本。当前版本 6 加入后台委托签名密钥，升级与重开保留已有签发方、设备、归属、主体、成员和凭据身份；更早结构升级时保留显示号码、凭据哈希、入站与 last_seen。运行时只使用最终结构，不存在双写或双读分支。结构变更在事务中完成，不支持高版本数据库由低版本程序打开。部署升级前必须备份 Relay 数据目录；生产部署仍需明确授权。删除表不承诺安全擦除 SQLite 空闲页或旧备份中的既有数据。

设备删除级联删除归属及分享；归属转移事务替换归属实例并级联删除旧分享。主体/成员不随设备删除。持久权限位固定为 info=1、read=2、write=4、exec=8、processes=16、handoff.submit=32、handoff.get=64，不因显示顺序变化重新赋义。存储事务只重试已经确认回滚的 busy 失败，不自动重试提交结果不确定的变更。

已鉴权请求的转发和终态处理不写数据库，也不为每次操作创建后台持久化任务。Relay 不提供命令审计功能。后续会员用量应由独立的可信主体、资源租约和最小计量模块负责，不能从命令内容或一次转发完成推算计费与真实作业结束。此处尚未实现该计量模型。备份 Relay 数据目录不等于备份用户文件。

---

### 本机身份管理

以下命令要求 Relay 数据目录的本机操作系统权限，不是公开管理接口。`relay --data <dir> principal create --name <name>` 创建主体和个人空间并输出 JSON 凭据；指定 `--tenant <id>` 将新主体加入已有空间，`--kind service` 创建服务主体。`principal space --name <name>` 创建团队空间。成员角色不自动赋予设备访问权；设备访问由归属和明确分享决定。

`principal issue --membership <id>` 为同一成员签发新凭据，旧凭据独立存在；`principal revoke --credential <id>` 撤销指定凭据；`principal membership --membership <id> --active=false` 停用成员，`true` 恢复并推进成员版本。签发支持 `--ttl`，期限见协议合同。

将签发输出保存为 UTF-8 JSON 后，在控制侧运行 `xallor-remote account use --credential-file <file>`，`account status` 只显示是否配置，`account clear` 明确移除配置。Windows PowerShell 5 的 `>` 会生成 UTF-16 文件，不可直接用于保存此 JSON；可通过 `[System.IO.File]::WriteAllText(path, json, [System.Text.UTF8Encoding]::new($false))` 保存无 BOM UTF-8。凭据只交给需要使用它的控制侧；CLI/MCP 的普通远程调用仍通过同一个 Runtime。

### 被控机的归属与分享

被控机导入身份凭据并连接 Relay 后，`device claim` 将本机设备认领到当前空间。`device owner` 查看归属和分享；`device share --member <成员关系ID> --allow info,read --ttl 24h` 授予指定操作；`device unshare --share <分享ID>` 撤销；`device transfer --member <接收者成员关系ID>` 转移并作废原分享。这些命令经本机 Runtime 的 device 连接完成，不把设备密钥交给控制方，也没有公开的 HTTP 管理入口。

控制方导入对应成员的账号凭据后可用 `peer add --id <设备ID>` 添加已认领设备，无需共享码。权限、未认领模式和作业边界见 [platform.md](platform.md)；超时或断线不能推断事务是否提交，先核对归属/分享，转移时可由接收者确认，再决定下一步。

## 4. 官方实例保护上限

开放 `hello_device` 会被滥用（占连接、当 C2）。v0 硬顶，超限 `quota_exceeded`：

- 每源 IP：注册/上线频率、同时 device 连接数、带宽
- 每 device：并发 inflight（与 protocol 一致）

自托管默认不启用或自行加大。数字实现时可调，**必须有开关和上限**。

---

## 5. 运维

默认监听 `127.0.0.1:8443`，仅供本机 `ws://` 开发。明文模式只接受数值 loopback 监听地址（`127.0.0.1` / `[::1]`）；主机名即使当前解析到 loopback 也不作为安全依据。非 loopback 监听必须配置内置 TLS 或显式 HTTPS 反代模式，否则启动失败，且 CLI 在打开数据目录前校验。

直接提供 WSS（TLS 最低 1.2，证书/私钥为 PEM，需同时提供）：

```text
xallor-remote relay --listen 0.0.0.0:8443 --tls-cert ./cert.pem --tls-key ./key.pem --data ./var/xallor-remote
```

客户端设 `XALLOR_REMOTE_RELAY_URL=wss://<证书域名>:8443/remote`，健康检查同端口 `/health`、`/remote/health` 或 `/xr/health`，均走 HTTPS。证书须受客户端系统信任；不要关闭校验。换证后需重启 Relay。

反向代理终止 TLS：

```text
xallor-remote relay --listen 127.0.0.1:8443 --reverse-proxy --trusted-proxy 127.0.0.1 --data ./var/xallor-remote
```

示例 [nginx-relay.conf.example](../deploy/nginx-relay.conf.example)。公网反代必须覆盖 `X-Forwarded-Proto` 为 `https`，覆盖 `X-Forwarded-For` 为实际客户端 IP；后端仅对反代开放。跨主机后端必须使用受保护的私网/隧道，不能把明文代理链路暴露公网。`--reverse-proxy` 与内置 TLS 互斥，必须有 `--trusted-proxy`。所有请求（含健康检查）均须来自可信 socket peer 且携带唯一 `X-Forwarded-Proto: https`，否则 HTTP 403。本机反代模式探活也要带该头。

`--trusted-proxy <IP或CIDR>` 可重复，默认不信任任何代理，拒绝全网 `/0`。仅对可信 socket peer 读取 XFF，从右向左剥离可信代理，遇到第一个非可信 IP 停止；不会采用它左边的伪造地址。链中任一值无效则回退 socket IP；IPv4-mapped IPv6 归一化为 IPv4。该有效来源同时用于 hello、连接、字节限额与拒绝日志；非可信直连的转发头不影响判断。不要把普通客户端网络配成可信代理。

默认拒绝所有携带 Origin 的 WebSocket；原生 Runtime/CLI 不发送 Origin，可照常连接。浏览器须显式配置 `--allowed-origin https://console.example`（可重复），按 scheme/host/port 精确匹配，不支持通配符或路径；显式 `:443` 与省略端口是两个配置值。IPv6 示例 `--allowed-origin https://[::1]:3000` 也按地址和端口精确匹配。空、`null`、多个或未知 Origin 返回 HTTP 403；Host/X-Forwarded-Host 不能绕过 allowlist。Origin 不是身份凭据，设备/授权校验仍必须通过。

以上仅为自托管配置说明，不代表官方实例已部署；本轮不修改官方环境。

仓库旧 ECS 部署脚本现在要求显式 `--trusted-proxy <已核对的网关源IP/CIDR>`，生成安全反代模式的 systemd 单元；不自动猜测 Docker 子网。本机探活额外信任 `127.0.0.1` 并带 HTTPS 转发标记。脚本须另行取得生产部署授权；运行前备份并 diff 现有单元、反代和防火墙，后端仅对已配置代理开放。模板回归检查只读本地文本，不导入或执行 ECS 连接脚本。

代理配置接受 IPv4、IPv6 地址/CIDR。IPv4-mapped IPv6 CIDR 同步转换为 IPv4，例如 `::ffff:127.0.0.1/128` 等价于 `127.0.0.1/32`，`::ffff:127.0.0.0/120` 等价于 `127.0.0.0/24`；mapped 前缀短于 `/96` 拒绝，转换后的 `/0` 也拒绝。无需把相同代理按两种表示法重复配置。

部署单元生成器只接受不含 zone（`%接口名`）、空白或控制字符的 IP/CIDR。它先检查原始 mapped IPv6 地址再转换和归并网段，以结构化参数生成唯一 ExecStart，非法输入在连接 ECS 前拒绝。
