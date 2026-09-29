# 开发与构建

[返回 README](../../../README.md) · [使用指南](../../user-guide.md) · [Agent Wiki 索引](../wiki/index.md)

## 环境与启动

从源码开发需要 Go 1.27.1+、Node 24+ 和 pnpm，版本要求见 [go.mod](../../../go.mod) 与 [package.json](../../../package.json)。构建 App 还需要 macOS Command Line Tools / Swift。以下命令均在仓库根目录运行。

```sh
pnpm install
make build
./bin/teamcross serve
```

`make build` 先生成嵌入式 Web 资源，再构建 Go 二进制，具体目标见 [Makefile](../../../Makefile)。前端调试使用两个终端分别运行：

```sh
pnpm --filter @teamcross/web dev
./bin/teamcross serve --foreground --dev-web http://127.0.0.1:5173
```

## 本地安装包

`Next` 的 Wails 桌面增量通过独立 module 构建：`make desktop-test` 运行原生请求边界的 race test，`make desktop-build` 生成隔离 Preview。它尚未替换下述正式 App 构建；环境、fixture 与产物位置见 [桌面开发说明](../../../apps/desktop/README.md)。

```sh
# 生成本地 CLI、App、DMG、校验文件和 tap 定义；不上传产物。
make release
make verify-release
make verify-homebrew
```

开发默认版本以 Makefile 的 `VERSION` 为准，输出位于 `dist/release/<版本>/`。指定版本时使用 `make release VERSION=<版本>`，并为两个安装验证目标传入相同的 `VERSION`。版本构建要求干净 checkout，并在重建 Web 资源后再次核对。

## 验证与发布

按修改范围从 [验证门槛](validation/test-gates.md) 选择工程检查、浏览器检查和真实客户端验证；完整命令、嵌入资源更新要求及测试环境边界由该页维护。真实模型验证使用专用会话与测试目录，不使用普通用户会话。

发布顺序、GitHub workflows、Homebrew 渠道与签名边界见 [分发与首次体验](distribution-and-onboarding.md)。历史版本的验收结果不能代替本次检查；本页不列举历史验收报告。

参与开发先读 [AGENTS.md](../../../AGENTS.md)。需要跨会话接续或交接时，按 [任务记录规则](../README.md#任务记录) 留存必要信息。
