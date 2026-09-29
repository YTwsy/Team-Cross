# Team Cross Desktop Preview

`Next` 的独立 Wails v3 module，精确锁定 `v3.0.0-beta.26`。根 module 的 Core/CLI 不依赖 Wails；桌面使用 Go 1.27.1+、macOS Command Line Tools 和系统 WKWebView。

当前增量是隔离的请求通道预览：内嵌小型验证页面，读取界面语言并显式保存偏好，保留重连前的草稿。完整 React 主窗口、菜单栏、邀请与正式安装切换按迁移计划逐步实现。预览不会发现默认用户目录、启动 Core、注册 URL scheme 或停止服务。

```sh
make desktop-test
make desktop-build
```

每次构建在 `bin/desktop-preview/<时间>/` 生成新的 `Team Cross Desktop Preview.app` 和 `desktop-build.json`，保留既有产物。包含独立 CLI helper，两个二进制使用同一版本与提交；产物为 arm64、macOS 14+，ad-hoc 签名、未公证。

先选择一个新的 fixture 目录，再用构建输出中的实际路径运行：

```sh
python3 scripts/desktop-smoke-service.py /private/tmp/teamcross-desktop-fixture \
  --manifest '/absolute/output/desktop-build.json'
'/absolute/output/Team Cross Desktop Preview.app/Contents/MacOS/TeamCrossDesktop' \
  --data-dir /private/tmp/teamcross-desktop-fixture
```

fixture 只接受空目录或它自己创建的标记目录，不读取用户会话或调用模型。`connection.json` 使用随机凭据和 0600 权限；观察文件 `writes.jsonl` 不记录凭据。停止并重新运行同一 fixture 会改变端口和实例，页面可直接重新读取，草稿不重新挂载。在 fixture 目录创建 `drop-write-response` 文件后，保存偏好会接受写入但丢弃响应，用 `writes.jsonl` 验证没有自动重放。

关闭窗口会隐藏；Dock reopen 或 `⌘1` 重新显示。`⌘Q` 只退出这个隔离预览。正式 App 的停止服务与退出行为仍以现有分发契约为准。

原生转发只接受明确允许的路径/方法，核对发现文件与 Core 的实例、PID、协议、版本和提交。普通 API 不携带控制 token，重定向、HTML、未知路径和控制路由不会转发给页面；Core 不兼容或离线返回结构化错误。所有请求都使用新连接，写入不会自动重放，WebView 取消请求会关闭上游读取。

相关：[桌面边界](../../docs/agent-wiki/sources/decisions/desktop-host.md)、[迁移计划](../../docs/agent-wiki/tasks/desktop-wails-migration.md)、[验证契约](../../docs/agent-wiki/sources/validation/test-gates.md)。
