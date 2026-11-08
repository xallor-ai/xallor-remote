# OIDC / SSO 边界

当前 PR 只提供 provider-neutral 的本地验证契约，代码位于 `internal/oidc`。它验证一个已经由管理面取得的 compact JWT 和已经独立配置的 JWKS key set：issuer 必须精确匹配 HTTPS URL，audience 必须命中配置值，只接受带 `kid` 的 `RS256`，并检查 `sub`、`exp`、`nbf`、`iat`。未知算法、未知 key、坏签名、坏 claim、过期或尚未生效的 token 一律 `ErrUnauthorized`。

`KeySet.Replace` 是原子 key rotation 边界；Runtime 不联网发现 issuer，不抓取 JWKS，不接受 token 中的 `tenant_id`、`principal_id`、role 或 membership 作为本地授权。验证成功只产生 `{issuer,subject,audience,expires_at}`，管理面必须把 `issuer + subject` 映射到本地 principal/membership，再由现有 account/access/Relay 事务生成版本化身份与 device scope。

issuer discovery、JWKS HTTPS 获取与缓存、浏览器 redirect/callback、PKCE、logout、SSO session、管理员 API 和真实 IdP 集成仍未实现。CLI 目前继续使用 Relay 运维签发的本地 account credential；MCP/GUI 不自行实现 OIDC，也不能把 provider token 直接当作 grant 或 device authorization。没有完成本地映射和成员状态复验时，不能建立 Relay client 连接或访问设备。

租户边界仍由本地 `Tenant`、`Principal`、`Membership`、`Credential`、设备归属和 Relay 每次调用的 access 验证决定。外部 subject 不是 tenant ID，设备显示 ID、grant hash、IP 和 Agent 名称也不构成 OIDC 身份。成员停用、凭据撤销、版本变化和设备转移继续使旧连接失效；OIDC key rotation 也不会自动改变本地 membership。
