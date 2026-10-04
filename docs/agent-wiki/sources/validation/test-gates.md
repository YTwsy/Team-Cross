# 验证门槛

本页维护当前可复用的验证方法、通过标准与结论边界；它不报告哪些检查已经执行通过。判断实际验收范围先读 [证据入口](evidence-map.md)，再核对报告与当前相关实现。具体任务选择适用检查，完整结果按 [任务记录规则](../../README.md#任务记录) 留存；历史报告不能证明后续提交自动通过。

## 按变更选择检查

| 变更范围 | 应完成的检查 |
| --- | --- |
| 仅 Markdown、导航或文档迁移 | 相对链接、代码路径、旧引用、索引可达性与 `git diff --check`；不需要启动模型或测试客户端 |
| 产品代码 | 下列 Go 与 Web 工程门槛，按修改范围补相关回归 |
| Git 预览、目录创建或恢复 | 原目录分支/HEAD/暂存区/文件不变；worktree 不复制四类未提交内容；子目录映射、新会话 ID 与来源关联 |
| 输入协调、共享或进程生命周期 | 相关 race test，以及直接连接/MCP 共用输入者、重复请求、审批、断线、结束访问和恢复 |
| 实现下一阶段只读分享与多人空间 | 除工程门槛外，完成 [首版契约](../decisions/collaboration-spaces.md#实施顺序与首版完成标准) 的三成员、逐人撤销、范围隔离、发布版本和多人接力检查；旧双人结果不能替代 |
| 空间请求、简报、专用角色或独立接收会话 | 三成员跨 Core 目标/身份/回执、固定引用和撤销、请求去重、关闭/重启不重放、简报 CAS、唯一角色并发、接手确认/暂停/更换及独立原生调用；见下方空间工作台门槛 |
| LAN / Tailcat 传输或邀请格式 | 邀请互斥字段、TLS pin、多人共用链接加入、成员重连、取消与资源关闭；Tailcat 另跑显式联网测试，并把同机、两台 Mac 直连和强制 DERP 分开报告 |
| 原生协议、客户端启动或账户路由 | 除协议测试外，使用专门会话实测对应 TUI/Desktop；分别验证登录、历史、输入、审批和 A 上执行 |
| 模型与推理设置 | 来源继承、客户端选择、失败不更新显示、通知、恢复与写入归属；真实调用仅使用 Luna |
| WebGUI | 类型检查（含中英消息完整性检查）、交互测试、production build，更新两套嵌入式资源；界面是主要改动时进行实际浏览器和截图复核。非界面任务附带的小型状态/文案调整由相关交互测试覆盖，默认省略浏览器与截图 |
| 菜单栏 App 启动、转交或退出 | Swift 检查和构建；在图形登录会话执行真实双副本并发、数据目录别名、邀请转交与确认重试、无响应恢复、独立目录、异常退出后复用 Core、显式退出停止服务 |

## 工程命令

在仓库根目录运行：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/mcpevents ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall ./internal/pluginpack
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
go build -o bin/teamcross ./cmd/teamcross
```

修改 `packages/web/src/` 后，提交重新生成的 `internal/webassets/dist/` 和 `internal/mcpassets/dist/`。纯文档修改无需重建这些产物。若沙箱阻止 Go 缓存写入，可将 `GOCACHE`、`GOMODCACHE` 指向 `/private/tmp` 下的任务专用目录。

旧 Node Agent Bridge 已移除，不再运行或恢复旧 Bridge 的 check/test/build 门槛。

## 现有自动化入口

Core 升级相关变更另运行 `scripts/verify-core-upgrade.py`：指定旧、新及下一构建的二进制、真实 Codex CLI 与全新输出目录；三个构建使用不同 commit、相同版本号以覆盖开发包。检查旧插件先启动、并发升级、仍存活的旧 MCP 再次启动、新 App 替换后未同步插件、磁盘构建比较、原地址/启动参数与数据保留、源安装缺失和未完成投递延后。隔离 Core/stdio 的行为证据不等于真实模型或桌面面板验收；脚本退出关闭自身进程。

| 文件 | 重点覆盖 |
| --- | --- |
| [CI workflow](../../../../.github/workflows/ci.yml) | main 推送与面向 main/Meta 的 PR：Go test/vet、相关 race test、Web check/test/build、嵌入资源一致性和 Darwin arm64 CLI 交叉编译；不运行真实模型或 Tailcat 联网测试 |
| [Unsigned release workflow](../../../../.github/workflows/release-unsigned.yml) | 手动 arm64 正式版/RC 构建，以及 annotated tag 对应的 Latest/Pre-release；校验 `origin/main` 来源、完整 release/Homebrew 验证、清单、SHA-256 和 artifact provenance；不代表 Developer ID 签名、公证或两台 Mac 网络验收 |
| [Homebrew publish workflow](../../../../.github/workflows/homebrew-publish.yml) | 只处理已存在的公开 Release；重新验证 tag/main 来源、公开 checksum、manifest、attestation 与对应渠道隔离安装，通过最小权限 GitHub App 创建 tap PR，等待受保护合并及公共 tap push smoke 后回写 Release；不直接修改 tap `main` |
| [workspace_test.go](../../../../internal/workspace/workspace_test.go) | 两种目录模式、Git 现场保留、文件路径范围 |
| [collab_test.go](../../../../internal/collab/collab_test.go) | 创建恢复不发送 prompt、输入归属、去重、审批、原生与工具并行、访问范围、本机登录路由 |
| [lifecycle_test.go](../../../../internal/collab/lifecycle_test.go) / [membership_test.go](../../../../internal/sharing/membership_test.go) | 链接重置与关闭、持久成员、丢失响应、主动离开、空闲释放、并发请求和旧连接隔离 |
| [invitation_test.go](../../../../internal/sharing/invitation_test.go) / [tailcat_test.go](../../../../internal/sharing/tailcat_test.go) | `tcx3` 传输字段互斥、Tailcat 地址与精确版本检查、回调 listener 关闭并发 |
| [runtime_mode_test.go](../../../../internal/collab/runtime_mode_test.go) | 创建模式与预览绑定、不可切换、原生权限、hook 确认归属、同 ID 恢复和结束访问 |
| [model_test.go](../../../../internal/collab/model_test.go) | 模型继承、设置更新、拒绝请求、恢复与通知 |
| [process_test.go](../../../../internal/nativecodex/process_test.go) | 客户端配置与启动不强制模型 |
| [discovery_test.go](../../../../internal/nativecodex/discovery_test.go)、[client_discovery_test.go](../../../../internal/collab/client_discovery_test.go)、[native-client-settings.test.tsx](../../../../packages/web/src/test/native-client-settings.test.tsx) | 新旧应用布局、同应用迁移、自定义路径与环境覆盖、Shell PATH 缓存刷新、保存值与检测结果分离及失效恢复；真实版本、TUI/Desktop 按原生客户端门槛另行验证 |
| [server_test.go](../../../../internal/mcp/server_test.go) | STDIO 读取不发送输入，保留输入文本与请求 ID |
| [agent_read_test.go](../../../../internal/collab/agent_read_test.go) / [agent_annotations_test.go](../../../../internal/collab/agent_annotations_test.go) / [read_test.go](../../../../internal/mcp/read_test.go) | 最终答复与工具过滤、目录/记录索引、UTF-16 连续分页、完整封装预算、单条批注与精简状态/回复回执 |
| [current_test.go](../../../../internal/mcp/current_test.go) / [current_share_test.go](../../../../internal/collab/current_share_test.go) / [source_turn_test.go](../../../../internal/nativecodex/source_turn_test.go) | 原生调用身份、历史落盘等待、固定轮次完成、去重、取消、漂移、中断与重启不重放 |
| [management_test.go](../../../../internal/collab/management_test.go) / [input_management_test.go](../../../../internal/collab/input_management_test.go) / [collaboration_test.go](../../../../cmd/teamcross/collaboration_test.go) | MCP/CLI 创建与加入、输入归属与 epoch、角色拒绝、邀请失败恢复、同 fork 恢复 |
| [service_test.go](../../../../internal/service/service_test.go) / [onboarding_test.go](../../../../internal/collab/onboarding_test.go) | 实例身份、控制协议、稳定 opt 路径、邀请预览、Core 心跳与输入申请 |
| [cliinstall_test.go](../../../../internal/cliinstall/cliinstall_test.go) | 参数与带引号路径、未知命令保护、并发安装、重复移除和 App 更新后的命令行为 |
| [verify-release.py](../../../../scripts/verify-release.py) / [verify-homebrew.py](../../../../scripts/verify-homebrew.py) | CLI/App/DMG、App 命令安装、稳定/RC 独立 Homebrew 定义、临时前缀安装、同渠道双向互斥、升级与卸载；`--public` 另要求通过公开 Release URL 安装 |
| [verify-app-instance.py](../../../../scripts/verify-app-instance.py) | 两个真实 App 副本与 macOS URL/退出事件；隔离 Core、请求去重、路径别名、无响应与异常退出恢复；测试邀请不联系远端或调用模型 |
| [flows.test.tsx](../../../../packages/web/src/test/flows.test.tsx) | 首页、创建、邀请失败、输入交接状态与模型显示 |
| [reading-scroll.test.tsx](../../../../packages/web/src/test/reading-scroll.test.tsx) | 整页阅读的段落恢复、吸顶高度变化、标签与显式批注定位、隐藏阅读器、成员折叠与焦点保护 |
| [library_test.go](../../../../internal/collab/library_test.go) / [library.test.tsx](../../../../packages/web/src/test/library.test.tsx) | 固定引用选择、读取编号、个人/共享访问隔离、撤回、并发更新、内联入口和设置导航保留；[独立浏览器 fixture](../../../../internal/collab/library_browser_test.go) 使用合成内容与真实 Core/存储 |
| [annotation_reply_test.go](../../../../internal/collab/annotation_reply_test.go) / [runtime_test.go](../../../../internal/mcp/runtime_test.go) / [annotations.test.tsx](../../../../packages/web/src/test/annotations.test.tsx) | 单层回复、去重、并发快照、访问撤销、受限运行时工具、内嵌草稿与键盘保存 |

## ChatGPT 本机插件

设置与安装回归见 [连接生命周期](../../../../internal/pluginpack/connection_test.go)、[本机 API](../../../../internal/collab/plugin_connection_test.go) 与 [连接页面](../../../../packages/web/src/test/plugin-connection.test.tsx)。覆盖显式首次启用、未创建 profile 的只读状态查询、来源保留、首次连接使用当前 Core、相同字节但绑定不同的自动迁移、切换前后失败的显式重试、无目录参数的 CLI 更新保留绑定、另一 App 不能自动接管、原数据不变、相同版本号不同字节、移除与停用不自动重装、失败后显式重试、并发锁与符号链接保护。旧代号或不同 Core 的界面回执不能清除新版加载提示。

`python3 scripts/verify-plugin-connection.py --teamcross-bin /absolute/teamcross --codex-bin /absolute/codex --output /fresh/output` 使用新的独立原生 profile，验证真实手动安装接管、已启用插件从旧目录自动切换到当前 Core、原生缓存资源与实际 bootstrap scope、旧进程与新进程的 bootstrap 回执、兼容命令、外部移除、无关配置和数据保留。不启动模型回合。App 构建后运行 `python3 scripts/verify-app-instance.py '/absolute/Team Cross.app' --plugin-codex-bin /absolute/codex`，追加主实例启动时的同步与数据目录校正验证；通过临时 App 和 profile 操作，不控制 ChatGPT 主窗口。没有 `--plugin-codex-bin` 的普通 App 门槛仍验证未启用时不产生插件写入，不要求 runner 安装 ChatGPT。

回归见 [UI MCP](../../../../internal/mcp/ui_test.go)、[本地包管理](../../../../internal/pluginpack/plugin_test.go) 和 [完整 WebGUI 宿主适配](../../../../packages/web/src/test/plugin.test.tsx)。检查 global/thread 元数据、嵌入资源、纯协议握手不启动 Core、UI 路由白名单、共享运行时的原有工具和配对契约保留、完整 App 导航、固定版本、读写分离、沙盒存储、Core scope 隔离、原有配对流程、同请求回复去重与消息不自动重放。构建的 iframe 应在 `connect-src 'none'` 下工作，不依赖外部脚本、字体或语法高亮资源。

空间工作台合入时同时检查插件中的工作台读取、简报保存、参与会话列表与请求入口；显式 UI 路由可用，内部 poll/claim 和原生 RPC 拒绝，写入不能改经读取工具调用。普通浏览器 WebGUI 的验证不能替代插件宿主桥验证。

Composer mentions 的 [MCP 回归](../../../../internal/mcp/mentions_test.go) 与 [Core 回归](../../../../internal/collab/mentions_test.go) 检查 app-only 搜索声明、允许空查询、中文与版本搜索、20 项上限、稳定排序、Core scope 和 URI 严格校验、撤回与成员撤销后的新查询/读取拒绝。检索不改个人选择、创建执行或生成读取编号；通用与共享 MCP 不获得搜索工具。下列验收脚本同时验证真实 stdio 和原生 app-server 所发现的工具元数据、搜索结果、材料固定版本与批注资源读取及回复分页。每个检索同时调用标准 `{query}` 和当前桌面实际使用的 `{query,path:[]}`，结果必须一致；拒绝非空、null 或非数组 path。不能以手工构造的 query-only 调用替代宿主请求格式验收。协议成功仍不能代替桌面输入框的搜索、选择与发送显示验收；成品阶段统一检查，不绕开 Codex 主窗口自动化限制。

使用两个终端，显式选择新的 fixture 与输出目录：

```sh
TEAMCROSS_PLUGIN_FIXTURE_DIR=/private/tmp/teamcross-plugin-fresh \
  go test ./internal/collab -run '^TestChatGPTPluginFixture$' -v -count=1 -timeout=91m

python3 scripts/verify-chatgpt-plugin.py \
  --fixture /private/tmp/teamcross-plugin-fresh/fixture.json \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex \
  --output /private/tmp/teamcross-plugin-check-fresh
```

fixture 使用合成原生历史，其他发布、版本、Core HTTP、存储、批注和读取编号均真实；不创建执行 fork。脚本在专用 Codex 配置目录完成 export/install/status/upgrade/remove，检查稳定启动路径、Core 绑定和其他配置保留，再核对精确引用及回复去重。安装仍生效时，在同一个隔离配置目录运行真实 app-server，验证仅启用本插件、资源 hash 与当前构建相同，以及固定读取编号可用；该步骤不发送模型输入。同名异源与符号链接拒绝由上述 Go 回归检查。

安装成品包并绑定该 fixture 后，可追加 `--installed /absolute/path/to/plugin-source` 检查真实插件缓存、原生 MCP 发现和资源内容；追加 `--model` 仅在专用仓库、ephemeral 个人会话使用 `gpt-5.6-luna`，核对实际读取指定材料版本和批注标记。个人 MCP 和无关插件在测试客户端中禁用，用户原配置不因此改变。

独立浏览器使用 `scripts/chatgpt-plugin-browser.py`，参数为 `--fixture`、`--teamcross-bin` 和新的 `--output`；对真实嵌入 HTML 使用 opaque iframe 和禁止网络的 CSP，业务工具通过本机 stdio，宿主 initialize/message/context 回执由测试页面模拟。在 1440、1024、768 CSS 像素及深浅主题复核、截图，检查整套 WebGUI 的空间、资源库、创建、加入和设置导航、版本切换、原文定位、回复回执与明确选定内容。与 fixture 上的普通浏览器 WebGUI 对照；不以只测试复用组件代替完整 App 验收。

真实桌面最后整体检查：global 和 thread 分别打开成品；读取、回复落盘、关闭重开、选择固定版本、点击分析后当前个人会话实际收到并读取正确引用。原生 app-server 成功不能代替宿主 `ui/message` 回执；独立浏览器模拟也不能证明桌面入口显示。禁止通过其他自动化方式绕开 Codex 主窗口控制限制。完成后写入 fixture 的 `finish` 并等待测试退出，关闭精确的浏览器、stdio、app-server 与辅助进程；保留测试数据供成品复核时，后续普通 Core 可读取已持久化材料。

## 空间工作台

仅修改 CLI 发现或错误恢复时，检查 [接收会话路径回归](../../../../internal/collab/receiver_discovery_test.go) 和 [工作台交互](../../../../packages/web/src/test/space-workbench.test.tsx)：设置与助手有效路径一致、设置变化立即生效、历史错误不冒充当前检测、只读刷新不启动会话、明确重试保留同一 ID、未知创建结果不重放。此范围不要求真实模型回合；使用可执行文件发现和模拟原生 RPC 即可。

接收生命周期修改检查 [审批与停止回归](../../../../internal/collab/receiver_lifecycle_test.go)、[安全交接回归](../../../../internal/collab/receiver_handoff_test.go) 和工作台交互：精确审批解决、跨 thread 隔离、旧 turn 拒绝、停止去重、暂停后投递/RPC 双重拒绝、等当前执行/审批/已接受调用退出、等待进程真正关闭、空历史拒绝、暂停持久化、同 ID/目录/配对恢复、最新模型继承和占用失败保留原状。空间结束后本机清理仍可用，恢复访问仍受限制。

`python3 scripts/verify-receiver-lifecycle.py --fixture-dir /fresh/output --teamcross-bin /absolute/teamcross --codex-bin /absolute/codex` 使用隔离 Core、home、目录和 `gpt-5.6-luna`，由第二原生 WebSocket 客户端回应精确测试审批，验证可再次发送、真实中断与去重。随后释放专属 worker，由另一个真实 app-server 恢复相同 thread：占用期间 Team Cross 恢复必须失败；续写、退出后接回原会话并核对新增历史、模型实际使用前轮标记、恢复不重放输入，以及审批期间暂停后自动释放。Desktop 打开仅捕获系统命令参数；此脚本不证明个人 Desktop UI 完整往返、侧边栏刷新、`agents` 的 `x` 快捷键或两台 Mac。`--preview-seconds` 可暂留首次审批现场供界面检查。

当前会话连接需验证 [传输身份与关联](../../../../internal/collab/agent_connections_test.go)、[ChatGPT 不透明身份](../../../../internal/mcp/current_test.go) 及 [插件回执不重放](../../../../packages/web/src/test/plugin.test.tsx)。`TEAMCROSS_TEST_NATIVE_DAEMON=1 go test ./internal/nativecodex -run '^TestExistingNativeDaemonConnection$' -count=1 -v` 启动专用 daemon，证明第二个代理只连接已加载的精确 thread、拒绝未知 ID、关闭借用代理不影响原 daemon；不调用模型。

真实已有会话连接使用 `TEAMCROSS_TEST_BINARY=/absolute/path/to/teamcross TEAMCROSS_TEST_CODEX_AUTH=/absolute/path/to/auth.json go test ./internal/nativecodex -run '^TestExistingConversationMCPRoundTrip$' -count=1 -v`。测试只用独立 home、repo、Core 和 `gpt-5.6-luna`；核对来源工具身份、同一 thread 的连接/投递/读取/完成及固定简报。仅允许精确测试会话的必要 Team Cross 工具审批，清理自身进程；结果不能推广到任意 Desktop 宿主。

普通请求的 [上下文回归](../../../../internal/collab/workbench_context_test.go) 覆盖简报变化、父结果快照、同 ID 不刷新、列表不重复长快照、大小预算与成员 CAS；界面另验证来源进展跳转、结果加入草稿但不自动保存。新增空间 mention 需检查 scope、只读资源和明确连接入口，旧材料 mention 的桌面验收不能替代新增行为。

[workbench_test.go](../../../../internal/collab/workbench_test.go) 覆盖同机三 Core 成员身份、明确目标关联、固定公开引用、请求/回执、撤销、简报 CAS、并发启用唯一角色、读取后接手、晚到结果、暂停阻止新请求、排队超时、取消启动、重启不重放，以及独立接收原生 ID 与绑定工具范围。相关变更运行 `internal/collab`、`internal/mcp` race 检查，并回归原有配对入口。[恢复回归](../../../../internal/collab/receiver_recovery_test.go) 另覆盖空命令表落盘、Core 重启、显式恢复后的首次投递、同请求去重、异常释放锁和控制状态有界等待；启动器回归覆盖状态超时、连接文件缺失或损坏但运行锁仍被持有时禁止重复启动。

真实模型检查使用专用空目录和当前构建：

```sh
python3 scripts/verify-space-workbench.py \
  --fixture-dir /private/tmp/teamcross-space-workbench-fresh \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex
```

恢复相关改动追加 `--reload-failed-receivers`：在两个成员的隔离数据目录种入原生创建前失败且省略空命令表的记录，重启 Core 后核对未自动创建，再明确重试并完成首次投递、后续请求链和 CLI 状态查询。此夹具不证明原生尚未落盘的零回合会话可以在 Core 重启后恢复。

脚本只用 `gpt-5.6-luna` 和隔离原生配置，启动同一台 Mac 上的三个 Core，通过真实 TLS 成员资格关联两名成员的独立接收会话。必须验证启动简报读取和接手、经明确授权的会话到会话 MCP 投递、关联请求及三个成员一致的完成回执、同 ID 去重、暂停/停用保留原生会话，且未启用共同执行。只能回应本测试精确原生会话、工具名称和请求 ID 的必要审批。模型返回摘要按明确指定的标记核对，协议完成不代表正文正确。脚本保留证据并退出自身 Core；不改写个人配置，不等价于两台 Mac 或 ChatGPT MCP Events 验证。

[space-workbench.test.tsx](../../../../packages/web/src/test/space-workbench.test.tsx) 验证工作台键盘导航、阅读器保留、固定引用发送、丢失响应只查同 ID、成员确认简报和显式接手预览。真实浏览器 fixture 使用合成材料和模拟接收者，但空间存储、成员访问和 HTTP 是真实的：

```sh
TEAMCROSS_WORKBENCH_BROWSER_DIR=/private/tmp/teamcross-space-browser-fresh \
  go test ./internal/collab -run '^TestWorkbenchBrowserFixture$' -v -count=1 -timeout=30m
```

按 fixture 输出的 `fixture.json` 打开专用页面，检查 1440/1024/768 像素、深浅主题、中英文、材料与批注就地发起、回执/关联导航、简报保存/冲突、专用角色初始化/暂停。创建该目录的 `finish` 文件结束；不得把模拟接收截图描述为真实模型回执。前端修改仍更新嵌入资源。

## 空间事件

运行 `go test ./internal/mcpevents` 和对应 race。检查发现与工具能力、空间凭据隔离、精确筛选、challenge、Standard Webhooks 签名、重启恢复、同 ID 重试、无历史重放、暂停/撤销/到期、失去访问、终态回执、密钥轮换及公网地址验证。测试回调使用受控 HTTP transport，不关闭生产出站限制，不把 loopback 假回调描述为真实 ChatGPT Cloud。

独立网关只转发 `/mcp` 的受限协议；拒绝 Core 路由、Origin、错误认证和超大请求。`event-gateway` 必须有独立 Core Bearer，不能从插件 app-only 工具调用。浏览器检查明确创建凭据、一次显示、丢失回执不自动重建、暂停/撤销以及宿主收到、模型读取和完成的分别显示。

真实 Cloud 验收需另有明确 HTTPS 目标、实际宿主订阅/验证/签名接收和模型处理证据。本次用户已暂缓该项；本地协议通过不声称已唤醒 Cloud 会话。LAN 与原生请求回归独立执行，不等待云端入口。

## 真实模型和客户端

个人 MCP 当前 Session 分享与 CLI 接力使用 [verify-agent-cli.py](../../../../scripts/verify-agent-cli.py)，每次选择新的空测试目录：

```sh
python3 scripts/verify-agent-cli.py \
  --provider codex \
  --fixture-dir /private/tmp/teamcross-agent-cli-fresh-fixture \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex
```

另以 `--provider claude --claude-bin /absolute/path/to/claude` 使用新的 fixture 重跑；Claude 要求已配置的本机 CliProxyAPI 路由，默认只读 `~/.claude/settings.json` 的授权用于内存转发，证据不保存真实密钥。两者限定 Luna，使用专用个人配置和会话；Codex fixture 仅为本次获授权的 `share_current_session` 设置[单工具批准](https://learn.chatgpt.com/docs/extend/mcp)，Claude 使用精确工具 allowlist，不修改用户个人 MCP 权限。

检查实际个人 MCP 的来源身份、登记后本轮完成、新 fork 包含最后 assistant 答复、CLI 无浏览器加入、批注/回复、非输入者拒绝、申请/交接/交还/接回、直接 TUI 连接、工具任务与重复 requestId、结束后同 fork 恢复。Claude 在首次输入持久化 fork 后核对完整历史。脚本退出精确关闭自身 Core、PTY 与测试 daemon，保留证据。它是同机两个 Core 的 loopback TLS 验证，不证明两台 Mac、Tailcat 或 Desktop。

[TestLiveCodex](../../../../internal/collab/live_test.go) 默认跳过。显式选择一个新的专用测试目录后运行：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

该用例建立测试仓库、来源和两个 fork，执行证明文件操作，并验证结束共享后独立原生进程可取得写入锁、只读不会重新占用、恢复保留同一 ID。全部真实模型调用限定 `gpt-5.6-luna`；不要把测试配置写入产品默认值。模拟模型名称只能证明转发、状态和拒绝语义，不能证明其他模型实际可用。

完成上述 fixture 后，可不发送 prompt 地检查真实 A/B 原生网关交接：

```sh
TEAMCROSS_LIVE_EXISTING_FIXTURE=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveNativeHandoff$' -v -count=1 -timeout=1m
```

该检查只接受 `TestLiveCodex` 生成的专用 manifest，不能指向普通用户会话。

原目录与新 worktree 分别覆盖直接 TUI、直接 Desktop、辅助 TUI、辅助 Desktop，共八种组合。实测核对执行主机、目录、fork、读写结果、审批、输入交接、并行辅助、客户端切换、重连、邀请失效和结束访问。

## LAN 与 Tailcat 网络门槛

[共享层 Tailcat 联网测试](../../../../internal/sharing/tailcat_live_test.go) 和 [协作层 Tailcat 联网测试](../../../../internal/collab/tailcat_live_test.go) 默认跳过。它们会访问 Tailcat DERP；只能在明确允许联网且能初始化系统网络监视的测试主机运行：

```sh
TEAMCROSS_TEST_TAILCAT=1 \
  go test ./internal/sharing ./internal/collab \
  -run '^TestLiveTailcat(Membership|Collaboration)$' \
  -v -count=1 -timeout=100s
```

共享层覆盖 Tailcat Server/Client、TLS 1.3 SPKI pin、多人共用链接加入和成员凭据重连；协作层再覆盖显式选择、状态、输入交接与直接客户端 WebSocket 桥接。两项在同一台 Mac 上运行，日志中出现 DERP 引导或随后出现 `via=direct` 只说明该次本机路径，不能证明两台物理 Mac、不同 NAT、受限 UDP 或持续 DERP 中继。

跨设备必须分别完成并记录：

1. 两台 Mac 同一 LAN，显式选择 `lan`，不使用 `--test-loopback`。
2. 两台 Mac 处于不同网络，显式选择 `tailcat`，覆盖加入、状态、直接客户端、输入交接、断线重连、离开和结束共享。
3. 阻断点对点 UDP 或使用受控 DERP，证明业务流量保持经 DERP；必须依据明确路径诊断，不能仅凭连接成功或启动时连接过 DERP 推断。

每项记录 Team Cross 提交、Go/Tailcat 版本、macOS 版本、网络条件、DERP 来源、可观察路径及清理结果。公共 DERP 可用性与性能不属于一次测试可长期保证的产品承诺。

## 界面与测试收尾

页面检查包括首页、创建、详情、加入、设置和关键失败状态；在 1440、1024、768 CSS 像素及深浅主题下检查布局。核对实际视口、长路径、水平溢出、滚动、焦点、键盘和主要动作可达性，并实际查看截图。

完成后关闭本次启动的专用 Desktop/TUI、Core、app-server、浏览器页面及其测试辅助进程。先确认 PID、父子关系或测试数据目录，再退出对应实例；保留用户正在使用的 Codex 和其他浏览器内容，不按程序名批量终止。测试会话、仓库和验收材料按本次约定保留。

验证结果可先在提交或 PR 中简要说明；需要详细记录时，按 [任务记录规则](../../README.md#任务记录) 写明日期、版本、环境、执行的检查及未覆盖范围，收尾后归档。新增一份执行记录不要求更新 Wiki 索引或短页面。

## Claude 原生 TUI

Claude 的 Go 回归位于 `internal/nativeclaude` 与 `internal/collab/claude_test.go`；覆盖历史链、过期起点、原生 fork 参数、只复制所选来源、只发布新 fork 的单文件个人 history 入口、个人 settings/全局状态/认证/插件/来源及其他历史不被 Team Cross 改写、协作运行配置隔离、精确 job 停止、终端白名单、取消、断线不重放、TLS 成员访问、输入收回、就绪判断和不支持的 API。

真实验收使用 [verify-claude-native.py](../../../../scripts/verify-claude-native.py)，显式传入一个新的空测试目录、当前构建 CLI 和不低于 `2.1.268` 的 Claude 正式版（实际运行版本写入验收记录）：

```sh
python3 scripts/verify-claude-native.py \
  --fixture-dir /private/tmp/teamcross-claude-fresh-fixture \
  --teamcross-bin /absolute/path/to/teamcross \
  --claude-bin /absolute/path/to/claude
```

脚本只使用本机 CliProxyAPI 中的 `gpt-5.6-luna`。凭据仅在 loopback guard 内存中使用，测试配置写入占位 token；所有实际模型请求逐个检查模型名。脚本建立专用个人 Claude home、额外个人历史哨兵、来源、原目录/worktree 原生 fork、两个真实 Core 与原生 TUI；校验 runtime 只复制所选来源、fork transcript 以单文件进入 A 的个人 history 并出现在原生 `/resume` picker、零输入不以 JSONL 落盘为创建门槛、个人 settings/插件标记/来源/其他历史不变、结束时只停止本次 job，以及执行端、审批交接、发送、输入后恢复与清理。`--keep-preview-seconds` 可暂留测试 Core 供页面复核，创建测试目录下的 `finish-preview` 可提前结束。此脚本属于同机验证，不能代替真实双 Mac LAN 验收。

## 个人 Claude 辅助 MCP

使用 [verify-claude-assist.py](../../../../scripts/verify-claude-assist.py)；参数形式与原生脚本相同，使用新的空测试目录。脚本运行真实 `claude mcp add --scope user`，验证重复配置与其他条目保留，然后通过普通个人 TUI 加载用户配置。禁止用显式 `--mcp-config` 注入替代个人配置验收。

门槛包括：工具列举、A 上文件/历史/批注读取、非输入者发送拒绝、交接后发送、相同请求 ID 去重、个人与共享会话分离、同一 worker 的直接 TUI 接入、结束共享后工具访问撤销。测试可为明确列出的 Team Cross 工具设置允许清单，不使用全局跳过权限；个人 TUI 不授予本地 shell/file 工具。所有模型请求继续只允许 CliProxyAPI 的 `gpt-5.6-luna`。

UI 按钮打开另需可访问 macOS 桌面自动化的本机 Core；沙箱中的 `osascript` 失败不能当成产品按钮成功。协议检查、实际客户端工具调用、启动请求与进程/原生 TUI 就绪分别记录。关闭 PTY 后再等待客户端退出，并独立清理每个测试资源，某个 TUI 退出异常不能跳过其他 Core 和验收材料保存。

配置与数值版本回归见 [mcp_test.go](../../../../internal/nativeclaude/mcp_test.go)，跨 Provider 辅助与 Codex 控制能力回归见 [assist_test.go](../../../../internal/collab/assist_test.go)。

## 共享原生批注工具

使用 [verify-annotations.py](../../../../scripts/verify-annotations.py)，分别为 Codex 和 Claude 选择新的空测试目录：

```sh
python3 scripts/verify-annotations.py \
  --provider codex \
  --fixture-dir /private/tmp/teamcross-annotations-fresh-codex \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex

python3 scripts/verify-annotations.py \
  --provider claude \
  --fixture-dir /private/tmp/teamcross-annotations-fresh-claude \
  --teamcross-bin /absolute/path/to/teamcross \
  --claude-bin /absolute/path/to/claude
```

脚本通过直接 TUI 读取只有批注与人工回复中才有的标记，再回复原批注，并验证结束共享后恢复同一会话仍能读取。真实模型只使用 Luna，Claude 沿用本页的 loopback guard；Codex 使用专用配置和已有登录凭据，不修改个人配置。检查继承的个人 MCP 被禁用；只接受本次批注工具对应的确认，不跳过所有审批。

`--keep-preview-seconds` 可暂留 fixture 供 WebGUI / 专用 Desktop 检查；写入 fixture 的 `finish-preview` 可提前结束。Desktop 须另外确认窗口内的实际读取与回复，进程启动或网关连接不等价于完成验收。

## 信任模式

为上面的 `verify-annotations.py` 增加 `--runtime-mode trusted`，分别为 Codex 和 Claude 使用新的专用 fixture。脚本继承测试 home 的只读个人 MCP 和无副作用标记 hook，并确认原生 TUI 实际读取工具返回值、执行 hook、读写批注、结束和同 ID 恢复。Codex 另核对主机权限 profile 与原生 hook 审阅写回 A；允许原生插件加载，不调用无关个人工具。Claude 的临时 home 与 Luna guard 保持隔离。默认参数仍验证受限模式的个人 MCP 隔离。

可信执行不等于跳过原生确认；脚本只能确认可见的专用测试 hook 和批注工具。新配置或插件可能延长客户端启动时间，必须等原生历史实际呈现再发送输入。Claude 结束应等待精确 worker 退出，保留个人 daemon 及其他 job；普通主机环境和沙箱的进程身份探测能力分别记录。浏览器、电脑控制、组织策略、第三方登录与专用 Desktop 窗口需要各自的实际验收，不能由继承配置测试直接推断。

## 接收会话配对

配对相关变更覆盖 `agents_test.go`、MCP `agents_test.go` 和 Web `agent-pairings.test.tsx`：验证一次性码、准确会话与运行时范围、接收挑战、权限撤销、断线、并发同 ID 去重、发送/读取/完成顺序及重启不重放。运行相关 race test，前端检查中英文、丢失响应后只查询同一请求，并进行实际浏览器与截图复核。

```sh
python3 scripts/verify-agent-pairing.py --fixture-dir <新的空目录> --teamcross-bin "$PWD/bin/teamcross" --codex-bin <Codex绝对路径>
python3 scripts/verify-agent-channel.py --fixture-dir <另一个新的空目录> --teamcross-bin "$PWD/bin/teamcross" --claude-bin <Claude绝对路径>
```

两者只在专门仓库、独立 home 与 Core 中使用 `gpt-5.6-luna`。Codex 脚本通过共同执行入口新建 fork，验证真实 TUI 配对、发送引用、读取原批注、完成摘要、重复请求不重放和只分析不回复；仅确认本次已知工具请求的原生审批。工作台新建会话的流程由 [空间工作台门槛](#空间工作台)覆盖；两者均未验证将测试前已存在的 Codex 会话接入投递连接的流程，不能将这一覆盖范围当作会话能力分类。Claude 路径要求当前客户端和账户允许开发 Channel，并保持测试模型代理可用；通过 STDIO 写入测试或宿主提示 Channel 不可用时，不能声称真实接收通过。不得为通过测试而伪造挑战回执或绕过宿主能力限制。

完成后关闭本次 TUI、Core、守护进程及测试浏览器，保留检查报告。单机检查不代表两台 Mac 或 ChatGPT Cloud MCP Events 验收。实际结果按任务记录规则另存。
