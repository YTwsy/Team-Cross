# 开发与验证

[返回 README](../README.md) · [使用指南](user-guide.md) · [Agent Wiki 索引](agent-wiki/wiki/index.md)

以下命令均在仓库根目录运行。

从源码开发需要 Go 1.27.1+、Node 24+、pnpm；构建 App 还需要 macOS Command Line Tools / Swift。

```sh
pnpm install
make build
./bin/teamcross serve
# 构建本地 CLI、App、DMG、校验文件和 tap 定义；不上传产物
make release
make verify-release
make verify-homebrew
```

本地开发构建默认 `0.2.1-dev`，输出位于 `dist/release/0.2.1-dev/`。版本构建使用 `make release VERSION=0.2.1`，要求干净 checkout，并在重建 Web 资源后再次核对。使用相同 `VERSION` 运行两个安装验证目标。构建不上传产物。

GitHub 对指向 `main` 的 PR 和 `main` push 运行 Go、race、Web 与 Homebrew 定义工程门槛。`Unsigned macOS release` workflow 可以手动构建、验证和保存一个不发布的 `X.Y.Z` 或 `X.Y.Z-rc.N` arm64 产物；推送可从 `origin/main` 到达的同版本 annotated tag 时，它才会生成 provenance 并创建 GitHub Release。RC 标记为 Pre-release，正式版本标记为 Latest；在取得 Developer ID 前，两者都明确使用 ad-hoc 签名且未经 Apple 公证。Release 创建后，独立 Homebrew workflow 重新下载公开资产、验证 checksum 与 attestation、隔离安装对应定义，再使用只覆盖 tap 仓库的短期 GitHub App token 创建 PR；tap CI 通过、自动合并及公共安装 smoke 完成后才更新 Release 中的 Homebrew 状态。Tailcat 公网 smoke 和两台 Mac 验收仍不在托管发布流水线中自动运行。完整参数、证据边界和发布顺序见 [分发与首次体验](agent-wiki/sources/distribution-and-onboarding.md)。

```sh
# 两个终端：前端 HMR 与 Go 服务
pnpm --filter @teamcross/web dev
./bin/teamcross serve --foreground --dev-web http://127.0.0.1:5173

# 工程门槛
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

修改 Web 后必须更新 `internal/webassets/dist` 并重新构建 Go 二进制。真实模型测试默认跳过，显式使用独立目录启动；全部固定为 `gpt-5.6-luna`：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

2026-09-10 的安装、首次体验、TUI 与未完成项目见 [2026-09-10 验收记录](agent-wiki/sources/validation/onboarding-macos-2026-09-10.md)；早期 Desktop 证据见 [2026-09-09 原生协作记录](agent-wiki/sources/validation/native-collaboration-2026-09-09.md)，不自动视为后续版本通过。后续修改按 [验证门槛](agent-wiki/sources/validation/test-gates.md) 选择检查。原生 Desktop 指定 WebSocket 入口属于当前本机版本的实验接口，不等同于公开稳定的远程产品合同。

工程知识按 `sources/` 与 `wiki/` 分层维护。首次进入仓库先读 [AGENTS.md](../AGENTS.md) 和 [Agent Wiki 索引](agent-wiki/wiki/index.md)，完整导航见 [文档入口](README.md)。各日期、版本的结果集中在 [验收导航](agent-wiki/wiki/concepts/validation-gates.md)，不把旧结果推广为当前版本通过。

直接阅读：[架构](agent-wiki/sources/architecture.md) · [接口](agent-wiki/sources/protocol.md) · [产品流程](agent-wiki/sources/product-flows.md)。
