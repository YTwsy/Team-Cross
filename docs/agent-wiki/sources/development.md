# 开发与构建

[返回 README](../../../README.md) · [使用指南](../../user-guide.md) · [Agent Wiki 索引](../wiki/index.md)

本页维护源码开发环境、启动和调试步骤，以及前端与本机插件的开发约束。产品行为由 [产品流程](product-flows.md) 维护，发布操作见 [构建与发布](releasing.md)，检查要求和已知结果分别见 [验证门槛](validation/test-gates.md) 与 [证据入口](validation/evidence-map.md)。

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

```sh
# 生成本地 CLI、App、DMG、校验文件和 tap 定义；不上传产物。
make release
make verify-release
make verify-homebrew
```

开发默认版本以 Makefile 的 `VERSION` 为准，输出位于 `dist/release/<版本>/`。指定版本时使用 `make release VERSION=<版本>`，并为两个安装验证目标传入相同的 `VERSION`。版本构建要求干净 checkout，并在重建 Web 资源后再次核对。

## 前端与本机界面

新增 Web 文案用 `t` / `tr` 并补 [英文消息表](../../../packages/web/src/en.json)；翻译必须在组件渲染或函数调用时执行，不要在模块顶层计算。`pnpm --filter @teamcross/web check` 会检查遗漏文案、占位符和模块顶层翻译。动态服务提示只按产品消息处理，不能翻译会话正文或用户内容。语言切换行为由 [产品流程](product-flows.md#界面语言) 维护，实现见 [Web 消息表](../../../packages/web/src/i18n.ts)、[Core 偏好](../../../internal/collab/ui_language.go) 与 [App 语言资源](../../../apps/macos/AppLanguage.swift)。

浅色为主要设计基准，同时支持系统、浅色和深色主题。改动阅读排版必须保留 UTF-16 原文定位；按 [浏览器门槛](validation/test-gates.md) 检查空状态、失败、长路径、小窗口、焦点和键盘，并更新嵌入资源。插件与浏览器共用源码时，两套嵌入资源均须重建。

## 本机 ChatGPT 插件

Web build 同时生成 `internal/webassets/dist` 与自包含的 `internal/mcpassets/dist/panel.html`，然后由 Go embed 打包。开发完成后运行：

成品首次接入使用设置页或 `teamcross plugin connect`；`connection-status` 查询状态，`sync` 仅同步已启用接入，`disconnect` 断开并关闭自动同步。调试时使用专门的 `CODEX_HOME` 和数据目录；以下底层包命令用于手动源，已经启用管理的源仍经统一生命周期更新。详细约束见[插件生命周期](decisions/chatgpt-local-plugin.md#包和生命周期)。

```sh
make build
./bin/teamcross plugin export
./bin/teamcross plugin install
./bin/teamcross plugin status
# 同一安装源更新自身缓存；不覆盖其他 MCP 或插件配置。
./bin/teamcross plugin upgrade
./bin/teamcross plugin remove
```

可追加 `--plugin-dir /absolute/path`、`--data-dir /absolute/path` 和 `--codex-bin /absolute/path/to/codex`。插件缓存使用包里的稳定 CLI 副本；更换源位置时先移除原来源注册，再向新的目录重新导出、安装，不直接搬动原包或把整台机器的绝对路径配置当作跨机器分发包。浏览器和插件均从 `packages/web/src/main.tsx` 构建同一套 WebGUI；插件构建只替换环境适配，内联脚本、样式和高亮资源，无需公网托管。修改前端后同时更新 `internal/webassets/dist` 与 `internal/mcpassets/dist`。职责与范围见 [本机插件决策](decisions/chatgpt-local-plugin.md)。

## 验证与发布

按修改范围从 [验证门槛](validation/test-gates.md) 选择工程检查、浏览器检查和真实客户端验证；完整命令、嵌入资源更新要求及测试环境边界由该页维护。真实模型验证使用专用会话与测试目录，不使用普通用户会话。

发布顺序、GitHub workflows、Homebrew 渠道与签名边界见 [构建与发布](releasing.md)。历史版本的验收结果不能代替本次检查；需要确认已知范围时从 [证据入口](validation/evidence-map.md) 定向读取报告。

参与开发先读 [AGENTS.md](../../../AGENTS.md)。需要跨会话接续或交接时，按 [任务记录规则](../README.md#任务记录) 留存必要信息。
