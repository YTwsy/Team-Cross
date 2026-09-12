# Claude Code CLI/TUI 接入实测记录

2026-09-11，在同一台 Mac 上使用本机 Claude Code **2.1.268**、官方 Agent SDK **0.3.268** 和 CliProxyAPI 的 **gpt-5.6-luna** 完成组件实测。Luna 在真实 Claude CLI 中正常回复，首次成功请求的 CLI 总耗时为 3.251 秒。请求与运行时报告均指向 gpt-5.6-luna。

**结论：历史 fork、恢复、外部审批和本机原生 TUI 的基础能力已经得到实际验证。剩余关键工作是让 Team Cross 控制端与协作者的原生 TUI 连接同一个、留在 A 上的运行实例，并把所有写入纳入输入归属校验。尚未实现或验收完整 Claude Provider。**

## 本次已验证

| 对齐能力 | 实测结果 | 证据 |
| --- | --- | --- |
| 真实模型回复 | 通过 Claude CLI → 本地测试转发器 → CliProxyAPI → Luna，收到精确测试标记 | [luna-stage1-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage1-summary.json>) |
| 无业务消息创建持久化 fork | 官方 SDK forkSession() 成功：返回新 ID、立即存在于磁盘、消息内容一致、消息 UUID 更新、原会话字节不变；随后真实 CLI 恢复成功 | [offline-fork-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/offline-fork-summary.json>)、[luna-sdk-attach-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-sdk-attach-summary.json>) |
| 带历史的 CLI fork | --resume SOURCE --fork-session 得到新会话，准确回答来源会话标记，原会话未改变 | [luna-stage2-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage2-summary.json>) |
| 恢复同一会话 | 关闭并重启 CLI 后 --resume FORK 仍使用同一个会话 ID，并保留历史 | [luna-stage2-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage2-summary.json>) |
| 新 worktree | 在专用 fixture 的新 Git worktree 中启动 fork，历史标记保留；system/init 与回复中的 cwd 都为新 worktree | [luna-stage2-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage2-summary.json>) |
| 外部审批拒绝与允许 | stdio 收到真实 can_use_tool/Bash 请求及 request_id；回复前文件不存在；拒绝后不执行；允许后创建准确内容的测试文件 | [luna-stage2-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage2-summary.json>) |
| 取消后的旧审批回复 | interrupt 得到确认，并收到 control_cancel_request；随后发送旧 allow，文件未创建 | [luna-stage2-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-stage2-summary.json>) |
| 结构化提问 | AskUserQuestion 发出真实外部回调；传入 ALPHA 答案后模型回复 ALPHA | [luna-sdk-attach-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-sdk-attach-summary.json>) |
| 原生后台 TUI | --bg 创建 fork，claude attach JOB 显示来源历史、接受真实输入并显示 Bash 审批；在 TUI 按 Yes 后命令执行，最终助手回复持久化 | [luna-native-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-native-summary.json>) |
| 原生 TUI 脱离与重连 | Ctrl+Z 正常退出 attach 客户端，后台 worker 仍存活；重新 attach 看到历史、执行结果和同一会话；/status 指向原 worker 的 Unix socket | [luna-native-summary.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-native-summary.json>)、[luna-native-reattach.txt](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/luna-native-reattach.txt>) |

SDK 的独立 forkSession() 与 CLI --fork-session 是两个入口。本次 CLI 只初始化而不发送首条消息时没有新 transcript；官方 SDK 的独立函数补齐了“先创建可恢复 fork，再等待用户输入”的能力，不能把前者的延迟写入判断为 Claude 不支持这个产品流程。[官方会话管理示例](https://platform.claude.com/cookbook/claude-agent-sdk-05-building-a-session-browser)。

## 已定位的接入差异

**SDK 运行实例不能仅凭会话 ID 被本次使用的 attach 命令接管。** SDK 实例 PID 为 25491，会话 ID 为 945a2f16-5a20-4eab-a645-f73334fea8b3。运行中的实例能够被 agents --json 列为 interactive，但 claude attach SESSION_ID 返回 No job matching。原生 --resume 同一 ID 加载了历史，另起 PID 25528；其 /status 显示 interactive，peer 为自己的 /tmp/cc-socks/25528.sock。这次 --resume 没有接到 PID 25491 的活跃执行端。

**本机原生后台 attach 是可行入口。** 成功实验中 worker PID 为 24676，会话 ID 为 8e05faad-accc-48c8-8920-c139831daf11。重连客户端 /status 显示 background job · attached，peer 为 /tmp/cc-socks/24676.sock。更换独立 CLAUDE_CONFIG_DIR 后，同机客户端也无法仅靠短 job ID 找到该 job。官方文档把 attach 的短 ID 定义为后台 job ID，原生后台 job 的目录内容不属于稳定接口。[Agent view 文档](https://code.claude.com/docs/en/agent-view)。

**自定义远程端点尚无可用的等价命令。** 本机版本的 --sdk-url 指向 localhost 测试 WebSocket 时立即拒绝，说明该入口保留给连接 Anthropic 后端的 Remote Control worker。cc:// 与 cc+unix:// 位置参数在本次安装中被当成普通用户输入，未连接测试端点；测试转发器阻止了这些探测触发模型生成。没有尝试绕过端点限制，也没有据此断言所有未来版本或其他官方入口都不能实现远程接入。证据见 [terminal-endpoints.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/terminal-endpoints.json>) 和 [sdk-url.txt](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/sdk-url.txt>)。

Team Cross 当前 Codex 客户端命令由 [process.go](</Users/wsy/CascadeProjects/Team Cross/internal/nativecodex/process.go:306>) 生成，形式为 codex resume ID --remote ENDPOINT。要对齐这个体验，需要证明 Claude 原生客户端能够经 Team Cross 连接 A 上唯一的 worker，且控制端也能处理这个 worker 的发送、停止、审批和状态。分别具备 stdio SDK 与本机后台 TUI 并未完成这个组合。

## 审批语义与后续验收

用户此前对现有 Codex 审批的理解正确：B 持有输入权时由 B 回答审批，Team Cross 检查归属后直接转发到 A 上的 Codex 运行时，不需要 A 再批准一次。现有 [rpc.go](</Users/wsy/CascadeProjects/Team Cross/internal/collab/rpc.go:321>) 的 Respond 检查 caller、writer 和待处理请求，然后调用运行时 Reply。

Claude 的外部审批回调和原生 TUI 审批都已成功，审批本身不再是未知能力。接回输入后的待审批交接仍需要 Team Cross 的实际路由验收：旧输入者不可回应，新输入者可以继续处理同一未决请求。本次 interrupt 后旧 allow 无效只证明取消行为，不能替代“保留同一审批并交接输入权”的验证。

下一步适合做一个范围有限的接入原型：用官方 SDK 创建持久化 fork，以 A 上的一个原生后台 worker 执行，让 Team Cross 控制端和 B 的原生 TUI 通过同一受控入口访问它。验收标准包括同一 worker/会话、B 输入并审批、A 收回输入后 B 的新写入和旧审批回答被拒绝、A 继续未决请求，以及断线后不自动重放写入。该原型尚未在本次实验中实现。

## 实测范围

- 本次为同一台 Mac 的组件实验，未证明两台 Mac、LAN/Tailnet 共享或生产接入。
- SDK 和 TUI 使用独立测试配置、专用会话和临时 Git 仓库。只在测试目录创建标记文件；没有修改用户 Claude 配置或 Team Cross 产品代码。
- 本次明确指定模型，未验收来源模型与推理参数的完整继承/持久化、所有 slash command、MCP/插件和 Computer Use。当前 Team Cross 原生 Codex启动配置显式关闭 browser_use 与 computer_use，这两项不应被当作已经验证的现有基线。
- 此前 Gemini 路由最小 API 请求成功，但实际 CLI 请求 429；Opus 的实际配置路由 claude-opus-4-6-thinking 的 CLI 请求也 429。字面 claude-opus-5 返回 unknown provider。换用 Luna 后完成了上述真实流程；未把前两个路由的问题定性为 CLI 不兼容。
- 原生 TUI 脚本早期遇到测试粘贴提交顺序和 PTY 关闭竞争问题，修正脚本后完整重跑。最终 luna-native-summary.json 的 error 为 null，审批文件、助手回复持久化、脱离与重连均通过。
- 所有本次启动的测试客户端、SDK CLI、后台 worker 和隔离 supervisor 已关闭；按已知 PID 与测试 cwd 核对无残留。没有按程序名终止用户的 Claude/Codex。证据见 [process-cleanup-check.json](</Users/wsy/CascadeProjects/Team Cross/output/claude-code-integration-probe-2026-09-11/evidence/process-cleanup-check.json>)。

完整临时实验目录：/private/tmp/teamcross-claude-probe-20260911-p6yo8o8_。此目录包含脚本、专用配置与 fixture；本报告 evidence/ 只保留经过凭据检查的摘要和终端文本。当前实验脚本含专用目录、固定标记和仅首次运行可创建的文件断言，后续重复实验应建立新 fixture。
