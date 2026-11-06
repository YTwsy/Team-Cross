# 无模型轮次的顺流感知关联

2026-11-05，`Meta` / `d440c57fa82f4202d701395e90b13753a5eab5ae` 加本任务未提交改动。共享工作区还包含其他任务的改动，以下结果针对当次整体工作树，不表示基线提交已通过，也未生成发布版本。

## 实现范围

- 独立 WebGUI 和插件的“参与会话”页可明确选择本机 Codex／Claude 会话并开启顺流感知。只读核对来源，不创建、恢复或向原生会话发送输入。
- 插件能核对宿主身份时，可直接为当前会话开启；使用 app-only 写入路由，不依赖 `ui/message`。身份只来自 MCP 传输，无轮次 Codex 身份和 ChatGPT 不透明关联键可用；模型工具的来源证明未放宽。界面桥协议升为 2，旧驻留 MCP 需重新加载。
- 复用已有身份关联与空间目标，保留自定义名称及已验证主动接收能力；并发或重复开启不新增目标、不推进首次基线。顺流开关与协作请求接收状态分别显示。
- 每位协作者在自己的 Core 加入空间、选择自己的会话；新增界面说明和[使用步骤](../../../user-guide.md#让自己的会话接收空间更新)。开启只保存偏好，后续仍需可用的 MCP 工具交互或可信 Hook。

## 工程验证

在单台 Mac 上执行，无真实模型调用。Go 缓存使用隔离的 `/private/tmp/teamcross-inflow-ui-go-cache`。

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 全量通过 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/shadow ./internal/collab ./internal/mcp ./internal/mcpevents ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall ./internal/pluginpack` | 全部通过 |
| Web `bun run check` | TypeScript 与 1428 条翻译检查通过 |
| Web `bun run test` | 30 文件、273 项通过 |
| Web 最后局部修改后的相关回归 | 3 文件、27 项通过 |
| Web `bun run build` | WebGUI、插件两套嵌入资源已更新 |
| 最后新增接收器保留回归及协议检查 | 相关 Go race、嵌入资源与桥协议检查通过 |
| 本地 CLI 构建 | `bin/teamcross` 构建通过 |
| 文档与差异检查 | 14 个改动文档的 545 个本地文件链接存在，相关来源可从 Wiki 索引进入；`git diff --check` 通过 |

[Core 回归](../../../../internal/collab/inflow_connections_test.go)覆盖无轮次关联、精确目录核对、Claude 目录路径、并发复用、基线、持久化、既有接收器和名称保留、移除后明确重连、本机 HTTP 权限，以及三个 Core 经 loopback TLS 加入同一空间后的成员隔离与撤销。[MCP 回归](../../../../internal/mcp/inflow_connections_test.go)覆盖传输身份、正文和查询伪造拒绝、app-only 路由边界以及模型工具仍要求来源证明。[交互回归](../../../../packages/web/src/test/inflow-connections.test.tsx)覆盖页面读取不自动写入、切换 Provider 清空选择、无 `ui/message` 的当前会话操作、缺少宿主身份回退、结果不明不自动重发和协作者配置说明。

原始日志位于本机 `/private/tmp/teamcross-inflow-ui-{go-test,go-vet,race,web-test,web-check,web-final,build,final-race,assets-final}.log`，属于临时证据，未另行保存到外部位置。

## 真实浏览器复核

使用[浏览器夹具](../../../../internal/collab/inflow_connections_browser_test.go)运行真实 Core、存储和 production 嵌入资源；原生会话目录与插件宿主身份为合成。夹具故意不提供向当前会话发消息的宿主能力。

复现入口：设置全新的 `TEAMCROSS_INFLOW_CONNECTION_BROWSER_DIR` 后，执行 `go test ./internal/collab -run '^TestInFlowConnectionsBrowserFixture$' -v -count=1 -timeout=26m`；从该目录的 `fixture.json` 读取 WebGUI 和插件宿主 URL。检查结束创建同目录的 `finish` 文件，夹具关闭自身服务。

- 独立 WebGUI：选择准确会话、明确开启，参与会话出现一条记录且顺流已开启，协作请求接收仍未连接。
- 插件：无 `ui/message` 能力仍可直接开启当前会话；再次从本机列表选择同一会话，目标仍只有一条。
- 界面：1440、1024、768 宽度，中文浅色和英文深色；选择弹窗、协作者说明和开关状态可用。发现弹窗继承过大最小高度后已收紧，再次截图复核；最终英文深色布局无横向溢出。
- 设置页的原生插件状态探测在合成运行时返回错误，未作为真实安装验收；新入口本身完成成功。

截图仅本地保存于 `output/acceptance/2026-11-05-inflow-connections/screenshots/`，未外部留存或提交 Git。`local-select-dark-en-1024.png` 为修正后的选择弹窗；`plugin-enabled-dark-en-768.png` 与 `plugin-enabled-dark-en-1024.png` 为最终窄屏/中屏。较早的 `local-select-light-1440.png` 记录修正前弹窗高度。

两次夹具均通过并退出；最终日志为 `/private/tmp/teamcross-inflow-ui-browser-final.log`。专用浏览器会话 `tc-inflow-20261105` 已关闭，最终服务监听端口已释放。没有关闭用户既有客户端或 Core。

## 证据边界

本次验证新增关联与配置路径，不新增真实 Codex Desktop、ChatGPT 插件宿主、Claude Hook、两台 Mac、跨 NAT 或 Tailcat 验收；未替换用户安装或重启用户服务。既有真实 MCP／Hook 投递证据仍按[顺流感知证据入口](../../sources/validation/evidence-map.md#顺流感知)中各报告的版本与环境解释。

## 后续提交与本机替换

同日用户明确要求一起提交原有删除“专用会话”改动，并替换本机版本。两部分代码、测试、文档和嵌入资源提交到 `Meta`：`a28e9cb6a12982edea42eda9d1f55fc869f89c0a`。删除范围为旧协调角色、初始化接手流程、角色控制路由及界面，普通接收会话和请求投递保留；提交前顺带清除了插件说明中残留的角色返回描述。

从该干净提交构建本地版本 `0.4.1-dev.20261105`，build `234`，arm64、macOS 14+，ad-hoc 签名、未公证。构建命令为 `python3 scripts/build-release.py --version 0.4.1-dev.20261105`；Web 重建后工作树仍干净。产物位于本地 `dist/release/0.4.1-dev.20261105/`，没有创建公开 Release 或更新 Homebrew 配方。

`python3 scripts/verify-release.py dist/release/0.4.1-dev.20261105` 通过校验文件、DMG 临时安装、图标、签名完整性、CLI/App 一致、命令入口安装/移除、Core 生命周期与真实菜单栏多副本检查。该脚本的可选插件启动同步分支未运行，不把 `false` 标记写成通过。构建与验证日志为 `/private/tmp/teamcross-inflow-install-build.log`、`/private/tmp/teamcross-inflow-install-verify.log`，仅本地临时保存。

已通过同卷原子交换替换 `/Applications/Team Cross.app`，旧 `0.4.1` App 保存在 `.local-backups/2026-11-05-inflow-a28e9cb/Team Cross.app`；精确核对旧菜单栏 PID 后仅重启此外壳，Core 经自身升级流程切换。替换后 App helper、`/opt/homebrew/bin/teamcross` 和运行中 Core 均报告上述版本及提交，仍使用原 `Team Cross Next` 数据目录。新菜单栏进程正常运行，验证产生的临时 App/Core 已由脚本清理。

已启用的本机插件自动同步至 `0.4.1-dev.20261105`，保持原来源和 Core 绑定，实际状态为 `reload_required`。没有终止个人 Codex／ChatGPT 宿主；旧驻留 MCP 仍需用户保存草稿后退出并重开宿主，本次没有新版真实宿主面板加载回执。以上本机替换是用户后续授权的步骤，与前述开发验证未修改安装的范围分别记录。
