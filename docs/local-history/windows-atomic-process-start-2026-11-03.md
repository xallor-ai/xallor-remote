# Windows 原子进程监督启动

基线 3681686b359244e0f1194bdd82f81c9459882ae3（PR28），分支 codex/windows-atomic-start。

## 问题与证据

原流程在 `cmd.Start()` 后才 `AssignProcessToJobObject`。Runtime 在两步之间退出时，子进程可能已经创建但尚未进入 Job Object；这会破坏“资源释放必须有 OS 结束确认”的前提。原有 attach-failure 测试只覆盖错误处理，不能覆盖 owner 在间隙崩溃。

新增 Windows 测试让独立 owner 创建挂起 child、写出 PID、故意不 attach 后被杀；旧实现实测 child 在3秒内仍存活，测试失败，证明缺口真实存在。

## 修复

Windows file-backed stdio 使用 `PROC_THREAD_ATTRIBUTE_JOB_LIST` 在 `CreateProcess` 时绑定 Job Object，再恢复主线程；不再依赖创建后 attach。标准 Runtime exec 使用 os.Pipe 文件，因此走此路径。非 file-backed stdio 保留 os/exec 路径，避免在进程监督层复制复杂的 IO 管道语义；这一路径暂不宣称具备原子 Job 绑定，handoff 的 bounded writer 属于后续独立收敛项。

官方 Windows 文档确认 `PROC_THREAD_ATTRIBUTE_JOB_LIST` 支持 Windows 10 / Server 2016 及以上；Linux 仍使用进程组现有监督。不能把 Job Object 或进程组当作租户安全沙箱。

## 验证

- Windows 本机：`go test ./internal/process -run 'TestJob_should_reclaimChild_whenOwnerDiesBeforeAttachment|TestProcess_should_preserveOutputAndExit_afterNormalCleanup' -count=1 -v` 通过，原子启动回归通过。
- 全量 `go test ./...`、`go vet ./...`、Runtime 构建通过。
- `job_lifecycle_e2e.py` 四组、`cross_layer_e2e.py` 14项、`execution_results_e2e.py` 14项通过。
- Windows 交叉编译 `internal/process` 与 `internal/job` 测试二进制通过；CI 将提供真实 Windows 结果。

## 尚未覆盖

当前修复只覆盖 file-backed stdio 的原子启动。要让所有 handoff/自定义 writer 也进入同一路径，需要在 process 模块中统一管理额外 pipe 的创建、复制、关闭和 Wait，这应作为独立改动，不能假装已完成。作业 ledger 仍保留 uncertain，不会因 RPC 终态或 TTL 直接释放容量。
