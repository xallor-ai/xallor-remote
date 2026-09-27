# 端到端：一次任务怎么走

SSOT：时序与「谁连谁」的产品路径。**不要从本文实现 Relay。** 转发、inflight、流、cancel、断线、背压只在 [dataplane.md](dataplane.md)。消息码见 [protocol.md](protocol.md)。落盘见 [relay.md](relay.md)。安装见 [model.md](model.md)。

一次调用里的「本机 / 目标机」是方向，不是两种安装包。四个头同级连 Runtime，见 [heads.md](heads.md)。

---

## 1. 路径

```text
Agent 或人
    → 本机头（MCP / CLI / TUI / GUI）
    → 本机 Runtime
    → Relay
    → 目标 Runtime
    → OS
    → stdout/stderr/exit 原路返回
```

两边都只出站。本机打不进目标机。

| 连接 | 证明 |
| --- | --- |
| 目标 Runtime → Relay（device） | 自己的 device_id + secret |
| 本机 Runtime → Relay（client） | **目标** device_id + **目标** grant |

---

## 2. 没有任务时

**目标机：** `xallor-remote ensure` / `start` → 若无身份则以登录用户名为 device_id 并生成 secret → 确保默认 workspace → `hello_device`（inbound=false）→ Relay 标 online。此时 **没有** 授权码。

**控制机：** `xallor-remote peer add` 或 mcp.json 覆盖被 Runtime 收编 → 本机 Runtime 对目标做 `hello_client`（可惰性：第一次 exec 再建）。

**要被控：** `xallor-remote grant issue` → 出示授权码 → inbound=true。

---

## 3. `exec` 时序

```text
头          本机 Runtime           Relay              目标 Runtime
 │  exec      │                     │                       │
 │───────────►│  invoke             │                       │
 │            │────────────────────►│  校验 grant / 在线     │
 │            │                     │  审计 accepted        │
 │            │                     │──────────────────────►│
 │            │                     │                       │ 策略 → Popen
 │            │                     │  stdout …             │
 │            │  stdout …           │◄──────────────────────│
 │  流式可见   │◄────────────────────│                       │
 │◄───────────│                     │  exit                 │
 │            │  exit               │◄──────────────────────│
 │  结束       │◄────────────────────│                       │
```

校验顺序：头参数 → Relay（grant、online、inbound、并发）→ 目标策略 → 执行。失败即停。**逐步转发、inflight、终态**见 [dataplane.md](dataplane.md)，此处不重复。

---

## 4. 改授权时序

不走 client / invoke。持 grant 的对端发 `grant_rotate` / `revoke` / `inbound_set` → Relay 拒绝。

```text
人 / CLI（被控本机）
    → IPC → 本机 Runtime
    → device 连接：grant_rotate | inbound_set | revoke
    → Relay 换哈希 / 改 inbound / 销登记并踢线
```

`rotate` 只换 grant，`device_id` 不变。`revoke` 销 Relay 登记。`reset` = 本机清空 + `revoke`。细节见 [credentials.md](credentials.md)。

---

## 5. 存什么（原则）

载荷转发，元数据落盘。细表只在 [relay.md](relay.md)。

| | 头 | Runtime | Relay |
| --- | --- | --- | --- |
| grant 明文 | 尽量不持有 | peers 里对方的；自己签发的可本地再显示 | **只存哈希** |
| secret | 无 | 只在本机 | 哈希 |
| 完整 stdout | 转给 Agent/人后可丢 | 不落库 | **不落库** |
| 审计 | 否 | 可选本地 | **必须** 元数据 |

---

## 6. 部署

默认连官方 hosted Relay。自托管：`xallor-remote relay`。公网 WSS；localhost 可用 WS。目标机连的是 Relay，不是 Cursor 所在机器。v0 hosted 无账号。见 [decisions.md](decisions.md)。
