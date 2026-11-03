# 成员动态实现与验证

> 截图仅本地保存，路径相对本文，不随 Git 分发；保存与引用方式见[验收截图约定](../README.md#验收截图)。

日期：2026-10-23，Asia/Shanghai。实现阶段基线为 `codex/reading-experience` / `8bc8397702bdc7ef6911678a2c6b9e726ad173dc` 加本任务工作树；仓库已有阅读体验、嵌入资源和分身设计改动，保留其内容。本记录不把基线提交或其他在途任务宣称为本次全部实现。下列检查记录于提交和安装包构建前。

用户确认依据：[完整设计](2026-10-23-member-activity-design.md)。空间内动态已按修正放进**空间概览页面正文**，没有新增空间侧栏入口；后续补充的视觉约束已落实为复用现有留白、分隔线、字体、头像、按钮与主题变量。

## 实现结果

- 材料、根讨论、协作会话的关注／取消／静音／恢复，发布或参与自动关注，显式取消不会被后续发言覆盖。材料标题、资源库阅读操作、讨论和会话标题，以及动态管理列表复用同一偏好。
- 结构化 @ 成员、UTF-16 正文范围校验、离线有效成员选择、明确的回复父项与父项短引文。稳定人／会话作者关系；已核实归属的参与会话回复保留成员，未知共同执行作者不猜配。
- 业务提交时计算收件原因，源记录 outbox 与单独空间日志持久化、稳定 ID 去重、按对象分组；@／直接回复／多层关注合并为一份个人提醒。原生 accepted 输入关系绑定实际 turn，结果归属不随输入交接变化。
- 全局“我的动态”和概览内摘要／完整列表共用主机已读；精确来源加载与可见确认、快照全部已读、只增水位、离线已读队列及恢复补交。原文弹窗周期核对访问，来源失效清除正文；离线缓存只保留计数与定位元数据。
- 关注清单／动态分页、过期游标与并发未读变更拒绝、90 天已读事件保留规则、数量及字节限制。私人动态不进入公共 View、发布材料或绑定运行时工具；个人 MCP 及嵌入 UI 白名单接入。

当前规范分别维护在[成员动态契约](../sources/decisions/member-activity.md)、[协议](../sources/protocol.md#成员动态接口)、[产品流程](../sources/product-flows.md#成员关注与动态)和[验证门槛](../sources/validation/test-gates.md#成员动态)。顺流感知、Cloud ack、拟议分身持续关注保持独立授权及进度，本次没有启动它们。

## 自动化验证

Go 使用 `GOCACHE=/private/tmp/teamcross-member-activity-gocache`，避免写默认沙箱外缓存。日志仅覆盖本次所列工作树，不代表已有发布包。

| 检查 | 结果与证据 |
| --- | --- |
| `go test ./...` | 通过；[日志](2026-10-23-member-activity-evidence/go-test.log) |
| `go vet ./...` | 通过；[日志](2026-10-23-member-activity-evidence/go-vet.log) |
| `go test -race ./internal/collab ./internal/mcp ./internal/mcpevents ./internal/sharing ./internal/nativecodex ./internal/nativeclaude` | 通过；[日志](2026-10-23-member-activity-evidence/race.log)。后续作者关系和私密边界补充以[专项 race](2026-10-23-member-activity-evidence/boundary-race.log)复验 |
| `go test -race ./internal/collab ./internal/mcp -run 'TestMemberActivity\|TestAnnotation\|TestWorkbench\|TestRuntime' -count=1` | 后续幂等校验顺序、取消关注不依赖历史读取及动态字节预算的相关回归通过；[日志](2026-10-23-member-activity-evidence/focused.log) |
| `pnpm --filter @teamcross/web check` | 通过，1263 条中英消息；[日志](2026-10-23-member-activity-evidence/web-check.log) |
| `pnpm --filter @teamcross/web test` | 25 个文件、233 项通过；[日志](2026-10-23-member-activity-evidence/web-test.log) |
| `pnpm --filter @teamcross/web build` | 两套嵌入资源构建通过；[日志](2026-10-23-member-activity-evidence/web-build.log) |
| `go build -o /private/tmp/teamcross-member-activity-20261023 ./cmd/teamcross` | 构建通过，未替换用户运行中的 Core |
| 文档相对链接、代码路径、索引与 `git diff --check` | 通过；当前设计链接到已实施契约与本记录，旧单层回复协议已同步 |

新增 Core 的 11 项成员动态回归覆盖三 Core 收件隔离、回复到回复、组合原因、取消与父对象静音、持久 outbox 故障恢复、并发已读、UTF-16、会话输入权变化、分页变化、容量／保留、离线补交／撤权、损坏日志、运行时私密边界、已核实 Agent 作者和终态先于 steer 回执。另有 MCP/UI 路由边界回归及四项前端交互回归，包含权限重新校验后移除已加载正文。

## 浏览器与视觉复核

独立 fixture 使用同一 Mac 的三个 Core、真实 HTTP/TLS 与持久材料／讨论；原生历史为合成，无模型轮次。多次构建复核中发现并修正了窄屏“仅未读”被挤成竖排，以及菜单被管理弹窗截断的问题；最终菜单按视口定位，键盘 Escape 和焦点离开可收起，选择后关闭。

已检查：

- 1440 宽中文浅色：概览正文摘要、全量展开、原有侧栏结构；最终概览（`2026-10-23-member-activity-evidence/overview-1440-light.png`）。
- 843 宽中文浅色：展开筛选正常换行，沿用原有折叠侧栏；展开状态（`2026-10-23-member-activity-evidence/overview-843-expanded.png`）。
- 1024 宽：关注管理列表；截图（`2026-10-23-member-activity-evidence/follow-manager-1024.png`）。
- 768 宽：关注菜单完整可见、静音保存、解除后恢复原关注；最终菜单（`2026-10-23-member-activity-evidence/follow-menu-768.png`）。英文深色全局列表、角标、筛选、来源用户文字不翻译；截图（`2026-10-23-member-activity-evidence/global-768-english-dark.png`）。
- 在回复框输入 `@周`，候选中选择周悦，Enter 接受候选后保存测试回复；回执显示准确父回复的可点击短引文。候选与父项（`2026-10-23-member-activity-evidence/mention-reply-1440-light.png`）。
- 动态弹窗读取实际讨论，打开原文保留 spaceId/annotationId/replyId 并展开评论。原文位置（`2026-10-23-member-activity-evidence/source-reply-1440.png`）。
- 同一成员的全局、概览在两个页面同时打开：全局全部已读后，全局立即更新，概览按轮询同步，后台列表读取本身不清空提醒。其他成员隔离由三 Core 确定性测试证明。

最终概览页面没有浏览器控制台 error。截图为合成测试内容；不包含真实协作讨论。

## 真实原生结果与限制

`TEAMCROSS_ACTIVITY_LIVE_DIR=/private/tmp/teamcross-member-activity-live-scope-20261023 go test ./internal/collab -run '^TestLiveMemberActivity$' -v -count=1 -timeout=9m` 通过。真实 Codex `0.163.0-alpha.11.2` / `gpt-5.6-luna` 使用专用 home、仓库和 Core；只链接本地认证，不复制或输出凭据。[结果日志](2026-10-23-member-activity-evidence/native-member-activity.log)。

来源回合完成后，分别在原目录和新 worktree 的 fork 中请求无工具答复；核对精确标记、executionId、原生 turn ID、own_turn_result 与已读归零。测试总时长 52.25 秒。输入权交接及 pending steer 的确定性故障顺序另由 Core 测试覆盖。

曾运行范围更广的 `TestLiveCodex`：来源创建成功，原目录文件执行在 `item/fileChange/requestApproval` 停于“command failed; retry without sandbox?”，该检查失败且没有自动放宽权限。随后运行上述针对成员动态的纯答复检查，未把它改称文件执行验证通过。

本轮没有证明直接 TUI/Desktop 的完整交互、实验性 Claude、真实 ChatGPT 插件宿主、两台 Mac、跨 NAT 或 Tailcat 已验收。UI 路由、个人 MCP 及构建资源有自动化验证；普通浏览器不替代插件宿主验证。中文输入法由 composition 保护实现，浏览器实测为中文文本输入和键盘候选选择，不宣称已做所有 macOS 输入法组合。

## 清理

浏览器 fixture 均通过写入各自目录的 finish 正常退出，关闭所建 Core／共享连接；真实模型测试通过 defer 关闭专用 app-server 和 Core。关闭本次两个浏览器标签并恢复视口覆盖；八个 fixture HTTP 监听端口均已不可连接。专用真实会话、仓库、worktree、构建文件和日志保留在对应测试目录，不触碰用户原有会话、运行中的 Core 或已有工作树。
