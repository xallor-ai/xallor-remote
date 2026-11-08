# #74 安装信任根方案（分阶段，尚未完成强制安装接入）

## 已核对的事实与本轮边界

`internal/release` 的 Stage 只校验 manifest/hash，Install 随后通过 Control.Check 执行候选版本；snapshot、rollback、ActivePath 和 MCP 的 managed/vendor/PATH 选择也没有来源认证。首次运行一个恶意 CLI/npm launcher 时，它自己的检查不能保护用户，因此 bootstrap 的可信来源必须独立于待安装制品。

本轮采用请求允许的降级路径：固定长期来源政策、增加只验证不安装的 `runtime verify`、补默认拒绝测试，并扩展现有 provenance CI 对 raw binary/manifest 的覆盖。**不宣称 install/upgrade/rollback/MCP 已受强制来源保护，不据此完成 #74，也不推进 #73。** 当前主线发行工作流未包含本候选的签名步骤；GitHub Releases 查询为空，本地没有本产品的真实已签两版本夹具，不能可靠验证安装/旧版本回滚正向链路。不会用 mock、可替换的 key、重算 manifest 或 dirty=false 伪造认证。

## 长期信任根

复用 #60 的 Sigstore/GitHub provenance，避免另造 detached-signature 发行协议。信任根由独立取得并信任的 bootstrap 验证器、可信系统 GitHub CLI 及其 Sigstore 根组成；生产验证政策编译在验证器中：GitHub host `github.com`，仓库 `xallor-ai/xallor-remote`，工作流 `.github/workflows/release.yml`，OIDC issuer `https://token.actions.githubusercontent.com`，SLSA provenance v1，拒绝 self-hosted runner，固定 release tag 与完整 source commit。制品携带的公钥、manifest、checksum、以前的“验证成功”JSON 均不是信任根。

从不可信 manifest 读取 version/revision 仅用于提出待验证的 `refs/tags/v<version>`/commit 断言；必须由两份真正的来源证明（binary、原始 manifest bytes）分别确认，且两者绑定同一来源。必须拒绝 dirty、unknown revision、非版本 tag、错误 signer/workflow/ref/commit。manifest 哈希仍负责内容一致性，不代替证明。不能只签外层 zip 后要求安装器信任任意解压目录；发行 CI 同时 attests 原始二进制和 manifest，zip/tgz 仍有原有证明。

`gh` 必须来自操作者信任的系统安装。PATH/本机管理员、bootstrap 或解释器被攻击者控制属于信任根已失陷，不能由候选自检解决；验证器禁止从待验目录内运行 gh，不允许候选指定 host、根证书或 signer。自托管发行需另行审查并独立分发带其政策的可信 bootstrap，不能用候选内 policy 或运行时的随意环境变量覆盖正式政策。

## 四条路径的最终方案

| 路径 | 独立信任锚 | 强制检查应发生的位置 |
| --- | --- | --- |
| 首次安装 | 用户先独立信任 bootstrap/系统 gh，再对下载附件作外部验证；npm 本身先验证 tgz，不能靠包内脚本自证 | 提取到私有隔离区；binary + 原始 manifest 验真、绑定身份/平台/hash；完成之前不能向安装目录复制、执行 version 或停止旧服务 |
| 升级 | 已信任的正在运行安装器政策，不接受候选更新政策 | 在 Stage/Control.Check 之前认证隔离快照；校验后使用同一快照落盘，验证缓存命中也不能略过；认证当前可恢复版本后才能停止它 |
| 旧版本/故障回滚 | 当前可信安装器政策及仍有效的旧版本证明 | 不把 state.json/旧成功记录当认证；旧版本每次重新认证，检查撤销与兼容性，再停止/替换进程。验证失败保留当前进程和可恢复记录，不重试候选 |
| MCP managed/vendor/env/PATH | 用户已独立信任的 npm launcher + 同一内嵌政策；绝不执行待选 Runtime 来验证自身 | 所有候选路径在返回可执行路径前完成同一来源认证；候选存在但无证明即拒绝，不悄悄回退到其它 unsigned PATH 或 XALLOR_REMOTE_BIN |

需将 Store 的“完整性 Verify”与“来源认证”命名区分，避免当前 Verify 被误当成签名校验。未来强制接入应通过单一认证快照接口，把已认证文件句柄/私有快照传到复制与执行步骤；不能把新 `runtime verify` 的输出当永久许可证再对原始可变目录安装。

## 本轮只读验证入口

```text
xallor-remote runtime verify --bundle <bundle-dir>
xallor-remote runtime verify --bundle <bundle-dir> --attestation-bundle <provenance.sigstore.json>
```

读取有上限的 manifest 和二进制到私有临时快照，检查平台/格式/大小/hash，调用真实系统 gh 对原始 manifest 和 binary 验证来源，返回 JSON 后删除快照。不会写本机数据目录、停止 Runtime、执行候选或安装。可选 bundle 是**待验证的证明载荷**，不是自定义信任根；格式、签名、证书链或政策不符仍拒绝。GitHub CLI 缺失、服务/根获取不可用、证明缺失/坏签名均非零退出，无 checksum-only 降级。该入口与已有 Python 附件验证器都不能使尚未接入的安装器变安全。

Windows/Linux 使用同一政策和参数向量，不经 Shell 拼接；平台差异只在二进制名称/平台校验。当前用户只应把入口用于验证带新增 raw-file provenance 的真实 release；本地 dirty 构建会被拒绝。

## 开发、失效与恢复

源代码开发可继续显式构建并运行可信源码；未签名开发构建不能获得正式来源认证，不引入生产 allow-unsigned 环境变量。现有 --allow-dirty 只控制本地构建/测试，不改变验证入口。MCP 自动选择的强制认证尚待接入，必须保留此缺口声明。

本轮不缓存可永久复用的验证结果；成功 JSON 仅是当次文件/政策/时间的观察。在线证明存在不等于具备产品级撤销机制。完整接入前还需由发行方定义可信更新的撤销/禁止摘要政策、政策版本防回退、根轮换审批、旧版本支持窗口与离线可信根更新流程。新政策须由现有可信政策/独立 bootstrap 分发链授权，不能来自待安装包。不能把 GitHub tag 可改写或删除当成充分撤销保障。

离线恢复需提前保存证明与经过独立验证的信任材料，并使用仍可信的验证器。没有可用信任根、撤销状态或受支持旧版本时拒绝激活，保留原服务与 profile；不能临时把制品里的 key 设为根，也不能为恢复自动执行未认证候选。系统 gh 读取 bundle 时仍可能更新其根/服务信息，本轮不承诺完全无网络离线验证。

## #74 完成前必须补齐的验收

1. 受保护的真实 release/tag 上签发并验证 raw binary/manifest、zip/tgz；同时验证 Windows/Linux 两平台。没有真实签发权限/发布授权时，不能本地伪造代替。
2. 同一发行政策下两个实际已签版本的首次安装、升级、旧版本回滚、失败恢复；将强制认证接到 Stage、snapshot、ActivePath、activate/restore 与 MCP 全部候选路径。
3. 真实签名正确、坏签名、未知 signer、错误 workflow/ref/commit、撤销、篡改后重算 manifest、缓存命中被换包、服务不可用、认证后路径交换；拒绝发生在停止旧服务、执行候选或安装复制之前。
4. Bootstrap/npm launcher 的独立分发与验证说明，可信根轮换/吊销及离线恢复政策，MCP/原生代码共享政策的构建一致性门禁。

纯参数/拒绝路径测试和已有 fakeControl 状态机测试不证明密码学正确；报告须把这些测试与真实 gh 密码学校验、真实产品签名正向链路分开。#74 未完成前 #73 保持待处理，不以新参数重构扩大本轮范围。
