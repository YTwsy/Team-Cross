# 2026-09-21 分享范围阅读与确认

## 版本与环境

基于 `ad72b6a`（v0.2.1 发布后的文档提交），在 `codex/feature/publication-range-reader` 独立 worktree 完成。环境为 macOS 14.8.5 arm64、Go 1.27.1、Node 24.18.0；浏览器加载当前 production build 的嵌入资源。

实现入口为 [Publisher](../../../../packages/web/src/components/Publisher.tsx)、[私有草稿阅读器](../../../../packages/web/src/components/PublicationReader.tsx) 和 [目录摘要](../../../../internal/collab/publication_read.go)。产品行为见 [分享范围与确认](../product-flows.md#分享范围与确认)。

## 工程验证

以下检查通过：

- `go test ./...`、`go vet ./...`。
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall`。
- Web `check`、`test`、`build`；7 个文件、96 项测试通过，包含 10 项发布交互测试。
- `go build -o bin/teamcross ./cmd/teamcross`，嵌入式 `internal/webassets/dist` 已更新。
- Markdown 相对路径、入口导航、旧按钮文案和 `git diff --check`。

发布交互回归覆盖紧凑目录与分页读取、导航不改范围、起止交叉收为单轮、范围外建议起点清除、确认页只读范围预览、返回保留正文与工具展开、旧边界缺失、迟到响应丢弃、读取失败重试、标题更新，以及响应丢失后的同请求发布重试。确认正文首次读取失败时，发布按钮保持禁用。

Go 回归另核对导出说明总数与逐轮计数、预览范围内统计、目录不内联正文，以及范围预览拒绝读取外部轮次。Vite build 保留主包大于 500 kB 的体积提示，未阻止构建。

## 浏览器复核

使用 [TestMaterialsBrowserFixture](../../../../internal/collab/materials_browser_test.go) 的隔离测试 Core、合成原生历史和本机 TLS。两次 fixture 均由自己的 `finish` 文件正常结束；第二次加载最终资源。未操作用户已有来源会话或已安装 App 的 Core。

实际操作包括：

1. 发起协作、选择来源后，形态卡片收为紧凑状态；目录与 Markdown 正文可见，目录定位不改变范围。
2. 选择第 2–3 轮并建议从第 3 轮读起；工具输出从折叠前缀读取到 2394 个 UTF-16 单位的完整内容。返回调整后，工具保持展开，滚动位置恢复。
3. 确认页从第 2 轮展示，目录和可见正文均不含第 1 轮的范围外文本。修改标题后用 Enter 更新预览，再发布为只有 2 轮的版本 1；在已发布材料中复核同样边界。
4. “发布新版本”弹窗重新固定来源后，保留第 2–3 轮与建议第 3 轮的设置；弹窗宽度为 1100 CSS 像素且无横向溢出。Escape 正常关闭弹窗。
5. 浅色与深色分别检查 1440 × 900、1024 × 800、768 × 900 CSS 像素；核对实际视口、目录分栏与窄窗排列、起止标记、代码块和底部动作，没有页面横向溢出。最终页面控制台未记录 warning/error。

截图保存在本次 worktree 的 `output/playwright/publication-range/`（本地忽略产物），包含六组尺寸/主题、桌面正文阅读和最终确认页。浏览器截图曾受视口缩放影响；最终使用明确像素比例并在布局更新后重新截图复核。验收后关闭测试页并恢复视口覆盖。

这次验证支持发布编辑与确认流程的判断。历史来自测试替身，未调用真实模型，也不代表两台 Mac、Tailcat、原生 TUI/Desktop、安装或发布验收。
