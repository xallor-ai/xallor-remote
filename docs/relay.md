# Relay

SSOT：职责边界、落盘、官方预算、运维。**连接 / inflight / 流式 / cancel / 断线 / 背压**只在 [dataplane.md](dataplane.md)。消息与限额见 [protocol.md](protocol.md)。策略不在这里执行。授权角色见 [credentials.md](credentials.md)。

---

## 1. 做 / 不做

**Relay 是有鉴权的实时消息路由器**：device 连接登记、grant 校验、按 `exec_id` 转发、生命周期错误。不是执行服务器。

做：登记 device / client 连接；校验 grant；按 `exec_id` 转发；online/offline 与踢线；**接受 device 侧的** rotate/revoke/inbound；官方实例连接/注册预算。

不做：解释命令、读目标盘、**持久化** stdout/文件正文/命令及其片段、策略判定、P2P、远程桌面、账号登录（v0）、为 invoke 另开 HTTP API。

**飞行中的 invoke 与 stdout，官方 Relay 在 TLS 终止后看得见。** 这是 v0 产品事实。自托管可换。E2E 以后再说。

控制面与数据面 v0 可同进程，逻辑上分开。Hosted Relay = **transport / rendezvous**，不是用户数据平台。

路由表与转发算法见 [dataplane.md](dataplane.md) §4–§8。这里不重复。

---

## 2. 连接（摘要）

同一 `device_id`：**一条** device 连接（新的踢旧的）。**多条** client 连接，v0 一条 client 只绑一个目标。细节与踢线后果见 [dataplane.md](dataplane.md)。

`grant_rotate` / `revoke` / `inbound_set`：**只处理来自对应 device 连接的消息**。client 连接上的同类消息丢弃或 `unauthorized`。

---

## 3. 落盘

v0 用 **SQLite** 单文件（`--data` 目录内），驱动见 [stack.md](stack.md)。必须持久：

| 集合 | 内容 |
| --- | --- |
| `devices` | id, secret_hash, grant_hash, inbound, last_seen；设备身份与当前授权状态的唯一事实来源 |

禁止持久：grant/secret 明文、stdout、文件正文、路径、命令及其片段或载荷摘要。

设备存储由 `DeviceStore` 管理：注册在事务中创建或校验身份，禁止覆盖已登记设备的密钥；更新和删除必须校验设备密钥哈希。不存在、身份不匹配与存储故障分别处理。数据库故障返回 `relay_error`，不得确认注册成功；授权修改必须提交后才更新内存，持久化失败时关闭设备连接并保留原状态。

身份操作通过可取消、按设备回收的协调器串行化；数据库操作不持有路由锁。已鉴权请求的在线/离线判断只访问内存，不再同步查询数据库。身份等待使用连接取消信号和协议规定的期限，数据库连接池等待也可取消。授权写入失败立即停止向该连接路由，不能等网络关闭排空后才失效；已提交的变更即使随后断线，也必须反映到同一身份的缓存，不能覆盖新的设备连接。

SQLite 的每条连接配置短 busy wait（50ms），避免长时间原生忙等遮蔽取消。仅对 `SQLITE_BUSY` 且事务明确回滚成功的身份操作进行有取消信号的重试；提交错误和其它错误不重试。这是数据库事务处理，不会重放远程命令。

SQLite 使用 `user_version` 管理结构版本。版本 2 保留 `devices` 中的身份与授权状态，移除重复授权镜像和操作历史表；不存在双写或双读分支。结构变更在事务中完成，不支持高版本数据库由低版本程序打开。部署升级前必须备份 Relay 数据目录；生产部署仍需明确授权。删除表不承诺安全擦除 SQLite 空闲页或旧备份中的既有数据。

已鉴权请求的转发和终态处理不写数据库，也不为每次操作创建后台持久化任务。Relay 不提供命令审计功能。后续会员用量应由独立的可信主体、资源租约和最小计量模块负责，不能从命令内容或一次转发完成推算计费与真实作业结束。此处尚未实现该计量模型。备份 Relay 数据目录不等于备份用户文件。

---

## 4. 官方实例预算（无账号）

开放 `hello_device` 会被滥用（占连接、当 C2）。v0 硬顶，超限 `quota_exceeded`：

- 每源 IP：注册/上线频率、同时 device 连接数、带宽
- 每 device：并发 inflight（与 protocol 一致）

自托管默认不启用或自行加大。数字实现时可调，**必须有开关和上限**。

---

## 5. 运维

公网只开 WSS。限制消息体。管理口 v0 用本机 loopback CLI，不要无鉴权的 0.0.0.0 HTTP。

```text
xallor-remote relay --listen 0.0.0.0:8443 --data ./var/xallor-remote
```

官方实例：`wss://api.xallor.com/remote`（探活 `GET https://api.xallor.com/remote/health`）。与 Tab API 同证书、同主机，靠路径分流。不要为 invoke 另开 HTTP。
