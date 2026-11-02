# Relay

SSOT：职责边界、落盘、官方预算、运维。**连接 / inflight / 流式 / cancel / 断线 / 背压**只在 [dataplane.md](dataplane.md)。消息与限额见 [protocol.md](protocol.md)。策略不在这里执行。授权角色见 [credentials.md](credentials.md)。

---

## 1. 做 / 不做

**Relay 是有鉴权的实时消息路由器**：device 连接登记、grant 校验、按 `exec_id` 转发、生命周期错误。不是执行服务器。

做：登记 device / client 连接；校验 grant；按 `exec_id` 转发；online/offline 与踢线；**接受 device 侧的** rotate/revoke/inbound；审计元数据；官方实例连接/注册预算。

不做：解释命令、读目标盘、**持久化** stdout/文件正文/完整命令、策略判定、P2P、远程桌面、账号登录（v0）、为 invoke 另开 HTTP API。

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
| `audit` | 见下 |

禁止持久：grant/secret 明文、完整 stdout、write 正文、**完整 command 字符串**。

设备存储由 `DeviceStore` 管理：注册在事务中创建或校验身份，禁止覆盖已登记设备的密钥；更新和删除必须校验设备密钥哈希。不存在、身份不匹配与存储故障分别处理。数据库故障返回 `relay_error`，不得确认注册成功；授权修改必须提交后才更新内存，持久化失败时关闭设备连接并保留原状态。

SQLite 使用 `user_version` 管理结构版本。版本 1 保留 `devices` 中的身份与授权状态，移除重复的 `grants` 镜像；不存在双写或双读分支。结构变更在事务中完成，不支持高版本数据库由低版本程序打开。部署升级前必须备份 Relay 数据目录；生产部署仍需明确授权。

**审计（诚实边界）：** v0 证明「哪类操作发生过」，**不**保证事故后能还原完整命令。字段：`ts, device_id, op, exec_id, decision, code, duration_ms, client_meta, args_preview, args_digest`。

- `args_preview`：最多约 64 字符（exec 的 argv0 / 相对路径前缀），脱敏
- `args_digest`：规范化参数的哈希
- `client_meta`：例如 IP 前缀、user-agent 类；**不是**用户账号

企业若要完整 command 录像，另开需求。backup 数据目录 ≠ 备份用户文件。audit 建议 ≥7 天。

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
