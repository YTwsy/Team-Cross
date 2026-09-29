# Wails 单实例增量检查（2026-09-30）

范围：同一用户/规范化数据目录的外壳锁、显示窗口转交、入队回执与重复请求去重。不涉及 Core 启停、邀请 URL 接收、菜单栏或生产安装替换。

环境为 Apple Silicon/macOS 14.8.5、Go 1.27.1、Wails `v3.0.0-beta.26`。本地 Preview 基于 `697784595c2fdee7a4507b117258afa3372a3984` 加单实例源码，清单记录 `dirty: true`；两个 arm64 二进制及 App 的 ad-hoc 签名检查通过，CI 从 PR 提交另行构建。复现入口是 [单实例测试](../../../../apps/desktop/internal/appinstance/instance_test.go) 和 [预览开发说明](../../../../apps/desktop/README.md)。

## 自动检查

- 桌面 module 的 `go test -race ./internal/...`、`go vet ./...` 通过；构建重建 Web 资源，既有嵌入资源没有变化。
- 请求版本、UUID、大小、URL 形状与 32 项批次上限；512 项/10 分钟回执缓存；接收队列拒绝时不登记已接收，后续可用相同 ID 重试。
- 路径别名使用同一身份，独立目录可分别取得锁；锁符号链接被拒绝，正常退出保留锁文件。
- 独立测试子进程实际接收 CFMessagePort 请求。暂停该进程令回执超时，恢复后重试同一 ID，入队计数为 1；暂停期间不能抢占锁。
- 结束该测试子进程后，新实例重新取得锁，测试目录中的保留文件不变。整个测试不调用 Core stop 或真实 Provider。
- [Swift 互通回归](../../../../apps/desktop/internal/appinstance/interop_test.go) 编译仓库现有的 Swift `AppInstance`，双向转交显示请求与重复回执均通过，不以手工复制的协议结构替代真实互通。

## 真实窗口检查

以同次构建的隔离 fixture 打开原生 Preview，另复制一份 App，通过真实路径与符号链接路径并发启动四个副本。四个副本均转交成功并退出，只有原外壳仍存活；窗口中 `单实例转交保留草稿 · aliases and receipt retries` 保持不变。

另暂停本次外壳，第二份副本保持等待；恢复原外壳后第二份成功转交并退出，同一 fixture 仍存活。该检查只操作已核对启动路径和数据目录的测试 PID。

互通检查发现 Foundation 与 Go 对 `/private/tmp`、`/private/var` 的表示不同，已令通信名称沿用 Foundation 的路径规则，锁与 Core 发现仍使用 Go 的规范化目录。修正后重建 App，再次完成四副本/别名转交和草稿检查；旧 Swift probe 的两次同 ID 请求均返回 `true`。邀请尚未接入，所以不将空 URL 批次转交通过推广为邀请兼容性通过。

最终测试 App 输出位于 `bin/desktop-preview/p2a-validation-v2/`（保留修正前的 `p2a-validation/`），数据与别名分别为 `/private/tmp/teamcross-desktop-p2a-20260930/` 和 `/private/tmp/teamcross-desktop-p2a-alias-20260930`。测试结束关闭本次外壳与 fixture。没有注册生产 scheme、停止用户 Core、发送模型 prompt 或修改真实用户会话。
