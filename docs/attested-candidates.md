# 受保护发行候选证据 / Protected candidate evidence

本页记录 Issue #75 的可复核证据。它只记录已经完成的 GitHub 受保护发布工作流和原始制品验证，不把本地构建或测试夹具当作签名来源。

## 当前集成状态：PR #108

用户已明确授权候选提交、PR 和 CI/评审通过后的集成。非 GUI 候选已从 main
`be9a32c4c9713b2c38b1d54664b1183ef3bb2931` 建立独立分支并提交
[PR #108](https://github.com/xallor-ai/xallor-remote/pull/108)。原 PR97 工作树保留。
PR #108 的最终提交 `669bd9af03eb731bc0f0cf5940ff7b5dec568847` 已通过完整 CI
运行 `44495435321`；未合并、未创建 `v0.3.0-rc.1` / `v0.3.0-rc.2`
tag，未生成新的真实签名 A/B 候选。提交授权已不再是阻塞项。

当前门槛依次为：最终提交 CI 和独立评审通过、集成候选 A、建立独立版本 B、
通过受保护 tag 生成真实签名制品，再运行下述双平台签名生命周期。
`release` 环境的人工审核与生产部署保持独立，不以代码提交授权代替。
具体 CI 缺口和首次失败记录由 [非 GUI 台账](superpowers/plans/2026-11-17-non-gui-todo.md) 维护。

## 集成前就绪性核对：2026-11-19

仓库仍只有旧 draft 候选，最新签名 workflow 仍是 v0.2.6 的 42454269210。
当前 Runtime 要求 owner protocol 1；旧候选对应源码的 Executable 没有该字段，
不能作为当前安装器的运行基线。完整 A→B→A 和 B 启动失败恢复 A，至少需要两个
新的、支持当前 owner 协议的真实签名候选（各含 Windows/Linux）。manifest 本身
不含 owner 字段不能直接判定能力；实际已认证映像的执行探测才是运行准入门槛。

默认登录账号 WangKangAandy 对仓库有 push 权限但无 admin/maintain；受保护 v* 规则
向它返回 current_user_can_bypass=never。规则返回值未包含 bypass_actors，**不能当作空名单**：
[GitHub API](https://docs.github.com/en/rest/repos/rules#get-a-repository-ruleset)
只向有规则写权限的调用者显示该字段。release 环境仍要求 xallor-agent 人工审核，
禁止管理员绕过；指定发布者的权限已在下文另行核对。本轮未改规则、
创建 tag、提交或部署。dirty/detached 工作树不是干净发行来源，AGENTS 的明确提交授权
要求仍适用。

07:20 后续只读核对：本机已有 `xallor-agent` keyring 登录。通过该既有账号的独立请求，
确认其为仓库 admin 且对上述 ruleset `current_user_can_bypass=always`；可见 bypass actor
正是该账号。默认 gh 登录未切换，未修改权限或规则。因此指定发布者凭据/权限并非当前
缺口，不要求用户重配。该 bypass 同时覆盖创建/更新/删除，不能宣称发布者无法修改 tag；
验收仍固定完整 commit、tag/ref 和制品摘要，人工发布批准保持独立。

当时工作树 HEAD 是未合并 PR #97 的两次 GUI 提交；GitHub main 为
`be9a32c4c9713b2c38b1d54664b1183ef3bb2931`。已用独立临时 Git index 准备 main 基线
非 GUI 补丁：不包含 `apps/gui` 差异，代码内容与已评审工作树一致，原 index/HEAD 保留。
这只是可审阅 tree/patch，不是 commit 或发行。版本元数据已准备为 `0.3.0-rc.1`；
第二候选计划为 `0.3.0-rc.2`，只调整三个版本文件并另建独立提交。仓库要求的明确
提交授权当时未取得（现已授权），两个 tag 都未创建。预发布准备曾暴露 Go 测试只认正式版号、MCP
测试硬编码旧 tag 的假设；修正测试以匹配现有生产版本契约后通过，未放宽生产验签。

双平台签名生命周期已新增本地工作流接线，尚未提交、远程运行或生成新签名候选。
Linux/Windows startup-fault 的 worker、候选映像和原事务恢复观察均已通过受控原生验收；
这不等于真实签名 A/B 验收。下述独立工作流是下一次干净候选的验收入口。

### 双候选验收工作流

`.github/workflows/signed-lifecycle.yml` 通过 `workflow_dispatch` 接收 `first_tag`、
`first_revision`、`second_tag`、`second_revision`。两个 tag 与完整 40 位 commit 必须各不相同，
tag 版本必须匹配各自制品。它须从官方仓库的受保护 B tag 发起，且触发 SHA 必须等于
`second_revision`；错误上下文显式失败，不通过跳过整个 job 形成绿色结果。checkout 固定同一 SHA。

Windows/Linux 各自下载 A/B 完整候选，固定 release workflow、OIDC issuer、tag、source/signer
commit 在线验证 index 和八个制品，核对 zip/npm/vendor 一致性。原始 binary/manifest 经过
白名单解压和独立验签后，才以已认证 B 为 installer 执行六个原生场景。只读 observer 从 B
源码构建，汇总保留其摘要、候选身份、run ID 和 attempt；observer 摘要本身不是签名来源。

六场景为首次安装、缺原始 baseline manifest 拒绝、升级、回滚、重新计算 hash 的篡改拒绝、
B 启动失败恢复 A。成功报告必须对应四次 A/B/A/A owner 启动、六阶段 ready/映像绑定、原生
退出与完整清理；恢复还必须包含同一 admitted/pending/final 事务、实际候选选择及连续恢复
effects。缺证据或超时均不能计通过；失败时保留报告/日志，不声称已清理不确定资源。

该工作流只有 contents/attestations read 权限，不生成签名、不创建或发布 release、不部署。
A/B draft 先由既有 release 工作流分别生成，再调用独立验收，避免首个候选必须依赖第二个
候选的循环。验收成功仍不能代替 `release` 环境人工批准或正式推广批准。

[GitHub 要求手动触发工作流已存在于默认分支](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch)。
接线必须先经评审提交/集成，并包含在 B tag 中，才能实际 dispatch；本地文件存在不是远程 CI
已运行。两个兼容候选和真实双平台 signed cycle 仍是开放验收项。

### npm 安装入口：接线与实际验收边界

该工作流的 zip/npm/vendor 一致性检查验证归档内容，不代表 `npm install` 后的入口已运行。
`tests/packaged_install.py` 当前只检查 disposable-prefix 安装与文件布局；
`tests/mcp_signed_vendor_e2e.py` 使用当前本地 wrapper 配真实已签 vendor，未证明 release tgz
内 wrapper 的行为。新增 `tests/signed_package_e2e.py` 先认证 tgz 和原始 native bundle，
预检归档路径/链接/重复项/大小/Windows 别名，再使用独立 prefix/cache/config 标准安装。
安装阶段移除 GitHub token，绑定独立 install-profile 和 loopback Relay；脚本触及该 profile
即保留失败现场。安装后及验收后，包内原始成员逐一与归档摘要一致；依赖 lock 单独记录，
不把依赖解析视为继承应用 tgz 的签名。

npm 由从 B 源码构建的 `package-supervisor` 调用现有 `internal/process` 监督：Windows Job /
Linux process group，要求真实 root 退出、未超时与监督边界结算。Linux 刻意 setsid/setpgid
逃逸不在此边界，不能称为 sandbox 或所有任意后台后代都被收回。删除目录前再次检查
install-profile；不确定资源保留，不能以 npm 主进程退出或目录一度不存在替代结算。

实际执行包内 `dist/runtime-bin.js` / `dist/index.js`：CLI version 后、MCP initialize 后先
确认没有 owner/安装 state，再由第一次 MCP 工具调用冷启动 Runtime。CLI ensure 必须复用
同一 owner；CLI stop 确认旧进程退出，再由包内 CLI ensure 显式启动新 owner，原 MCP 重新
连接。MCP 保持一次 bootstrap 的现有限制。两次 owner 的 ready/映像/退出、Node head 退出
和 workspace 清理都纳入报告；这里不声称经过全局 npm command shim。

该入口已接入独立 signed lifecycle 工作流的 B 阶段，与原始 binary 六场景分别验收。
Windows/Linux 受控 package-head mechanics 已通过，两个平台监督器都确认了普通后代由
存活到 parent 正常退出后被收回。真实旧 v0.2.6 的签名、标准 npm 安装与安装字节核对通过，
但 CLI 缺 owner protocol 1，整体仍失败。新监督器复测中，旧 CLI 对未运行 Runtime 的 stop
返回非零，失败目录保留；只读核查未发现对应活进程，不能因此改写原失败/清理报告。
两个新候选的真正正向安装包验收和远程 CI 仍未完成。

本轮新报告校验曾接受删除进程身份/恢复细节但保留 success flags 的报告；七个反例先失败，
补齐绑定后通过。Windows `signed-ci-recovery-windows-20261119.json` 和独立 Linux 容器
`recovery-cross-platform-linux-20261119-051737/report.json` 再次通过受控原生故障恢复并清理，
新的恢复汇总校验也接受这两份实际记录。这里只验证机制和接线，不证明真实签名生命周期。

本轮已修复签名脚本的旧成功报告残留：候选输入读取前写 incomplete，异常写 failed；
准备阶段新目录登记物理身份，执行开始后不得由外层错误处理猜测清理。缺失/损坏
manifest 的两项原版反例失败，修后五项报告/准备清理测试通过，已接入 PR 与 release
工作流。独立复核确认该 P2 闭合；这不是正向签名 install/upgrade/rollback 验收。

签名证明构建来源，draft 是待审候选，正式推广批准是另外的门槛。当前签名发生在
bundles job，早于 draft 的 release 环境审核；不能把 provenance 当人工发布批准。
`release.yml` 的 workflow_dispatch 在普通分支上会被 job 条件跳过，不是一次分支 dry-run 构建。

本轮补齐原生观察链路：Windows 用只读进程句柄与原始映像 file mapping 核验，而非
只信自报路径/hash；Linux保留sealed memfd/pidfd。新增只读 `runtime-observer` 工具通过
既有runtime.status IPC核对内核peer和ready身份，再与安装状态及持有的进程引用绑定。
CLI runtime status 的 running 本身不含launch/activation，未为测试强加产品字段。
升级/回滚比较独立引用、旧进程退出和新launch，允许合法数值PID复用；最终先确认退出、
关闭引用再删除目录。缺manifest负向检查只能复用此前已绑定的同一活引用，不算新的ready成功。

Windows和专用Linux容器中的真实Runtime接线各五组通过，含篡改launch拒绝且不修复
磁盘、缺manifest仅复用原引用、恢复后同一活引用、最终内核退出及清理。来源验证仍为
受控fixture，不是当前signed A/B cycle。Windows原生映像正/反例、引用管理和报告回归
通过；Linux原生sealed-image/pidfd及Go IPC helper测试通过。全仓Go95868退出0，37包通过
（新增helper实际执行、36未变化包缓存）。完整边界和报告见
`dist/verification/runtime-observer-summary-20261119.md`。两个真实新签名候选仍是后续前提。

Linux故障观察器现通过lifecycle IPC核对原operation/intent/namespace和worker内核peer，
持有Python pidfd后再次查询同peer，再核验基线A的真实映像、seals和runtime-transaction入口。
仅关联pending原事务的B manifest/raw摘要、pending activation/launch及候选实际PPID；
不声称事后独立回溯原A→worker历史父链。恢复由同事务failed/settled、B启动quiescent_failure、
A restore start/commit/ready连续confirmed，以及实际新A进程/身份/配置/文件来证明。
SQLITE_BUSY仅作为旧A退出后、数据库解锁前的原因佐证，不再匹配旧stderr恢复短语。
不确定时只停止自有CLI head、不杀worker/candidate制造通过，并保留原事务证据。

最终受控Linux报告为 `non-gui-release/recovery-observer-linux-20261119-040220/report.json`：
installer331、worker411、B946且父进程411；B/worker内核退出、同事务恢复确认，profile和容器
清理完成，输入摘要一致。该夹具特意用追加非执行overlay生成第二种映像字节并由受控verifier
接纳，**不是真实签名A/B**；真正signed脚本仍只使用外部传入且验证的两个候选。全仓Go40654
退出0（37包、36缓存）；Windows原观察链路复跑通过。详情见
`dist/verification/recovery-observer-summary-20261119.md`。未生成新tag、未提交、未部署。

随后补齐Windows等价观察：Toolhelp只筛选候选，接受前仍要求持有进程句柄、原始映像
mapping/摘要、实际parent PID及精确启动参数。第一轮漏采同轮退出的旧A之后日志起点，
已记录失败；原事务后来settled/failed，恢复出的A另行核验并IPC停止，原失败未改绿，
测试目录保留。补充discovery前采样后通过。另已独立复现普通文件读取会阻碍Windows
原子替换（WinError5），因此候选选择改从同事务actual before/observed读取，绝不用
Expected意图字段，也不轮询活动state.json。这不是首轮漏采的根因。

最终Windows `runtime-recovery-windows-final-20261119.json` 与Linux
`non-gui-release/recovery-cross-platform-linux-20261119-045158/report.json` 均通过，profile/
进程清理，Linux容器删除、输入hash稳定。源代码仅改测试侧Python和CI；上一轮Go通过记录
不描述成本轮重跑。Windows当前相关Python检查30通过2平台skip，release/docs16通过；
Linux32检查通过（6 Windows-only skip），真实恢复及普通observer bridge通过。CI已对两平台
启用此机制检查，但未有新的远程CI或真实签名候选结果。详情见
`dist/verification/recovery-cross-platform-summary-20261119.md`。

## 历史复核：2026-11-18 下午

GitHub 受保护 tag、`release` required reviewer 和 `v*` tag 部署策略已再次读回。
现有 draft `v0.2.6` 的发行索引及 8 个制品在线来源/hash 校验通过，保存的 bundle
和 trusted root 离线验签也通过；在线逐项验证约耗时 15 分钟，具体延迟原因尚未定位。
这些结果只覆盖旧候选，不代表当前未提交工作树的签名发行。

本轮当前 MCP 测试为 74 passed / 2 skipped，Go CLI 构建成功；实际 signed-vendor
正向启动却在 180 秒后超时，生命周期脚本在首次 `runtime verify` 来源验证阶段失败，
未进入安装/升级/回滚。临时 vendor 目录和对应进程已清理，生命周期报告
`results=[]`、`cleanup_errors=[]`。不能以单测或较早成功覆盖这次端测失败。
第 4 项仅对旧 draft 通过，第 5 项仍开放；没有发布、部署或关闭 #74/#75。
完整机器记录和日志索引见本机 `dist/verification/config-45-live-summary-20261118.md`。

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

2026-11-11 补充复核：使用 v0.2.6 的发行证明 bundle 和同 SHA `8345804ca0afc218ac01f850df9a272c3899f49f` 的 Windows/Linux raw binary、原始 manifest，在 Windows 主机以真实系统 `gh attestation verify` 分别检查正确身份、错误 tag ref、错误 source commit，共 12 项符合预期。拒绝诊断明确指出 SourceRepositoryRef 或 SourceRepositoryDigest 不符，未把网络失败计作拒绝证据。该检查不执行候选，不证明 Linux 原生安装或双版本升级回滚；本地机器记录为 `non-gui-release/raw-policy-026/results.json`，复现脚本为 `verify_raw_policy.py`。

2026-11-11 Windows 本机进一步通过 `tests/mcp_signed_vendor_e2e.py` 验证当前本地 MCP launcher 与真实 v0.2.6 vendor 的实际启动：`version --json` 返回的路径、hash、版本和提交均与候选相符；篡改 manifest、保留真实 manifest 但替换 binary、替换 binary 后重算 manifest hash 三种情况均拒绝。无证明的二进制使用校准过的执行标记检测，拒绝时未执行；独立包目录清理完成，未启动日常 Runtime。该结果不是对已签 npm tgz 内 launcher 的认证；当前源码与已签 binary 是明确分开的输入。

2026-11-11 Linux 原生补测：在已授权主机的专用限资源容器内，以核对官方 SHA-256 的 Node 22.23.4 和 GitHub CLI 2.96.0 运行同一脚本，正常 version 启动及三种篡改拒绝均通过。负向 marker fixture 从当前源码交叉构建，并在 Linux 上先校准再使用。由于未复制 GitHub 凭据，可信测试工具目录中的适配脚本仅给真实 `gh` 追加官方原始 `--bundle`，不替换验证结果或改动固定来源政策；这证明 bundle 模式的原生路径，不代表在线证明获取或完全离线信任根更新。临时包及容器均已清理，未修改宿主机或业务容器。本地机器报告为 `non-gui-release/linux-native-vendor.json`。

后续 tag release 已配置同一测试在真实 attestation 后运行并单独保存机器报告，在线取证配置尚待新的实际 CI 运行验证。尚未宣称：两个独立签名版本的正向 upgrade→rollback 全链路、完整已签 npm 安装链及离线 trust-root rotation/revocation。它们仍由 #74 的后续验收负责，不能用 mock 或单个版本的存在代替。

## 2026-11-18 配置后复核（非 GUI）

07:34 再次复核：GitHub API 仍返回 active 的 protected-tag ruleset、`release`
required reviewer `xallor-agent` 和 `v*` tag 部署策略。重新下载完整 v0.2.6 候选后，
发行索引及 8 个制品的在线来源/hash 校验，以及使用本地 bundle 和新获取 trusted root
的离线证明校验均通过。最初使用的历史缓存缺少三份 SBOM 和一份 checksum，第一次
检查失败；补齐来自 GitHub 的完整候选后才得到通过结论。

本轮 MCP 为 74 passed / 2 skipped，真实 signed-vendor 五场景通过且清理完成。
六个 Go 包的无缓存复核中，`releasetrust` 的
`TestVerify_shouldSettleDescendant_whenRootExitsWithInheritedStdout` 首次因
`descendant did not start` 失败，其余五包通过；失败测试单独复跑通过，但原因尚未确认，
不得以复跑覆盖首次失败，也不得称本轮 Go 检查全部通过。没有修改产品代码或放宽超时。
因此台账第 4 项仅对旧 draft 已验证，第 5 项仍为部分验证，完整当前版本签名生命周期
尚未闭合。详细记录见 `dist/verification/config-recheck-summary-20261118.md`。

随后定位到测试夹具自身的期限冲突：根进程在 5 秒后主动退出，而测试允许后代
10 秒内就绪。新增 6 秒就绪延迟用例在修复前失败，记录 `spawned` PID 和
`root_timed_out=true`；修复后根进程等待测试释放，15 秒总上下文、10 秒就绪窗口、
3 秒释放后结算断言均未放宽。夹具的孤儿 watchdog 设在总上下文之外，避免后代自然
退出被误计为清理成功；测试还要求释放前存活、清理结果确定，并在失败退出前等待
验证器结算。Windows 两场景、相关 vet 及 Linux 原生 releasetrust 包 15 项通过；
Linux 报告为 `non-gui-release/proof-settlement-linux-20261118-073758/report.json`，
源文件摘要一致、容器已移除、无清理错误。此复现证明夹具缺陷，不证明首次偶发失败
一定由该时序触发，也不构成签名生命周期验收。

修复后 Windows `go test -p 2 ./...` 返回 0；releasetrust 本轮执行 17.280 秒，
未受影响的包使用 Go 测试缓存。完整日志为
`dist/verification/proof-settlement-full-go-20261118.log`，首次失败和受控复现失败记录均保留。

GitHub API 已确认当前发布治理配置可见：`protected-release-tags` ruleset 为
active，`release` environment 启用 `xallor-agent` required reviewer，且部署分支策略要求
自定义匹配策略。该观察不是一次新的发布授权，也没有创建、更新或发布 tag/release。

针对现有 draft `v0.2.6`（commit
`8345804ca0afc218ac01f850df9a272c3899f49f`，release workflow `42454269210`），在隔离临时目录
完成了两层来源矩阵：

- `scripts/verify_release.py` 在线校验通过，索引内 8 个制品逐一通过真实 GitHub
  attestation、固定 signer/ref/revision 和 SHA-256；使用下载的
  `provenance.sigstore.json` bundle 对同 8 个制品离线复核也全部通过。
- 当前工作树的 `internal/releasetrust`、`internal/release`、`internal/runtimeowner`、
  `internal/runtimehead`、`internal/runtimeprocess`、`internal/launcherresource` Go 测试全部通过；
  Windows 进程启动观察测试把首次扫描造成的观察窗口从 3 秒放宽到 10 秒，仍受 15 秒总上下文限制。
  MCP 测试为 74 passed / 2 platform skips；真实 v0.2.6 npm tgz 的 disposable-prefix 安装通过；
  `tests/mcp_signed_vendor_e2e.py` 的签名启动、跨平台 manifest、篡改 manifest、未签字二进制和重算
  hash 五个场景全部通过，清理完成。

边界必须保留：`v0.2.6` 仍是 draft，尚未有基于当前脏工作树的全新 protected-tag run。
历史 PR check `42454200555` 的 Go/MCP 作业成功，但两个 release-lifecycle 作业因当时
`tests/packaged_install.py` 使用未定义的 `env` 失败；当前工作树已包含该修复并用真实 tgz
重跑通过，但还没有新的远程 CI 结果。因此 #74/#75 仍不能关闭，也不能宣称 clean candidate
或生产发布完成。

## 2026-11-12 双签版本本机安装验收

从已有候选缓存重新验证 v0.2.5/v0.2.6 各 15 项实际来源检查，包含 index、Windows/Linux zip、原始 manifest/binary、npm tgz 的正向验证，以及错误 workflow/ref/commit、篡改、缺证明和重算 manifest hash 的拒绝；不凭缓存文件名认定可信。

`tests/signed_upgrade_e2e.py` 在 Windows 独立 profile 中使用当前本地构建安装器和上述两个真实签名版本，已通过首次安装 v0.2.5、升级 v0.2.6、回滚 v0.2.5、拒绝重算 hash 的篡改升级四项。每阶段核对实际 PID/path/hash/build 与安装记录；身份只比较摘要、不输出秘密，配置和 workspace 哨兵文件保持不变。Relay URL 固定为本机 loopback 未监听端口，不使用生产服务。

第二轮机器报告 `non-gui-release/signed-upgrade-windows.json` 为 passed=true、cleanup_errors=[]，并记录本地安装器 SHA-256。首轮四项操作同样通过但清理因测试脚本未关闭 SQLite 连接失败，原报告保存在 `signed-upgrade-windows-attempt1-cleanup-failure.json`；已修复测试脚本并完整重跑。首轮残留目录递归删除被执行策略拒绝，因此不宣称历次运行都已清理。

Linux 随后在专用限资源容器内通过同一四项安装循环，测试 profile 和容器清理完成。安装器为当前本地 Linux 构建，候选为真实 v0.2.5/v0.2.6；验证器使用核对官方 SHA-256 的实际 GitHub CLI 2.96.0，加两份官方证明的 bundle，不复制 GitHub 登录凭据。记录为 `non-gui-release/linux-signed-cycle-e8a14365465e.json`，内含安装器摘要、实际运行版本、PID/path/hash 与清理结果。这是 bundle 证明模式，不代表在线证明获取链路。

Linux 后续还通过 `--startup-fault` 场景：测试只对独立 profile 的 identity.db 持有短期排他事务，观察与预期安装路径、start 参数和 profile 环境一致的新版本 PID，确认其因 SQLITE_BUSY 退出后释放锁。安装器返回失败并恢复旧签名版本的新进程，pending 清空，身份指纹、配置和文件不变；未修改候选字节、模拟验证器或增加产品测试开关。报告 `non-gui-release/linux-signed-cycle-e20a65aee11f.json` 含 5 项通过、故障 PID/原因和完整清理结果。该场景有观察延迟造成旧版本也暂时受锁影响的失败可能，遇此情况测试会失败，不能把重试结果覆盖成同一次成功。

这些结果证明 Windows/Linux 正常双版本安装/升级/回滚、激活前拒绝，以及 Linux 的上述一种真实启动故障恢复；不覆盖所有故障类型、Windows 同等故障注入或无来源证明旧版本迁移。**截至该段记录的 2026-11-12 历史快照**，GitHub rulesets/environments 查询均为空，main 经典保护接口返回404；这不覆盖 2026-11-18 已读回的当前 `protected-release-tags` / `release` 配置。真实签名存在不等于发布治理已完整验收。#74/#75 保持开放。

证据时效：上述 Linux 4 项是此前候选的历史结果，不覆盖后来新增的配套 manifest 校验。加入异平台已签 manifest 拒绝场景后的 Windows 5 项已通过；对应 Linux 重跑在工具下载阶段超时，未进入产品测试，失败报告保存在 `non-gui-release/linux-native-vendor-pairing-setup-failure.json`。`linux-native-vendor.json` 是滚动的最近结果，不能当作历史成功的固定链接；后续运行另按容器 run ID 保存独立报告。

## English

This page records independently reviewable evidence for Issue #75. It lists GitHub release workflow runs and raw artifacts; local builds and test fixtures are not trust roots. On 2026-11-18 the repository API showed an active `protected-release-tags` ruleset and a `release` environment requiring the `xallor-agent` reviewer; this confirms configuration visibility, not a new protected-tag publication.

`v0.2.5` (commit `7972481`, workflow `42453251415`) and `v0.2.6` (commit `8345804`, workflow `42454269210`) both passed the release gates and remain draft candidates. The verifier pins the release workflow, OIDC issuer, predicate, tag ref, and source commit. Missing or mismatched evidence fails closed.

The evidence proves real attestation and installer rejection paths. The local Windows launcher passed native signed-vendor version execution and tamper-refusal cases. An earlier Linux candidate passed four cases in a dedicated container using the actual GitHub verifier with an official proof bundle, without copied credentials; its latest pairing revision and online retrieval path remain unverified. These tests combine the local launcher under review with authentic v0.2.6 binary inputs; they do not certify the launcher inside a published npm archive. Windows and Linux additionally passed real signed 0.2.5→0.2.6→0.2.5 installation cycles and pre-activation tamper rejection. Linux used the actual verifier with both official proof bundles. Post-activation failure recovery, complete npm installation and offline trust-root rotation/revocation remain follow-up boundaries for #74.

On 2026-11-11, a Windows-hosted verifier also checked the v0.2.6 Windows/Linux raw binary and manifest subjects against the authentic release bundle. All 12 positive/wrong-ref/wrong-commit expectations were met, with explicit identity-mismatch diagnostics. No candidate was executed; these are provenance checks, not native Linux execution or upgrade/rollback acceptance.

On 2026-11-18, the same v0.2.6 draft passed online and bundle-based offline attestation/hash verification for all eight indexed artifacts. The current checkout's release-trust/owner Go suites, MCP tests, disposable npm installation, and five-case signed-vendor matrix also passed. The historical PR check `42454200555` still contains two release-lifecycle failures caused by an undefined `env` in the then-current `packaged_install.py`; the current worktree fixes and reruns that case locally, but no clean protected-tag run has yet been produced. Issues #74 and #75 therefore remain open.

The earlier statement that rulesets and environments were empty is a historical 2026-11-12 observation, not the current configuration: the 2026-11-18 API readback found the active protected-tag ruleset and release environment. A new protected-tag candidate is still required.
