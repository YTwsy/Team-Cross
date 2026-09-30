# Team Cross Desktop Preview

`Next` 的独立 Wails v3 module，精确锁定 `v3.0.0-beta.26`。根 module 的 Core/CLI 不依赖 Wails；桌面使用 Go 1.27.1+、macOS Command Line Tools 和系统 WKWebView。

当前增量内嵌现有 React 主窗口，复用协作空间、材料发布/阅读、批注、资源库和设置流程。请求通过原生端连接隔离 Core，复制通过固定的原生剪贴板接口，Web Storage 按规范化数据目录隔离。预览要求显式测试目录（`--data-dir` 或 `TEAMCROSS_DATA_DIR`），拒绝默认生产目录；持锁实例通过内置 helper 确保隔离 Core，`--connect-only` 可禁止启动。仅注册测试邀请 scheme `teamcross-desktop-preview://`，正式 `teamcross://`、菜单栏及安装切换继续按迁移计划实现。退出预览仍只退出外壳，不停止 Core。

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

从 `fixture.json` 读取 `dataDir`，传给 App 的 `--data-dir`。fixture 的原生会话是合成数据，HTTP、发布、材料存储与批注走实际 Core 实现，不调用模型；浏览器用同一清单的 `url`。`invitePage` 提供本机测试邀请，来自另一个独立 fixture owner，只读分享与加入使用实际 pinned-TLS 实现。`invitation-requests.jsonl` 仅记录暂存、预览和加入的路由，不记录邀请 body 或控制凭据。创建 `offline` 文件可模拟失联，移走此文件后恢复读取；创建 `finish` 文件会关闭 fixture 和临时服务。不要对生产数据使用这些测试开关。

先选择一个新的 fixture 目录，再用构建输出中的实际路径运行：

```sh
python3 scripts/desktop-smoke-service.py /private/tmp/teamcross-desktop-fixture \
  --manifest '/absolute/output/desktop-build.json'
'/absolute/output/Team Cross Desktop Preview.app/Contents/MacOS/TeamCrossDesktop' \
  --data-dir /private/tmp/teamcross-desktop-fixture --connect-only --probe
```

fixture 只接受空目录或它自己创建的标记目录，不读取用户会话或调用模型。`connection.json` 使用随机凭据和 0600 权限；观察文件 `writes.jsonl` 不记录凭据。停止并重新运行同一 fixture 会改变端口和实例，页面可直接重新读取，草稿不重新挂载。在 fixture 目录创建 `drop-write-response` 文件后，保存偏好会接受写入但丢弃响应，用 `writes.jsonl` 验证没有自动重放。

关闭窗口会隐藏；Dock reopen 或 `⌘1` 重新显示。`⌘Q` 只退出这个隔离预览。窗口只加载内嵌页面，点击 HTTP(S) 外链交给系统浏览器，拒绝其他导航与新 WebView；浏览器入口继续使用普通 Web 能力。正式 App 的停止服务与退出行为仍以现有分发契约为准。

同一用户、同一规范化数据目录只有一个外壳，符号链接别名和多份 App 副本会转交给已有窗口，保留当前路由和草稿。`app.lock` 独立于 Core 锁，不随退出删除；已有外壳暂时无响应时最多等待 15 秒，不启动重复外壳。转交回执只确认入队，同一请求 ID 在 10 分钟内去重，最多缓存 512 项。

App 事件循环先接收冷启动邀请，再争用外壳锁；第二份外壳保留原请求 ID 转交并退出自身。待转交队列仅存在内存，最多 32 个邀请/批次，10 分钟后过期。原生端将邀请暂存于 Core，只用 pending ID 打开独立确认窗口，不覆盖主窗口路由或草稿；展示预览后仍需用户明确加入。Core 重启不会重建暂存邀请或重放加入。Apple Event、独立确认窗口及安装路径的实际验收范围见迁移任务，不从构建成功推定已通过。

原生转发只接受明确允许的路径/方法，核对发现文件与 Core 的实例、PID、协议、版本和提交。普通 API 不携带控制 token，重定向、HTML、未知路径和控制路由不会转发给页面；Core 不兼容或离线返回结构化错误。所有请求都使用新连接，写入不会自动重放，WebView 取消请求会关闭上游读取。

相关：[桌面边界](../../docs/agent-wiki/sources/decisions/desktop-host.md)、[迁移计划](../../docs/agent-wiki/tasks/desktop-wails-migration.md)、[验证契约](../../docs/agent-wiki/sources/validation/test-gates.md)。
