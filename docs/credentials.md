# 设备 ID 与授权码

SSOT：三套秘密与授权语义。协议角色见 [protocol.md](protocol.md)。

---

## 1. 用户看见什么

`xallor-remote start` 之后：

```text
Relay:     wss://api.xallor.com/remote
Device ID: andy
Workspace: C:\Users\<you>\XallorRemote\workspace
入站:      关（还没有授权码）
```

设备 ID = 本机登录用户名（去掉域前缀，如 `ANDY\andy` → `andy`）。本机与对方设备一律用这个名字识别与配对；同一 Relay 上用户名须唯一。

需要给别人控：`xallor-remote grant issue`（**必须在这台被控机本机**）

```text
授权码: xr_grant_8f3c…    ← 给对方 peer add / mcp 覆盖
```

短配对码 v0 不做。反向再签发一张，互不替代。

---

## 2. 三套秘密

| 名字 | 谁拿着 | 寿命 | 用途 |
| --- | --- | --- | --- |
| 设备 ID | 可见 | 直到撤机 | 路由 |
| 授权码 grant | 控制端 | 直到轮换/吊销 | 允许指挥这台 |
| 设备密钥 secret | 仅本机 Runtime | 直到重装/撤机 | 证明上线的是这台 |

`peers.json` 里存的是**对方**的 grant，等同密钥库：笔记本失窃 = 能控所有 peer。权限仅当前用户。

---

## 3. Grant 是 bearer capability（写死）

v0：**一台设备一个有效 grant**；轮换即作废旧的。多控制端（几台 Cursor、CLI）**共享这一份**。

```text
知道 device_id + 出示 grant 明文
  = 可以对这台设备 invoke（在 inbound 开、策略通过的前提下）
```

- **不绑定**控制端 `device_id`、账号、Cursor 安装。
- 审计 **不能** 声称「这是某个人的 Cursor」；最多 `client_meta`（连接侧元数据）。
- 泄漏 grant = 执行权交给持有者。补救是目标机本机 `rotate`（换码、ID 不变）。撤机才 `revoke`。
- v1 若要「换电脑不用换码」，另加 `consumer_type=account`，**不改** v0 这条主路径。

禁止把 v0 grant 理解成 session（短 TTL、绑一台控制端）。它就是可复制的能力凭证。

---

## 4. 谁能改授权（写死）

| 动作 | 谁 | 怎么证明 |
| --- | --- | --- |
| `grant issue` / `show` / `rotate` | **被控机本机** CLI/TUI/GUI | 本机 IPC → Runtime |
| `inbound on/off` | 同上 | 同上 |
| `revoke` | 同上 | Runtime 用 **device 连接 + device_secret** 通知 Relay 销登记 |
| `reset` | 同上 | 本机清空数据目录，并走 `revoke` |
| 持 grant 的 MCP / 对端 Runtime | **不能** | client 连接发 revoke/rotate → 协议拒绝 |

签发、轮换、吊销、开入站：**不走 MCP tool**，也不走对端的 `hello_client`。

`rotate` ≠ `revoke`：前者作废旧 grant；后者注销这台设备在 Relay 上的身份。

---

## 5. 其它规则

授权码：高熵、前缀 `xr_grant_`；Relay 只存 **SHA-256** 哈希；日志不得打全文；禁止放进 URL。

设备 ID：登录用户名；同一 Relay 唯一；不是密钥。

多 peer：`xallor-remote peer add`；多台时 exec 必带 device_id。不得用 A 的 grant 打 B。

---

## 6. 威胁

| 威胁 | 缓解 |
| --- | --- |
| 只装 MCP 就被控 | 默认无 grant、入站关 |
| grant 进 git / 聊天 | 日志打码；MCP 不签发 |
| 多控制端共享同一码 | 产品如此；泄漏则 rotate |
| `peers.json` 失窃 | 文件权限；尽快在**各目标机** rotate |
| 持 grant 者想拆掉授权 | client 不能 revoke（§4） |
| 冒充设备上线 | secret 不出盒 |
| 伪造 ID 打别人 | grant 必须匹配该 ID |
| **Relay 能看见飞行中的命令和 stdout** | 产品事实：v0 不 E2E；只保证不落盘 + TLS；自托管可换 |
| 模型乱开入站 | 无此类 MCP tool |
| 官方 Relay 开放注册 | 每 IP / 每 secret 连接与注册预算，见 [relay.md](relay.md) |
