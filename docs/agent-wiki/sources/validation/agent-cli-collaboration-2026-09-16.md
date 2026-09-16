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
