# Claude 个人 CLI/TUI 历史验收（2026-09-14）

## 范围与环境

本记录验证 Team Cross 创建的 Claude 协作 fork 在首次业务输入持久化后进入邀请者个人 Claude Code CLI/TUI 的 `/resume` 历史，同时不把个人设置、插件、认证原件或已有历史作为写入目标，也不向协作 worker 挂载其他个人历史。对象为从 `dcda482` 基线开发的 `v0.1.4` Claude 个人历史实现；项目处于 prerelease，不验证旧 `collaborations/<id>/claude-home` 数据迁移。

环境为一台 Apple Silicon Mac、macOS 14.8.5（23J423）、Go 1.25.3、Node 24.18.0、Claude Code `2.1.270`。真实流程使用两个独立 Team Cross Core 和本机 CliProxyAPI 的 `gpt-5.6-luna`；这不是两台 Mac LAN、Claude Desktop、Web 或 Cloud 历史验收。

## 设计落点

- 创建使用 Claude 原生 `--resume <sourceId> --fork-session --bg`，不发送业务 prompt，也不由 Team Cross 解析、改写或生成 Provider JSONL。
- 每个 `collaborations/<id>/claude-runtime` 是独立 `CLAUDE_CONFIG_DIR`。它只获得用户选择的来源 transcript 逐字节快照，并保存本次 fork、受限 settings、认证快照、daemon/job 与所有权标记；不链接完整个人 `projects`。
- fork transcript 首次落盘后，只为这个新 session 在个人 `projects` 发布同一文件入口。同磁盘使用 hard link；因此 runtime 与个人入口具有相同 device/inode，任一侧续写都是同一份 transcript。跨磁盘的单文件 symlink fallback 本轮未做真实外置卷验收。
- 个人 `settings.json`、`.claude.json`、插件目录、认证原件、来源 transcript 和额外历史都不是协作运行时的写入目标。共享运行中设置 `/effort` 写回独立 runtime。
- 零输入 fork 不要求提前生成 JSONL；若 Claude 尚未持久化，个人 `/resume` 暂时不可见，释放后也不保证可恢复。这不算创建失败。

## 真实执行结果

[验收脚本](../../../../scripts/verify-claude-native.py) 的完整通过轮次位于 `/private/tmp/teamcross-claude-personal-history-20260914d`，摘要为 `evidence/summary.json`，原生 picker 输出为 `evidence/personal-history-picker.txt`。本轮有 10 次真实模型生成请求，guard 观察到的模型全部为 `gpt-5.6-luna`；创建 fork、读取、设置 `/effort`、恢复和打开 picker 均未引入额外模型请求。

| 检查 | 实际结果 |
| --- | --- |
| 原生 fork 与创建幂等 | 原目录和 worktree 都得到不同于来源的新 session ID；相同 requestId 重复创建不产生第二个 fork |
| runtime 历史隔离 | 两个 runtime 都是普通目录，只包含所选来源快照和各自新 fork；额外个人历史哨兵未进入 runtime |
| 个人历史发布 | 原目录与 worktree fork 首次输入后各只新增一个个人 history 文件；原目录 fork 的 runtime/个人路径 device、inode 相同，权限为 `0600`、link count 为 2 |
| `/resume` picker | 释放运行时后，用个人测试 home 在来源仓库运行真实 `claude --resume`；picker 同时显示 `Claude 实测 · existing` 与 `Claude 实测 · worktree` |
| 个人配置与旧历史 | Team Cross 流程结束前，个人 `settings.json`、`.claude.json`、插件哨兵、来源 JSONL 和额外历史 JSONL 的哈希/内容不变；打开 picker 后 Claude 自己写入启动指标不算 Team Cross 写入 |
| Git 现场 | 原目录 HEAD、分支、暂存内容和工作文件不变；worktree 从 HEAD 创建，不复制暂存、未暂存、未跟踪或忽略内容 |
| B 原生 TUI | 通过真实 TLS 成员入口 attach A 的同一个 worker；B 的客户端目录没有 Provider 历史 JSONL |
| 审批交接 | B 收到 Bash 审批；A 收回后 B 的旧 Enter 不执行；A attach 同一 worker 后批准，命令只执行一次 |
| MCP、去重与输入归属 | 实际 STDIO MCP `send_input` 成功；相同 requestId 返回同一结果且历史只有一条输入；非输入者文本未进入历史 |
| 中断与推理强度 | 原生 Esc 阻止 `sleep 20` 命令的末尾写入；`/effort medium` 不触发生成，下一轮和释放后恢复都保持 medium |
| 独立生命周期 | 结束原目录协作只停止其精确 job 与独立 supervisor，worktree job 继续；随后原目录按同一 session 恢复 |
| Core 异常恢复 | 精确终止测试 A Core 后重新启动，重新绑定原先存活的同一 worker PID，原生 TUI 可再次 attach |
| worktree 实际执行 | 相对路径证明文件只写入所选 worktree，不出现在来源仓库 |

本轮审批出现时 `pending_tool_ids_persisted` 为 false，因此交接结论依据同一 worker PID、同一待审批界面、批准前文件不存在、旧 B 输入失效及批准后只有一个完成 tool ID；不声称匹配了尚未落盘的待审批 ID。

## 工程检查

- `go test ./...` 与 `go vet ./...` 通过；Go cache 使用任务专用 `/private/tmp/teamcross-go-cache`。
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall` 通过。
- Web check、49 项测试与 production build 通过。本功能没有修改前端源码，`internal/webassets/dist` 无需更新。
- Python 验收脚本通过 `py_compile`；文档与源码通过 `git diff --check`。
- 验收结束后按专用 fixture 与二进制路径检查进程，只看到检查命令自身，没有遗留匹配的 Core、TUI、worker 或 supervisor。

## 调试记录与边界

第一版尝试直接把个人 home 用作协作 `CLAUDE_CONFIG_DIR`。`/effort medium` 真实操作会改写个人 `settings.json`，因此该方案被否决；最终方案把设置、daemon/job 和 transcript 写入独立 runtime，再只发布新 fork 的单文件入口。另一次最终方案验收已在 picker 中显示两个 fork，但首次断言保留了空格，而终端规范化文本移除了空格；修正测试匹配后以全新 `20260914d` fixture 完整通过。

真实 fixture 使用路由环境和占位 token，没有覆盖文件型登录、OAuth 或 Keychain。文件型 `.credentials.json` 的只读复制与个人原件不变由 Go 回归覆盖；OAuth / Keychain 仍需单独真实验收。跨磁盘 symlink fallback、共享期间用户从普通个人 `/resume` 并行打开同一 session、两台 Mac LAN、Claude Desktop/Web/Cloud、Computer Use、个人插件或外部 MCP 均不在本结论内。

完整运行时和能力边界见 [Claude 接入契约](../decisions/claude-native-tui.md)。
