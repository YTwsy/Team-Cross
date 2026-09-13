# Codex 动态 MCP 与运行时恢复兼容验证 · 2026-09-14

## 问题与修复

已安装的 Team Cross `0.1.3-dev.icon-20260914.4` 在恢复一个已有 Codex 协作时，原生 app-server 启动前退出，日志为 `invalid transport in mcp_servers.codex_app`。环境为 macOS 14.8.5 / arm64、ChatGPT Desktop `26.908.40834`、Codex CLI `0.154.0-alpha.6.2`。

当前 Codex 的 `mcp list --json` 会包含由插件动态提供 transport 的 `codex_app` 和 `cua_repl`。此前 Team Cross 先在普通个人配置下枚举全部 MCP 名称，再以 `features.plugins=false` 启动共享运行时，并为每个已枚举名称生成只有 `enabled=false` 的覆盖。插件关闭后，这两个动态条目没有可继承的 transport，导致整个 app-server 配置解析失败。分别对 `codex_app` 和 `cua_repl` 只传 `enabled=false`，均可独立复现相同错误；普通个人配置本身可以正常解析。

[原生 Codex 进程](../../../../internal/nativecodex/process.go) 现在让 MCP 枚举和 app-server 启动复用同一组运行时隔离参数。插件动态 MCP 在枚举前已按真实运行条件移除，仍存在的个人或项目 MCP 继续逐项禁用，协作批注 MCP 最后以完整 STDIO transport 注入。实现不按名称特判插件，也不修改个人 `~/.codex/config.toml`。[回归测试](../../../../internal/nativecodex/process_test.go) 模拟未隔离时出现 `codex_app` / `cua_repl`、隔离后只保留稳定个人 MCP 的行为。

## 工程检查

以下检查在 `codex/fix/runtime-mcp-transport` 的独立 worktree 中通过：

- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall`
- Node `24.18.0`、pnpm `11.19.0` 下的 Web check、49 项测试和 production build
- `git diff --check`

本次没有修改 Web 源码；production build 后 `internal/webassets/dist` 没有差异。

使用当前真实 Codex CLI 按产品参数顺序执行配置探测：完整运行时隔离下的 MCP 枚举不再包含 `codex_app` 或 `cua_repl`；逐项禁用剩余 MCP 并加入完整的测试批注 STDIO 后，配置继续解析成功。

## 真实 Codex 恢复

使用 [批注验收脚本](../../../../scripts/verify-annotations.py) 和全新的 `/private/tmp/teamcross-runtime-mcp-fix-dcda482/fixture`，运行单机两个真实 Core 与直接 Codex TUI。实际模型仅为 `gpt-5.6-luna`，Codex CLI 为 `0.154.0-alpha.6.2`。

结果为：

- 新协作 fork 和直接 TUI 正常启动，没有 `invalid transport`。
- 个人测试 MCP 保持禁用且没有握手信息；唯一激活的服务为 `teamcross_annotations`，只暴露 `read_annotations` 和 `reply_to_annotation`。
- 直接 TUI 读取原批注与人工回复，并通过受控审批保存 Codex 回复。
- 结束共享并释放运行时后，明确恢复保持同一个 `sessionId`；恢复后的 TUI 再次读到先前回复。
- 验收结果为 `annotation_acceptance_verified`，`inherited_mcp_disabled=true`、`native_read_and_reply=true`、`same_session_restore_read=true`。
- 两个测试 Core 和两次 TUI 均已退出，四个 PID 复查不存在；fixture 中没有配置 transport 错误。

## 能力边界

本次真实结果属于同一台 Mac 上的两个 Core，不证明两台 Mac LAN；没有验证专用 Desktop 窗口。没有恢复或修改用户原有协作，也没有替换已安装的 Team Cross App。现有用户协作应在包含本修复的新构建中另做一次不发送业务 prompt 的恢复复核，确认保存的 session 和目录保持不变。
