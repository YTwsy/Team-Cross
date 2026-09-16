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
