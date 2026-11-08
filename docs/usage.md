# Privacy-preserving usage / audit boundary

`internal/activity` 的 `activity.json` 是本机 bounded metadata history：只帮助本机 UI/CLI 展示请求阶段和终态，不是防篡改审计、不可否认日志、tenant ledger 或账单来源。它不保存命令、路径、prompt、stdout、grant、account token 或支付数据；损坏时报告不可用，不伪造空历史。

`internal/usage` 定义独立 usage event：`id`、时间、幂等键、tenant/membership/device scope、operation、outcome 和 units。`completed`、`failed`、`unknown` 是明确结果；相同幂等键与相同 event 是 no-op，冲突复用拒绝。unknown/failed 事件可保留供 reconciliation，但永远不计入 completed usage 或 billable 结果。事件没有命令、路径、输出、IP、凭据或付款字段。

`xallor-remote usage contract` 与 target `info.usage_event_contract` 只返回字段和隐私规则，不读取余额、生成账单或证明审计完整性。真实 usage provider、跨 Relay 去重、租约/计费周期、不可篡改审计存储、账单和 retention policy 仍未实现；任何商业系统必须在独立管理面基于已确认终态和 tenant/membership 授权接入。
