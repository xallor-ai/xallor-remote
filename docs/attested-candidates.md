# 受保护发行候选证据 / Protected candidate evidence

本页记录 Issue #75 的可复核证据。它只记录已经完成的 GitHub 受保护发布工作流和原始制品验证，不把本地构建或测试夹具当作签名来源。

## 已生成的候选

| 版本 | tag commit | release workflow | 状态 | 覆盖 |
| --- | --- | --- | --- | --- |
| `v0.2.5` | `79724818584dae06073d5b29c673c375386b7bbf` | `42453251415` | success / draft | Windows/Linux raw binary、manifest、zip/tgz、SBOM、Sigstore provenance、installer source gate |
| `v0.2.6` | `8345804ca0afc218ac01f850df9a272c3899f49f` | `42454269210` | success / draft | 同一 release workflow 的第二个候选，修复 packaged-install 验证边界后重新生成 |

两次 workflow 都通过 Go、MCP、audit、文档、SBOM/provenance、raw verification 和 installer source gate。候选 release 仍是 draft，没有发布到用户或生产环境。

## 固定信任策略

- workflow：`xallor-ai/xallor-remote/.github/workflows/release.yml`
- OIDC issuer：`https://token.actions.githubusercontent.com`
- provenance predicate：`https://slsa.dev/provenance/v1`
- signer workflow、tag ref 和 source commit 必须同时匹配；可选的本地 manifest、checksum 或 proof 文件不是信任根。
- `runtime verify` 和 installer source gate 对缺少 verifier、错误 signer、错误 workflow/ref/commit、篡改后二次计算 manifest 都 fail closed。

## 验证边界

已验证：v0.2.5 的真实 raw attestation、篡改拒绝、installer 在复制/检查/停止旧 Runtime 前拒绝未验证来源；v0.2.6 的同一工作流候选和完整 release CI 也通过。

尚未宣称：两个独立签名版本的正向 upgrade→rollback 全链路、签名 MCP vendor 的正向选择、离线 trust-root rotation/revocation。它们仍由 #74 的后续验收负责，不能用 mock 或单个版本的存在代替。

## English

This page records independently reviewable evidence for Issue #75. It lists protected GitHub release workflow runs and raw artifacts only; local builds and test fixtures are not trust roots.

`v0.2.5` (commit `7972481`, workflow `42453251415`) and `v0.2.6` (commit `8345804`, workflow `42454269210`) both passed the release gates and remain draft candidates. The verifier pins the release workflow, OIDC issuer, predicate, tag ref, and source commit. Missing or mismatched evidence fails closed.

The evidence proves real attestation and installer rejection paths. It does not claim a positive two-version signed upgrade/rollback flow, signed MCP vendor selection, or offline trust-root rotation/revocation; those remain explicit follow-up boundaries for #74.
