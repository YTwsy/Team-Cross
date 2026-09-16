# 个人 Agent 与 CLI 协作入口验证

日期：2026-09-16。分支：`codex/feature/agent-cli-collaboration`，起点 `0c744cf`。环境：本机 macOS arm64，Go 1.27.1；使用任务专用 Go 构建缓存。

## 第一批：输入接力

- 新增个人 MCP 输入申请、取消、交接、接回和交还，以及 CLI 协作列表/详情与输入命令。
- HTTP/Core 统一拒绝未加入的交接、过期 epoch、错误角色、忙碌交出/交还；接回不自动中断轮次。
- `TestMCPInputRelayUsesCoreOwnershipAndEpoch` 使用模拟 Provider 与两个真实本机 Core API/TLS 成员连接验证完整输入接力，检查管理操作不发送模型输入、旧输入者不能发送、结束共享后访问被拒绝。
- CLI 回归检查本机实例认证、命令到 API 的映射、显式 epoch、JSON 输出与邀请脱敏；没有启动用户客户端。
- `go test ./...`、`go vet ./...`、`go test -race ./cmd/teamcross ./internal/mcp ./internal/collab` 通过。
- Web check、53 项测试与 production build 通过；嵌入资源没有变化。
- Markdown 相对链接和 `git diff --check` 通过。

以上证据未运行真实模型、原生 TUI/Desktop 窗口或两台 Mac 网络，不替代这些入口的实际验收。测试由 Go fixture 精确关闭自身 Core、TLS 服务与模拟运行时。

## 第二批：创建到结束

- 个人 MCP 新增来源枚举、预览、创建、邀请、邀请预览、加入、直接客户端打开、结束、离开与恢复；CLI 复用同一适配，并增加创建后邀请的 `share` 组合命令。
- `TestMCPManagementCreatesInvitesJoinsAndResumesSameFork` 用模拟 Provider 和两个本机 Core/TLS 连接覆盖原目录与 worktree、创建去重、模式不可变、邀请脱敏、预览不加入、重复加入、角色限制、输入权与客户端计划、已消费邀请、离开及同 fork 恢复；不发送业务 prompt。
- CLI 回归模拟邀请失败，确认输出已创建协作，单独重试邀请不再次创建。
- `go test ./...`、`go vet ./...`、相关 CLI/MCP/collab race test、文档相对链接与 `git diff --check` 通过。Web 源码与嵌入资源未改变，沿用本轮第一批已执行的 53 项回归。
- 本批自动化只验证客户端启动计划，没有声称原生窗口已打开。真实 Provider、个人 MCP 实际调用和当前 Session 行为见后续实测记录。

## 第三批：当前 Session 分享

- 个人 MCP 新增当前来源核对、预览、分享登记、状态查询与等待期间取消。CLI 新增 `share-status/cancel-share`。分享固定调用者本轮，工具立即返回；Core 观察本轮完成后才 fork 和邀请，避免工具等待自身结束。
- Codex 使用实际调用元数据的 Session/Turn ID；Claude 使用原生 Session 环境及 toolUseId，再核对当前 transcript 主链。元数据缺失、冲突、过时或无法核对时拒绝，不猜最近会话。
- 实测发现 Codex 独立 reader 会把外部客户端仍在运行的轮次重建为 interrupted，已改为结合原生 rollout 完成事件核对；Claude 工具分发可能早于 transcript 异步落盘，已增加最多 3 秒身份证据等待。
- 自动化回归覆盖同一请求只创建一次、来源轮次与 Git 漂移、本轮中断、取消、Core 停止与异常重启不重放、部分创建结果保留、MCP 身份不可由工具参数替换。等待请求计入 Core 的活动保护。

真实验收入口为 [verify-agent-cli.py](../../../../scripts/verify-agent-cli.py)，使用独立会话、Git 仓库、Provider home 和 Core 数据目录，模型仅 `gpt-5.6-luna`：

| 验证 | Codex | Claude Code |
| --- | --- | --- |
| 实际版本 | `codex-cli 0.154.0-alpha.6.2` | `2.1.270` |
| 个人 Agent 调用 MCP 核对自身 Session、预览并登记 | 通过 | 通过 |
| 等待来源轮次完成，新 fork 含最后 assistant 答复 | 通过 | 通过；首次业务输入落盘后核对 |
| CLI 邀请预览、`join --no-open`，MCP 重复加入复用记录 | 通过 | 通过 |
| MCP 批注与回复、非输入者发送被拒绝 | 通过 | 通过 |
| 申请输入，CLI 交接、交还、接回 | 通过 | 通过 |
| CLI 打开计划及原生 TUI 通过 PTY 连接 A 的同一 fork | 通过 | 通过 |
| MCP 发送 Luna 任务、同 requestId 重试 | 通过 | 通过 |
| 离开、结束、恢复同一 sessionId 与目录 | 通过 | 通过 |

最终成功 fixture 为 `/private/tmp/teamcross-agent-cli-codex-live-03` 和 `/private/tmp/teamcross-agent-cli-claude-live-03`；证据在各目录 `evidence/summary.json` 与个人客户端输出。早期失败 fixture 保留，用于区分发现的适配问题与最后通过结果。Claude 通过本机只允许 Luna 的代理 guard 验证，观测到的模型集合只有 Luna。

这次真实测试使用原目录、受限模式、同一台 Mac 的两个 Core 和 loopback TLS，原生 TUI 在测试 PTY 中运行。没有执行新的 Desktop 图形窗口、Terminal.app 新窗口、两台 Mac、Tailcat、跨网络或信任模式实测；worktree 与固定模式入口由第二批 Core 回归覆盖。不会把个人 CLI 的调用元数据结论自动推广为所有 Desktop/未来版本；不支持的客户端仍可明确选择来源。

脚本关闭自身 TUI、Core、app-server 和 Claude 测试 daemon；随后按专用 fixture 路径核对进程，未发现遗留。本次未修改用户既有会话、配置或工作目录，测试资料保留。

最终工程检查：`go test ./...`、`go vet ./...`、`go test -race ./cmd/teamcross ./internal/collab ./internal/mcp ./internal/nativecodex` 通过；Python 验收脚本语法、变更文档相对链接与 `git diff --check` 通过。前端和嵌入资源没有修改，沿用本轮第一批执行通过的 Web check、53 项测试与 build。最后加入的请求超时与 Core 停止保护由 Go/race 回归覆盖，不把此前真实客户端运行描述为这些异常分支的实测。
