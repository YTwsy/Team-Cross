# Team Cross Desktop Preview

`Next` 的独立 Wails v3 module，精确锁定 `v3.0.0-beta.26`。根 module 的 Core/CLI 不依赖 Wails；桌面使用 Go 1.27.1+、macOS Command Line Tools 和系统 WKWebView。

当前增量内嵌现有 React 主窗口，复用协作空间、材料发布/阅读、批注、资源库和设置流程。请求通过原生端连接隔离 Core，复制通过固定的原生剪贴板接口，Web Storage 按规范化数据目录隔离。菜单栏、邀请转交与正式安装切换按迁移计划逐步实现。预览要求显式隔离目录（`--data-dir` 或 `TEAMCROSS_DATA_DIR`），拒绝默认生产目录；不启动 Core 或注册 URL scheme。显式退出会停止对应的隔离 Core，活动协作先确认。

```sh
make desktop-test
make desktop-build
```

先运行 `pnpm install --frozen-lockfile`。构建会重建 `internal/webassets/dist`，然后在 `bin/desktop-preview/<时间>/` 生成新的 `Team Cross Desktop Preview.app` 和 `desktop-build.json`，保留既有产物。包含独立 CLI helper，两个二进制使用同一版本与提交；产物为 arm64、macOS 14+，ad-hoc 签名、未公证。

完整页面可连接专用 Core，也可使用 [合成会话 fixture](../../internal/collab/library_browser_test.go)：

```sh
TEAMCROSS_LIBRARY_BROWSER_DIR=/private/tmp/teamcross-desktop-library \
  go test -count=1 -run '^TestLibraryBrowserFixture$' -v \
  -ldflags '-X teamcross/internal/buildinfo.Version=0.2.6-dev.desktop -X teamcross/internal/buildinfo.Commit=<desktop-build.json 中的完整 commit>' \
  ./internal/collab
```

从 `fixture.json` 读取 `dataDir`，传给 App 的 `--data-dir`。fixture 的原生会话是合成数据，HTTP、发布、材料存储与批注走实际 Core 实现，不调用模型；浏览器用同一清单的 `url`。创建 `offline` 文件可模拟失联，移走此文件后恢复读取；创建 `finish` 文件会关闭 fixture 和临时服务。不要对生产数据使用这些测试开关。

先选择一个新的 fixture 目录，再用构建输出中的实际路径运行：

```sh
python3 scripts/desktop-smoke-service.py /private/tmp/teamcross-desktop-fixture \
  --manifest '/absolute/output/desktop-build.json'
'/absolute/output/Team Cross Desktop Preview.app/Contents/MacOS/TeamCrossDesktop' \
  --data-dir /private/tmp/teamcross-desktop-fixture --probe --leave-core-running
```

fixture 只接受空目录或它自己创建的标记目录，不读取用户会话或调用模型。`connection.json` 使用随机凭据和 0600 权限；观察文件 `writes.jsonl` 不记录凭据。停止并重新运行同一 fixture 会改变端口和实例，页面可直接重新读取，草稿不重新挂载。在 fixture 目录创建 `drop-write-response` 文件后，保存偏好会接受写入但丢弃响应，用 `writes.jsonl` 验证没有自动重放。

关闭窗口会隐藏；Dock reopen 或 `⌘1` 重新显示。`⌘Q` 读取活动协作状态，确认后等待同一 Core 实例释放锁，再退出外壳；默认取消按钮，取消或失败保留窗口和草稿。`--leave-core-running` 仅供独立 fixture 使用，使退出只结束外壳。原生退出确认跟随 Core 的本机界面语言偏好，失联错误读取同一 settings.json。第二份外壳转交或启动失败退出不会调用 Core stop；处理退出时拒绝新的转交回执，让发送者保留原请求重试。

窗口只加载内嵌页面，点击 HTTP(S) 外链交给系统浏览器，拒绝其他导航与新 WebView；浏览器入口继续使用普通 Web 能力。

同一用户、同一规范化数据目录只有一个外壳，符号链接别名和多份 App 副本会转交给已有窗口，保留当前路由和草稿。`app.lock` 独立于 Core 锁，不随退出删除；已有外壳暂时无响应时最多等待 15 秒，不启动重复外壳。转交回执只确认入队，同一请求 ID 在 10 分钟内去重，最多缓存 512 项。当前只转交显示窗口请求；邀请 URL 接收与暂存尚未接入，因此不确认接收邀请批次。

原生转发只接受明确允许的路径/方法，核对发现文件与 Core 的实例、PID、协议、版本和提交。普通 API 不携带控制 token，重定向、HTML、未知路径和控制路由不会转发给页面；Core 不兼容或离线返回结构化错误。所有请求都使用新连接，写入不会自动重放，WebView 取消请求会关闭上游读取。

相关：[桌面边界](../../docs/agent-wiki/sources/decisions/desktop-host.md)、[迁移计划](../../docs/agent-wiki/tasks/desktop-wails-migration.md)、[验证契约](../../docs/agent-wiki/sources/validation/test-gates.md)。
