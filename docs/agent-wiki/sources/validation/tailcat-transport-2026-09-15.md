# Tailcat 显式连接方式实现验证 · 2026-09-15

## 范围与版本

本轮验证 `codex/feature/tailcat-transport` 工作区中的 Tailcat 首版实现，起点为 `42adb81428669e2e0d48d688b58c537bc93f987c`。验证时改动尚未提交，不是发布版本、已安装 App 或远端分支。

首版在创建和重新邀请时由用户明确选择 `lan` 或实验性的 `tailcat`，不自动探测、回退或切换。两种传输复用同一个 `tcx3` 邀请协议、TLS 1.3/SPKI pin、一次性加入、成员凭据与输入协调；Tailcat 只向共享网关提供虚拟 TCP 443。

环境为 Apple Silicon、macOS 14.8.5（23J423）、Go 1.27.1、Node 24.18.0、pnpm 11.19.0；依赖固定为 Tailcat `v0.6.0`。Go 模块和发布构建均使用任务专用 `/private/tmp` 缓存。

## 工程与并发门槛

最终串行运行并通过：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing \
  ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
CGO_ENABLED=0 go build -o bin/teamcross ./cmd/teamcross
git diff --check
```

Web 共 50 项测试，生产资源已重建到 `internal/webassets/dist`。Tailcat 回归覆盖邀请候选互斥、地址和精确库版本校验、回调 listener 关闭、显式选择、默认 LAN 与无效传输拒绝。连接层只对 LAN 的多个具体候选做只读重连；Tailcat 保留邀请中的单一逻辑地址和同一个 Client，由底层处理网络变化，避免一次状态探测失败关闭仍在使用该网络栈的 WebSocket。写入失败在两种传输上都不自动重发。

一次并行冷编译为 test、vet 和 race 分别创建了大型 Tailcat/Tailscale/gVisor 缓存，使临时卷空间耗尽并出现 `no space left on device`；这是验证环境容量失败，不是用例失败。停止进程后只删除本轮新建的三套失败缓存（约 1.9 GB），改用一套共享缓存串行重跑，以上门槛全部通过；验证结束后也删除了该共享 Go 编译缓存、模块缓存和 clang 缓存，只保留 38 MB 临时发布验证件与仓库内浏览器截图，未删除仓库缓存或用户数据。

## Tailcat 同机实网

在允许访问 Tailcat DERP 并初始化 macOS 网络监视的主机环境运行：

```sh
TEAMCROSS_TEST_TAILCAT=1 \
  go test ./internal/sharing ./internal/collab \
  -run '^TestLiveTailcat(Membership|Collaboration)$' \
  -v -count=1 -timeout=100s
```

结果：

- `TestLiveTailcatMembership` 通过，约 4.5 秒；覆盖临时 Server/Client、HTTPS、TLS pin、一次性加入、关闭旧 Client 后以成员凭据重建连接。
- `TestLiveTailcatCollaboration` 通过，约 4.0 秒；覆盖显式 Tailcat 分享、远端状态、输入交接和本机直接客户端 WebSocket 桥接。

两项使用同一台 Mac 上的隔离 A/B Core 和模拟原生运行时。较早的诊断运行曾由 Tailcat 日志显示该次同机路径升级为 `via=direct`；最终实现关闭默认 Tailcat/Tailscale 诊断日志，避免把节点、网络和密钥相关信息写入普通日志。测试本身不把路径文字作为断言。

因此本轮只证明当前网络上的同机数据面闭环，不证明两台物理 Mac、不同网络/NAT、受限 UDP、睡眠切网恢复或强制持续 DERP 中继。公共 DERP 的长期可用性、限流和性能也不属于本轮保证。

## WebGUI 与浏览器

真实 Chrome 复核使用当前嵌入式 production bundle 和隔离 Core。创建页在 1440、1024 和 768 宽度检查局域网/Tailcat 显式二选一，加入页检查 Tailcat 预览，详情页检查邀请选择、准备中取消及深色主题。768 宽度没有横向溢出；最终 1024 × 900 复跑确认 Tailcat radio 实际选中，`scrollWidth` 等于 `clientWidth`，控制台为 0 error、0 warning。

截图保存在忽略目录 `output/playwright/tailcat-transport-2026-09-15/`，包括：

- `create-transport-1440-viewport.png`
- `create-tailcat-selected-1440.png`
- `create-tailcat-768.png`
- `join-tailcat-preview-1024.png`
- `detail-invite-tailcat-1024.png`
- `detail-invite-tailcat-1024-dark.png`

页面数据使用浏览器路由 fixture；浏览器证据验证生产资源、布局、文案和交互状态，不证明真实模型或两台 Mac 网络。

## arm64 发布构建链

现有发布脚本以 `0.1.4-tailcat-dev`、`CGO_ENABLED=0` 从该脏工作区生成临时 arm64 CLI、App 和 DMG，并由 `scripts/verify-release.py` 完整通过：校验和、只读 DMG 挂载安装、App 图标、签名完整性、CLI/App 版本一致性、单实例、CLI 安装/移除、用户已有同名命令保护及兼容 Core 复用均为 true。

与未包含 Tailcat、代码内容和当前起点只差 App 图标提交的既有 `0.1.4` 产物比较，stripped CLI 从 8,717,536 字节增至 20,245,408 字节（增加 11,527,872 字节，约 132%），压缩 tar 从 3,483,287 字节增至 7,807,835 字节（增加 4,324,548 字节，约 124%）。DMG 从 5,331,603 字节增至 10,080,323 字节，但该差值同时包含之后的 App 图标变化，不能全部归因于 Tailcat。该体积成本来自 Tailcat/Tailscale/gVisor 依赖，应在合并或发布前作为明确取舍复核。

临时证据位于 `/private/tmp/teamcross-tailcat-release-check.O35R45/artifacts`：

- `teamcross-0.1.4-tailcat-dev-darwin-arm64.tar.gz`：SHA-256 `2f42661e8234751f22f8baada7541bd38424310c83046d11706c6a4cba37481c`
- `Team-Cross-0.1.4-tailcat-dev-arm64.dmg`：SHA-256 `87ce4da856a9dc4a5a664fff85e4b1c2147b8c892e806426c46108e0518ed612`

该验证件只使用 ad-hoc 签名，`developerIDSigned=false`、`notarized=false`、`publicInstallation=false`，没有安装、上传、推送、打 tag 或创建 Release；其 manifest 的 commit 仍是工作区起点，实际 Tailcat 改动由 `dirty=true` 表示，不能当作可追溯正式产物。

## 未完成的产品验收

合并或发布前仍应使用同版本构建完成：

1. 两台 Mac 同一 LAN 的显式 `lan` 回归。
2. 两台 Mac 不同网络的 Tailcat 加入、状态、直接客户端、输入交接、断线重连、离开和结束。
3. 在受控条件下阻断点对点 UDP，依据明确路径诊断证明业务流量持续走 DERP。
4. 决定是否接受已测得的 CLI 体积增长，并继续评估冷启动资源、公共 DERP 失败文案与新增依赖的第三方许可证清单；正式发布另做 Developer ID 签名、公证和安装验收。

本轮启动的浏览器、隔离 Core、DMG 挂载和发布验证辅助进程已关闭；本轮实网测试没有修改系统 TUN、路由或 Tailnet 配置。
