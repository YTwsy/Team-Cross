# 协作体验衔接

状态：完成约定的实现与本地验证。2026-10-04，基于 `Meta` / `ae66ff6`，工作分支 `codex/collaboration-flow`；本记录对应未提交的工作树。用户授权推进三个部分，不创建子 Agent；随后明确暂缓真实 ChatGPT Cloud 宿主验收。未部署公网，未修改个人插件安装或原生配置。

## 完整范围与完成证据

1. 当前会话连接：空间 mention、对话面板连接动作、传输身份核对、去重和空间关联；接通已有 Codex 会话的实际原生连接。分别报告空间关联与主动接收状态。材料/批注 mention 保持只读，保留跨客户端配对提示。验收需覆盖真实专用已有会话的绑定、投递、读取和结果回执。
2. 请求与简报衔接：普通请求携带不可变简报版本及前序结果入口；来源旁展示相关进展；结果可生成成员确认的简报草稿，保存仍执行版本比较。覆盖多成员权限、撤回、并发、同请求去重和重启不重放。
3. 持续接收：明确授权的订阅、事件筛选、去重、撤销与暂停，以及 MCP Events 发现、订阅、回调验证、签名投递和持久化。独立回环网关仅暴露受限 `/mcp`；空间凭据与 Core Bearer 分离。真实宿主订阅和处理回执需未来独立取证，本次用户明确暂缓，未将协议测试标成 Cloud 通过。

工程门槛：Go test/vet、相关 race、Web check/test/build，两套嵌入资源，真实浏览器与截图，中英和深浅主题。真实模型只用 `gpt-5.6-luna`，专用会话/配置/仓库，关闭本次测试进程。更新领域契约、协议、验证门槛和证据入口。

## 实现

- 空间 mention 可直接读取简报和角色；面板明确点击发送当前会话连接要求。`connect_current_session` 从工具传输核对身份、复用已有空间关联；ChatGPT 的不透明 session 键只用于 `linked`，原生接收另行核验。原有复制配对提示保留。
- 已有 Codex 通过 `app-server proxy` 字节隧道上的 WebSocket 接入同一原生 daemon，核对已加载 thread；连接不启动、fork 或 resume 用户会话，不改模型设置和审批。
- 普通请求保存简报版本、不可变快照及父结果；来源旁显示相关进展，结果生成带请求来源的简报草稿，仍由成员 CAS 保存。过大上下文在投递前拒绝。窄栏进展长文本已修正为受限宽度与省略，不撑开批注网格。
- 本地事件协议提供三种筛选事件、挑战签名、稳定 ID 重试、暂停/撤销/到期、重启恢复、读取/完成分离及限制读取范围。独立 CLI 网关不会转发 Core 管理接口。LAN 成员加入与原生投递无需此网关。

## 原生与 LAN

环境：单 Mac，Codex CLI `0.160.0`，真实模型仅 `gpt-5.6-luna`，独立 home、repo、Core 和测试会话。未将同机结果称为两台 Mac 验收。

- `TEAMCROSS_TEST_NATIVE_DAEMON=1 go test ./internal/nativecodex -run TestExistingNativeDaemonConnection -count=1 -v`：通过。第二个代理接入精确已加载会话，未知 ID 拒绝；关闭代理保留所有者与 daemon。无模型输入。
- `TEAMCROSS_TEST_BINARY=/private/tmp/teamcross-collaboration-flow-test TEAMCROSS_TEST_CODEX_AUTH=/Users/wsy/.codex/auth.json go test ./internal/nativecodex -run TestExistingConversationMCPRoundTrip -count=1 -v`：通过，36.41 秒。相同 thread 经真实 MCP 连接、提交、读取和完成，固定简报版本 1。最初 fixture 的 `approval_policy=never` 拒绝写工具，改为仅精确测试会话的指定工具审批后重跑通过；生产权限未放宽。
- `python3 scripts/verify-space-workbench.py --fixture-dir /private/tmp/teamcross-collaboration-flow-lan-20261004 --teamcross-bin /private/tmp/teamcross-collaboration-flow-test --codex-bin <ChatGPT bundled Codex>`：通过。三 Core 真实 TLS，两名成员独立接收，启动接手、授权会话间分派、父请求归属、三个成员一致的完成记录、同 ID 去重、暂停/停用保留会话。未启用共同执行或 Cloud 网关；三个 Core 均正常退出。[结构化结果](evidence/lan.json)。

## 事件与界面

- `internal/mcpevents` 的受控回调测试核对 Standard Webhooks 签名、challenge 失败不建订阅、相同订阅刷新、503 后同事件 ID 重试、重启持久化、2xx/读取/完成分离、筛选与权限、暂停/撤销/过期、410/413、密钥轮换与刷新保留双签、过期待投递事件取消。生产 DNS/地址限制保持启用。
- 真实 `teamcross events` CLI 连接隔离 Core：发现、目录、空间简报读取、错误凭据、跨空间参数、Core 路由、Origin、内部 Core Bearer、暂停/恢复/撤销均核对通过。[网关结果](evidence/gateway.json)。该项未联系真实 Cloud 宿主。
- `TestChatGPTPluginFixture` 提供合成材料/接收者，Core、存储、HTTP 与 stdio 为真实实现；通过后正常结束。随后以最终 CLI 和两套嵌入资源复核同一持久化数据。原生模型链独立于此浏览器 fixture。
- 浏览器核对来源进展打开精确请求及固定快照；请求结果先进入草稿，Core 仍为版本 1，成员点击保存后变为版本 2，保留来源请求与材料/批注。插件连接点击只产生一次模拟宿主 `ui/message`，没有声称已配对或已接收；通知连接创建、隐藏凭据、暂停和撤销走真实业务桥。
- 1440/1024/768 像素、中英和深浅主题复核；窄栏溢出在最终构建中修正。截图见 [来源进展](evidence/web-source-1024-en.png)、[成员确认简报](evidence/web-brief-1024-zh.png)、[插件当前会话连接](evidence/plugin-connect-768-en-dark.png)。宿主初始化/message/context 回执是模拟；新增空间 mention 及快捷绑定未做真实 Desktop 宿主验收。

## 工程与收尾

- `go test ./...`、`go vet ./...` 通过；collab/mcp/mcpevents/sharing/nativecodex/nativeclaude/service/cliinstall/pluginpack 的 race 通过。事件轮换与过期队列新增回归再次通过 race。
- Web check 通过，965 条中英消息完整；15 个文件、151 项交互测试通过。两套 production 资源重建通过，CLI build 通过；保留既有大 chunk 构建提示。
- 领域契约、协议、验证门槛、CI race 范围与证据入口同步更新。文档链接、代码路径和 `git diff --check` 在收尾核对。
- 测试只使用当前任务的临时目录。测试 daemon、Core、事件网关、MCP stdio、浏览器及辅助进程按各自 PID/会话关闭，不批量终止用户客户端；不删除个人会话或工作目录。

## 界面优化复核

2026-10-04 后续界面优化，仍基于同一未提交工作树。此次仅调整 React 界面、样式、中英文本与相关交互测试，没有修改原生接收、LAN 或事件投递实现。

- 工作台采用紧凑分段导航，普通请求是主要动作，专用会话设置降为次级入口。参与会话先列出已有目标和接收状态；添加入口集中到一个弹窗，明确选择关联已有或新建独立会话。当前会话的自定义名称按需展开，空名称可以直接发起连接。
- 请求结果、来源、固定简报和继续协作分层展示；详情入口命名为“请求详情”。只有一条请求时使用完整可用宽度。确认简报分栏展示决定和待解决事项，未保存草稿有独立提示，来源改为可搜索的勾选列表，编辑区底部保留保存操作。
- `pnpm --filter @teamcross/web check` 通过，981 条中英消息完整；15 个文件、154 项交互测试通过。新增测试核对添加会话只在明确确认后写入、可跳过名称步骤的当前连接、搜索来源时保留其他勾选以及切换页签保留草稿。`go test ./...`、`go vet ./...` 与工程门槛的 9 个包 race 通过；两套嵌入资源与 CLI 重新构建通过，保留既有大 chunk 提示。
- 独立 `TestChatGPTPluginFixture` 正常通过。随后使用最终 CLI 读取同一隔离数据，普通浏览器读取真实嵌入 WebGUI，插件 harness 读取最终 stdio MCP 的内嵌面板。材料、成员和请求结果为合成数据，Core HTTP、存储和插件业务桥为真实实现；不运行模型。
- 真实浏览器核对两种添加方式只切换表单而不创建会话；打开固定请求上下文；请求摘要先变为草稿，经成员编辑保存后简报从版本 1 变为 2。之后搜索并勾选材料版本 2，切换页签保留草稿与原版本 1 来源，明确保存后版本变为 3。最终插件点击连接只记录一次 `ui/message`，按钮停止重复发送，没有把宿主消息回执当成实际接收能力。
- 最终 WebGUI 的中文浅色和插件英文深色分别检查 1440、1024、768 CSS 像素的请求、简报和参与会话；均无页面横向溢出。实际截图发现并修正添加方式单选框宽度和通知折叠箭头；最终构建再次复核。通知展开只检查可见表单，不创建凭据或订阅。浏览器日志中的 harness favicon 404 不涉及产品资源；首轮合成 fixture 的插件安装状态检查返回 400，最终普通 Core 页面无此问题。
- 保留最终截图：[添加参与会话](evidence/ui-add-1024-zh.png)、[确认简报](evidence/ui-brief-1024-zh.png)、[请求结果与后续操作](evidence/ui-requests-1440-zh.png)、[英文深色插件](evidence/ui-plugin-sessions-768-en-dark.png)、[插件添加弹窗](evidence/ui-plugin-add-768-en-dark.png)。宿主初始化和 `ui/message` 回执仍由浏览器 harness 模拟，不能替代真实 ChatGPT Desktop / Cloud 宿主验收；本轮没有重跑真实模型与跨 Core LAN 链路。
- 收尾对空简报的新增计数与待解决事项显示加入空值保护，相关 4 个测试文件的 24 项检查再次通过，并重新生成两套嵌入资源和 CLI；布局与截图一致。283 个文档相对链接、路径、新证据锚点以及 `git diff --check` 通过。两个浏览器会话、fixture、普通 Core 和两个 harness 的 stdio 均已退出，核对已知 PID 无剩余；关闭浏览器时的 harness BrokenPipe 只来自已断开的测试请求。

## Meta PR 整合复核

2026-10-04，将原工作树的 78 个修改或新增文件保存为 `0b1792b`，在独立 worktree 的 `codex/collaboration-flow-meta` 分支整合 `origin/Meta` 的 `c4b72c0`。原工作区的文件、分支和暂存区保留，文件 hash 与快照一致。

- 保留 Meta 的 Codex CLI 自动发现与客户端设置修复。MCP UI 的 `info?refreshClients=1` 只读查询与空间事件路由同时保留；中英消息合并没有重复键。生成资源的重命名冲突通过重新构建两套嵌入资源解决。
- Go test/vet、验证门槛中 9 个包的 race、CLI build 和 10 项 Homebrew 渲染测试通过。Node `24.18.0`、pnpm `11.19.0` 下 Web check/test/build 通过，1000 条翻译完整，16 个文件的 161 项测试通过；`git diff --check origin/Meta` 通过。
- 真实浏览器复核中文客户端设置、英文浅色 WebGUI 和英文深色插件。请求、简报、会话列表分别检查 1440/1024/768 CSS 像素，无页面横向溢出；检查添加弹窗和无需填写名称的当前会话连接。插件的重新检测经真实 stdio/HTTP 业务桥成功返回；连接动作只记录一条模拟宿主 `ui/message`，随后禁止重复发送。
- 截图：[客户端设置](evidence/meta-web-client-settings-1440.png)、[整合后的简报](evidence/meta-web-brief-1024.png)、[插件添加会话](evidence/meta-plugin-add-768-dark.png)。fixture 的原生历史和接收者仍为合成数据；`/usr/bin/true` 测试路径呈现版本检测失败，插件安装状态查询的失败不作为真实宿主安装验收。未重跑真实模型、跨 Core LAN 或 Cloud 宿主验收。
- `TestChatGPTPluginFixture` 正常通过；两个专用浏览器会话、harness 和 MCP stdio 按自身会话关闭，fixture Core 随测试退出。
