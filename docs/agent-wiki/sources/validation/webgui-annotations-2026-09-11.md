# 2026-09-11 WebGUI 原处批注验证

本次在已有未提交改动的工作区增量验证批注交互、原文定位和 MCP 传输。环境为 macOS 14.8.5 / arm64，Go 1.25.3、Node 24.18.0、pnpm 11.19.0；浏览器为独立配置的 Chrome 152 headless。本记录只覆盖本轮工作区，不自动推广到已安装 App 或之后的版本。

## 工程检查

- Go 全部实际源码包的 test 与 vet 通过，相关 race test 通过。
- `pnpm --filter @teamcross/web check` 通过；Web 28 项测试通过，其中 7 项新增批注回归。
- Web production build 通过，已更新 `internal/webassets/dist`；`go build -o bin/teamcross ./cmd/teamcross` 通过。
- 文档相对链接、源码入口和 `git diff --check` 已检查。

仓库已有 `bin/go-mod` 缓存导致 `go test ./...` 的包扫描报错（嵌套模块缓存位于主模块之外）。保留该缓存，改用 `go test ./cmd/... ./internal/...` 与对应 `go vet`，覆盖当前仓库的全部实际 Go 源码包。race 范围为 `internal/collab`、`internal/mcp`、`internal/sharing`、`internal/nativecodex`、`internal/service`、`internal/cliinstall`；Go 编译和模块缓存放在本次专用临时目录。

## 批注与工具回归

[annotation_test.go](../../../../internal/collab/annotation_test.go) 验证：保存当时原文和指纹、绑定协作 fork、拒绝错误会话、磁盘持久化、读取批注不调用原生模型、代码变化后产生不同指纹、UTF-16 对话范围与无效路径/行号校验，以及执行子目录的 diff 相对路径。

同机两个模拟 Core 的协作者可以在没有输入权时保存与读取批注，输入仍属于发起者；结束共享后拒绝远端写入。此用例不代表跨设备 LAN，也没有调用真实模型。

[MCP 测试](../../../../internal/mcp/server_test.go) 验证 `add_annotation` 与 `read_context kind=annotations` 保留原文、路径、old 一侧、行号和版本指纹，不转成发送模型输入的请求。

[前端测试](../../../../packages/web/src/test/annotations.test.tsx) 验证重复文字与 emoji 的精确选区、多行文件范围、保存失败与当前页草稿、更早历史自动查找、old/new 行号、Git 中文/制表符路径和指纹变化后的拒绝高亮。原有刷新回归继续通过。

## 实际浏览器

使用生产 Web 资源和专用 loopback 模拟 API，没有接入用户已有协作或原生 Codex 会话。

- 1440、1024、768 CSS 像素分别覆盖浅色与深色，共 6 种组合；代码改动和编辑弹窗各保存一张截图。自动核对视口、文档无横向溢出、弹窗边界与主题，实际查看截图。
- 真实鼠标拖选对话文字后出现批注入口，保存结果包含正确 turn/item、UTF-16 选区与 quote。
- 点击文件行可打开带原文的弹窗，焦点直接进入意见框；`⌘ + Enter` 成功保存，`Escape` 关闭后草稿仍可继续。
- 模拟 HTTP 503 保存失败，正文和原文保持；显式再次保存成功。唯一控制台错误为这次预期的 503，没有额外脚本异常。
- 文件修改后点击已保存批注，重新读取文件并显示旧片段和变化提示，没有高亮新的同号行。
- 在 diff 删除行保存批注，实际请求包含 `side=old`、旧行号 4、基准提交及 diff 指纹。

本机截图位于 `output/playwright/annotations-2026-09-11/`，作为工作区复核材料，不属于发布包。测试结束后关闭本次独立浏览器与模拟 API 服务；本次没有启动产品 Core、app-server 或真实模型客户端。

## 结论边界

以上证明 UI 交互和工具数据足以明确表达原文位置，不是模型理解质量评测。保存批注不会自动注入共享 fork；需要模型处理时仍由用户明确提出。文件指纹不同会保守地提示核对，不实现跨版本模糊重定位。完整字段与行为见 [协议](../protocol.md#批注引用) 与 [产品流程](../product-flows.md#上下文阅读)。
