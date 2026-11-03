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
| 本机 Runtime → Relay（client） | **目标** device_id + 共享码或账号凭据；设备授权规则见 [credentials.md](credentials.md) 与 [platform.md](platform.md) |

共享码仅用于未认领设备；已认领设备使用账号模式，并按归属或分享检查访问权。账号配置失效不能退回共享码访问。

---

## 2. 没有任务时

**目标机：** `xallor-remote ensure` / `start` → 加载或创建设备身份 → 准备 workspace → `hello_device` 证明自身身份 → 提交本机设置快照 → 收到 Relay 确认后报告在线。新设备生成 9 位数字 device_id + secret，尚无授权码且入站关闭；已有设备重连保留本机设置。确认过程见 [protocol.md](protocol.md)。

**控制机：** `xallor-remote peer add` 或 mcp.json 覆盖被 Runtime 收编 → 本机 Runtime 按已配置身份对目标建立并确认 client 连接（可惰性：第一次 exec 再建）。凭据选择见 [credentials.md](credentials.md)。

**要被控：** 未认领设备走本机 `grant issue` 的共享入口；已认领设备走账号归属或分享授权。两条路径都要求入站开启，管理入口见 [relay.md](relay.md)。

---

## 3. `exec` 时序

```text
头          本机 Runtime           Relay              目标 Runtime
 │  exec      │                     │                       │
 │───────────►│  invoke             │                       │
 │            │────────────────────►│  校验身份/权限/在线     │
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

校验顺序：头参数 → Relay（身份、设备访问权、online、inbound、传输准入）→ 目标策略与执行授权 → 执行。失败即停。**逐步转发、inflight、终态**见 [dataplane.md](dataplane.md)，此处不重复。

---

## 4. 改授权时序

不走 client / invoke。持 grant 的对端发 `device.settings` / `revoke` → Relay 拒绝。

```text
人 / CLI（被控本机）
    → IPC → 本机 Runtime
    → device 连接：device.settings | revoke
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

默认连官方 hosted Relay。自托管：`xallor-remote relay`。公网 WSS；localhost 可用 WS。目标机连的是 Relay，不是 Cursor 所在机器。身份授权基础与后续托管登录/订阅的边界见 [platform.md](platform.md) 和 [decisions.md](decisions.md)。
