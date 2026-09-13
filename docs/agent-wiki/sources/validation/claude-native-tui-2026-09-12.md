# Claude 原生 TUI 验收（2026-09-12）

本记录对应 `Codex/feature/claude-native-tui` 的实验性实现。使用一台 macOS 14.8.5（arm64）、两个独立 Team Cross Core、真实 Claude Code `2.1.268` 和本机 CliProxyAPI 的 `gpt-5.6-luna`。未使用两台 Mac，也未将此结果推广到其他 Claude CLI 版本或账户路由。

## 真实执行结果

[验收脚本](../../../../scripts/verify-claude-native.py) 的完整通过轮次位于本机 `/private/tmp/teamcross-claude-feature-live-20260912-k`。本轮包含 10 次模型生成请求，guard 观察到的模型全部为 `gpt-5.6-luna`；源码没有写入固定模型。先前调试轮次不计入这 10 次，失败记录保留在各自测试目录。

| 检查 | 实际结果 |
| --- | --- |
| 新 fork 与创建幂等 | 原目录和 worktree 均取得新的原生会话 ID；重复同一创建请求返回同一个 fork |
| 来源与 Git 现场 | 原生来源 JSONL 哈希不变；原目录的 HEAD、分支、暂存内容和工作文件保留；worktree 从 HEAD 创建，不复制暂存、未暂存、未跟踪和忽略内容 |
| 首次输入前恢复 | fork JSONL 在创建返回前存在；worktree 尚无新输入时停止、恢复，再打开原生 TUI 仍显示来源历史，sessionId/jobId 不变 |
| B 原生 TUI | 经真实 TLS 成员入口连接 A 的一个 worker，渲染来源历史；B 客户端目录没有复制 provider 历史 JSONL |
| 审批交接 | B 收到 Bash 审批；A 收回后 B 的旧 Enter 未执行命令；A 重新 attach 同一 worker 的审批并批准，文件仅写入一次 |
| MCP 与 TUI 共用执行端 | 实际启动 STDIO MCP 的 `send_input`；原生 TUI 显示后续回复；重复 requestId 返回原 ACK，历史中只有一条对应用户输入 |
| 输入归属 | B 持有输入时 A 的发送被拒绝，拒绝文本未进入原生历史 |
| 原生中断 | 批准包含 `sleep 20` 的 Bash 命令，确认起始文件后按 Esc；保持 worker 运行超过 21 秒，结尾文件始终不存在，实时状态回到 idle |
| 推理强度恢复 | 在 TUI 用 `/effort medium` 并确认，无模型生成；下一次真实回复的历史确认 medium；结束再恢复后原生 job 启动状态与 TUI 均保留 medium |
| worktree 实际执行 | 在原生 TUI 中批准相对路径写入；证明文件仅存在于 worktree，原目录不存在 |
| Core 异常退出 | 精确终止测试 A Core PID 后重启，重新绑定存活的同一个 worker PID；新的原生 TUI 可显示历史，本机陈旧 socket 可按所有权恢复 |
| 结束与读取 | 结束共享后运行时释放，轻量历史仍可读取；正常恢复保留同一会话 |

审批出现时，这个 Claude 版本尚未将待审批的 tool_use 写入 JSONL。因此审批接力依据是相同 worker PID、同一待审批界面、批准前文件不存在以及批准后仅一个完成的 tool ID；不能将本次结果写成“前后匹配了持久化的待审批 ID”。原目录中 B 输入、A 审批、MCP 发送和中断全程使用同一 worker；正常停止后恢复允许新 PID，但会话 ID 保持不变。

补充的零生成回归位于 `/private/tmp/tcx-claude-checkpoint-rkf95bxf`，使用专用来源副本，对原目录和 worktree 各执行三次真实原生 TUI attach，中间两次停止与恢复，均显示来源内容并保持 ID。它验证了首次输入前的持久化，不依赖 Web 历史回退。

## 工程与页面检查

- 全部项目 Go 包的 test、vet 通过；相关 race 覆盖 collab、mcp、sharing、nativeclaude、nativecodex、service 和 cliinstall。新增历史读取修正后再次通过 nativeclaude/collab 的 test/race 与项目 vet。
- 当前工作目录存在此前留下的 `bin/go-mod` 模块缓存；直接 `go test ./...` 会把该缓存纳入遍历并报重复模块问题。本次使用等价的项目包入口 `./cmd/... ./internal/...`，保留已有缓存。Go 为 `go1.25.3 darwin/arm64`，测试缓存放在任务专用 `/private/tmp` 路径。
- Web check、32 项测试和 production build 通过；嵌入式 `internal/webassets/dist` 已更新。
- 实际浏览器检查首页、Claude 来源选择、原目录/worktree 预览、详情、原生启动命令、加入及设置页；详情覆盖 1440、1024、768 CSS 像素的浅色/深色组合，实际视口与 document scrollWidth 无水平溢出。截图经过查看，长路径换行、主要操作及窄屏布局正常。
- 页面恢复与准备启动命令不会生成模型请求。隔离环境用 `/usr/bin/false` 代替 Codex，验证其缺失不阻止 Claude 来源与原生入口；Codex 来源/配置的错误提示属于预期测试状态。
- 页面检查后的文案与历史展示修正已重新构建并复核：不把 Claude 本地命令的 XML 标记显示为用户对话，不把中断记录判为新的待完成输入。

## 本轮修复与边界

直接使用 `--resume <source> --fork-session --bg` 会延迟保存新历史，首次输入前反复恢复可导致原生 TUI 为空。当前先在 Go 中按官方 Agent SDK `0.3.268` 的离线 fork 规则写入原生 JSONL，生成新消息 UUID、重建父链和来源关联，再启动原生 worker；不引入第二个 SDK 执行进程。公开的 [会话语义](https://code.claude.com/docs/en/agent-sdk/sessions) 说明 fork 与 resume 的区别，实际格式兼容性以本次固定版本测试为准。

当前不覆盖 Claude Desktop、Computer Use、外部 MCP/插件、OAuth/Keychain、OS 沙箱等价性、任意 CLI 版本和实际双 Mac LAN。控制 API 的补充、中断、审批及模型设置返回 `native_client_required`，使用当前输入者的原生 TUI 完成；结构化事件流不是 Codex app-server 全量事件。Team Cross 批注由 Web/辅助 MCP 读取，当前 Claude worker 没有加载 Team Cross MCP。

完整运行时、权限与传输约定见 [Claude 接入契约](../decisions/claude-native-tui.md)。[摘要与截图](../../../../output/claude-native-tui-2026-09-12/README.md) 供复核，不包含真实邀请或 API 凭据。[进程与打开文件核对](../../../../output/claude-native-tui-2026-09-12/cleanup-audit.json) 未发现剩余匹配项。测试启动的 TUI、Core、worker、supervisor 和浏览器页面按专用 PID/目录关闭；原目录、worktree、测试历史与证据保留。
