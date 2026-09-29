# Wails 主窗口增量检查（2026-09-30）

范围：内嵌现有 React 页面、WebGUI 路由转发、原生复制、数据目录隔离的 Web Storage 和 WKWebView 导航边界。主窗口基于 `42435af06523d359cc8d5be95989a00717424bfd` 加 `codex/next/desktop-main-window` 的本次修改构建；本地清单为 `dirty: true`，CI 另按 PR 提交构建。

环境：Apple Silicon、macOS 14.8.5、Go 1.27.1、Node 24.18.0、pnpm 11.19.0、Wails `v3.0.0-beta.26`。产物为 arm64/macOS 14+，ad-hoc 签名通过，未公证。完整复现入口见 [桌面开发说明](../../../../apps/desktop/README.md)。

## 通过的检查

- 根工程 `go test ./...`、`go vet ./...`，collab/mcp/sharing/nativecodex/nativeclaude/service/cliinstall 的 race test 通过。桌面 module 的 `go test -race ./internal/...` 与 `go vet ./...` 通过。
- Web check（750 条消息）、11 个测试文件/130 项测试、production build 通过；提交重建的 `internal/webassets/dist`。新增测试覆盖浏览器路径保持、桌面请求能力/取消信号、禁止外部 API URL、断线写入不重试、固定剪贴板和偏好隔离。
- 同一个合成会话 fixture 通过真实 Core HTTP/存储提供材料。真实 WKWebView 中完成来源选择、固定范围、修改标题后重新预览、创建只读空间与发布；新材料在资源库出现。
- 验证材料中的 Markdown、表格与代码高亮，点击“复制代码”后粘贴到批注输入，得到原代码；验证中文与 emoji 草稿。
- 模拟 Core 失联后点击保存，界面显示连接失败并保留草稿。恢复后读取成功，没有自动保存；显式提交才新增一条批注。P0 的换端口和接受写入后丢失响应测试继续由 coreclient 回归覆盖。
- 真实桌面窗口浅色/深色截图复核；同一 fixture 的浏览器在 1440、1024、768 CSS 像素下阅读已发布材料，768 无页面横向溢出，浏览器 console 无 error/warn。

## 结论边界

这轮使用模拟原生历史，没有真实模型 prompt，也没有启动用户的 Codex/Claude 客户端。它证明本机主窗口与既有页面的集成，不证明双机协作、真实网络、输入法候选组合、全部长内容/UTF-16 手动选区或正式安装已通过。URL Apple Event、单实例转交、菜单栏/速览、共享主题及完整退出语义仍由后续 PR 验证。外链与原生确认已有受限实现，完整真实系统浏览器/弹窗回归尚待窗口生命周期批次。

本轮测试输出位于工作目录 `bin/desktop-preview/p1-validation/` 和专用 `/private/tmp/teamcross-desktop-p1-20260930-v2/`。测试结束关闭本次 App、浏览器页和 fixture；不停止用户既有 Core。
