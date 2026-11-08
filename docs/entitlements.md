# Entitlement / admission boundary

`internal/entitlement` 是 provider-neutral 的权益判定原语。它接收管理面提供的 tenant、principal、membership、membership version、有效期和 feature values，按精确 scope 与时间做 fail-closed decision。provider 不可用、成员停用、版本不符、过期、缺 feature 或值不足均拒绝；它不接受客户端自报租户，不生成 billing 状态，也不把拒绝转换成重试。

当前稳定 feature contract：`exec.concurrent`、`terminal.concurrent`、`relay.bytes_per_second`、`relay.monthly_egress_bytes`、`file.max_transfer_bytes`。`xallor-remote entitlement contract` 与 target `info.entitlement_contract` 只返回 feature/unit/scope 说明，不返回当前租户余额或计费状态。

边界保持分离：Relay `admission.Service` 的 per-device inflight 是传输硬顶；`relay.Quota` 的 IP hello/bytes 是滥用保护；`internal/job` 的 reserved/occupied/uncertain 是本机安全容量；frame、read/write、argv 上限是协议硬顶。它们都不是会员权益，也不能从任一计数推导订阅、月度用量或付款状态。真实 entitlement provider、usage ledger、原子租约、周期结算和跨 Relay 计量仍未实现。
