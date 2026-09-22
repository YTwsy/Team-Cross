# 2026-09-23 分享范围吸顶操作区

## 版本与范围

基于 `930ea1b`，环境为 macOS 14.8.5 arm64、Go 1.27.1、Node 24.18.0。实现入口为 [Publisher](../../../../packages/web/src/components/Publisher.tsx)、[PublicationReader](../../../../packages/web/src/components/PublicationReader.tsx) 和 [样式](../../../../packages/web/src/styles.css)，产品行为见 [分享范围与确认](../product-flows.md#分享范围与确认)。

范围摘要、快捷范围、阅读提示、专注阅读及下一步操作合并到顶部吸顶区域，删除底部重复范围与按钮。导出说明保留为吸顶区内的可展开计数，展开后解释附件或不支持内容的缺失，并区分说明条目、轮数和工具折叠。目录及轮次跳转使用操作区实际高度避让。

## 工程检查

- `go test ./...`、`go vet ./...` 通过。
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall` 通过。
- Web `check`、`test`、`build` 通过，共 8 个文件、113 项测试，包含 11 项发布交互测试。
- 更新嵌入式资源并完成 `go build -o bin/teamcross ./cmd/teamcross`。

新增回归覆盖共享操作区内唯一的下一步按钮、编辑与确认之间阅读工具切换、返回时保留专注状态、导出说明折叠展开及随选择范围更新。原有范围隔离、确认正文加载后才能发布、标题更新、失败重试与请求标识测试继续通过。Vite 保留主包超过 500 kB 的现有提示。

## 浏览器与收尾

使用 [材料浏览器 fixture](../../../../internal/collab/materials_browser_test.go) 加载当前 production build；Core 与存储真实，来源为包含图片导出说明的合成历史。

- `1440×900` 浅色下，滚动后操作区顶部保持 12px。展开说明后操作区高度自动更新，第二轮目录跳转最终停在操作区下方约 16px，目录同步避让。
- 选择第 2–3 轮并展开完整工具输出后，范围摘要与预览按钮仍在吸顶区。
- 实际 CSS 视口 `1024×900`、`768×900`、`390×900` 的浅色和深色下，未发现页面横向溢出，按钮均位于操作区内；手机宽度的吸顶位置为 61px，避让顶部导航。查看了宽屏与窄屏截图。

按用户要求停止追加视觉测试，未继续做发布弹窗的本轮视觉复核；确认与返回流程由交互测试覆盖。恢复临时视口及主题模拟，关闭测试页面，并通过专用 `finish` 文件结束 fixture，测试最终通过且测试 HTTP 端口已释放。用户服务未重启。本次没有调用真实模型或进行跨设备验证。
