# ChatGPT 插件首次接入与 App 同步

日期：2026-10-03。开发分支：`codex/chatgpt-plugin-onboarding`，从 Meta `af811fff28279e0d0e20e84ab924aa4c7121454e` 创建。功能及构建来源提交：`7089781d20f67e7d0656657098129612ba04bbbf`。

## 已完成

首页提供设置入口；菜单栏增加“ChatGPT 插件…”；浏览器和插件共用设置页中的连接管理。首次显式安装或接管后，App 启动/重新打开时同步已启用接入。原有来源、数据绑定、无关原生配置保留；被外部移除/停用的插件不自动装回。旧缓存进程与纯 MCP/HTML 探测不能确认新版界面加载；新的安装代号在正确 Core 的 WebGUI bootstrap 成功后记录。

长期规范见[本机插件](../../sources/decisions/chatgpt-local-plugin.md)、[分发与首次体验](../../sources/distribution-and-onboarding.md)、[本机接口](../../sources/protocol.md)和[验证门槛](../../sources/validation/test-gates.md#chatgpt-本机插件)。

## 本次验证

- Go：`go test ./...`、`go vet ./...`，以及 collab/mcp/sharing/nativecodex/nativeclaude/service/cliinstall/pluginpack 的 race 检查通过。
- Web：check 通过，917 条翻译；14 个测试文件、148 项测试通过；普通 WebGUI 与 MCP Apps 嵌入资源均重新构建并提交。
- Swift：类型检查通过；构建的真实 App 通过 `verify-app-instance.py --plugin-codex-bin ...`。覆盖主实例去重、转交、崩溃恢复、原 Core 保留；未启用时无插件写入，启用后下一次 App 启动刷新过期 runtime，来源和数据绑定保留，状态等待新版界面打开。
- 实际安装的原生 CLI `0.159.0-alpha.12.1`：使用全新独立 profile 执行 `verify-plugin-connection.py`，旧手动安装接管、缓存资源匹配、旧进程不能确认新版、新进程 bootstrap 确认、兼容 upgrade/remove 命令、外部移除与显式恢复均通过。用最终 App 包内 helper 再运行一次也通过，没有启动模型回合。
- 真实浏览器：普通 WebGUI 完成安装/断开；opaque iframe 在 `connect-src 'none'` 下，经实际 stdio/Core/原生 CLI 完成同步与断开。1440/1024/768 宽度、中英文、浅深主题截图复核；新版 bootstrap 后提示清除，断开后首页设置入口恢复。业务工具没有失败；测试页有一个无关 favicon 404。
- 包校验：DMG SHA-256、只读挂载、App 签名完整性、包内 CLI 版本与源码提交一致。未执行完整 Homebrew/公开发布门槛。

原生缓存测试和浏览器宿主消息模拟均不能替代 ChatGPT 主窗口验收。本次未控制或重启用户 ChatGPT；按用户要求，主窗口安装后的整体体验留到成品阶段。未把本机测试当作两台 Mac LAN/Tailcat 验证。

## 本地产物与清理

安装包版本 `0.2.6-dev.plugin-onboarding.20261003`，arm64，最低 macOS 14，build 133。从上述干净提交构建，ad-hoc 签名，无 Developer ID、未公证。

- `dist/release/0.2.6-dev.plugin-onboarding.20261003/Team-Cross-0.2.6-dev.plugin-onboarding.20261003-arm64.dmg`
- DMG SHA-256：`f902b06f1df085bc99900714939b44e114647435ddd42dbc41f620210a78d322`
- 同目录保留 `SHA256SUMS`、`release.json`、App 和 CLI tar。
- 原生客户端证据：`output/plugin-connection-packaged/result.json`。
- App、Go、DMG 与浏览器结果：`output/plugin-onboarding-native-v2/`。
- 截图：`output/playwright/plugin-onboarding/`。

本次临时 App、app-server、stdio、Core 和独立浏览器均已关闭，测试 profile 中自动同步已关闭。现有用户插件仍为原先的 `chatgpt-local` 来源；runtime SHA-256 仍为 `b117d8414be2f932b4280d9a400359679b871772e2e3c5b0106121403c8cf434`，本次没有升级或改绑它。原 Next checkout 保留，本分支尚未推送或合并。
