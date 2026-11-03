# 同目录多会话与原生工作目录

2026-10-23，分支 `codex/native-session-workspace`，基线 `73e503c` 加本次工作树修改。环境：单 Mac、macOS 14.8.5 arm64、Codex CLI `0.163.0-alpha.11.1`、Go `1.27.1`；真实模型仅使用 `gpt-5.6-luna`。

## 最终范围

- 移除 Core 的目录独占；不同原生会话可以使用同一原目录或 worktree，输入归属与原生会话写入锁仍独立有效。
- 创建时的目录选择和新 worktree 流程保持原样。继续输入不把旧目录或远端客户端本机路径覆盖到原生会话。
- Codex `executionCwd` 跟随同一 `threadId` 最近确认的 `thread.cwd`。恢复先在已保存的原生配置目录读取元数据，再用最新 cwd 启动；保留创建时的目录归属。
- 复用现有菜单、文件和改动视图；目录更新触发现有内容刷新。命令级 `cd/workdir` 不改变会话目录显示，失败或过期读取不覆盖最近确认值。
- Claude 移除 Team Cross 层的目录独占，仍遵循原生 job 的目录与恢复契约；没有增加 transcript 推断或动态迁移功能。

长期规则见[目录与生命周期](../sources/decisions/workspace-and-lifecycle.md)、[协作模式](../sources/decisions/runtime-modes.md)和[协议](../sources/protocol.md)。

## 工程验证

| 检查 | 实际结果 |
| --- | --- |
| `go test ./...` | 通过，含最终按保存 ProviderHome 恢复的改动 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/collab ./internal/nativecodex ./internal/nativeclaude ./internal/mcp ./internal/sharing ./internal/workspace` | 通过，含最终目录刷新及恢复逻辑 |
| Web check / test / build | 1157 条翻译检查、209 项测试通过；两套嵌入资源重建后无文件差异 |
| `go build -o bin/teamcross ./cmd/teamcross` | 通过 |

[目录回归](../../../internal/collab/native_workspace_test.go)覆盖继续输入不回退、文件/改动跟随、命令目录不冒充会话目录、完成通知后的刷新、重启持久化、默认 CODEX_HOME 变化，以及空路径、相对路径、错误会话、读取失败、旧实例和关闭后的响应。[多会话测试](../../../internal/collab/executions_test.go)覆盖同目录双实例创建、暂停、恢复和独立输入。[文件读取测试](../../../internal/workspace/workspace_test.go)覆盖 cwd 符号链接及文件越界拒绝。

## 真实原生验证

`TestLiveCodex` 使用全新隔离 home/仓库和测试 CLI，来源、原目录 fork、worktree fork 与同目录新增 fork 均为测试专用。首次运行被旧测试中“结束会话后成员失去阅读”的断言中断；按现有空间契约改为继续可读、实例已释放且输入接回后，在另一个全新 fixture 重跑通过，耗时 155.82 秒。

本次确认：

1. 原目录保留未提交现场，新 worktree 从选定 HEAD 检出；模型在两种目录写入证明文件。
2. 两个不同原生会话可同时在同一原目录保持运行。
3. 通过原生 `turn/start` 设置测试会话的新 cwd，模型在新 worktree 写入证明文件；完成后现有目录字段同步。
4. 经 Team Cross 继续输入时附带一个无效的远端客户端 cwd，模型仍在已确认的新目录写入第二个证明文件。
5. 暂停、释放并恢复同一原生会话后保留新目录；相邻会话继续使用原目录。文件上下文读到新目录中的证明内容。

以上目录变化由原生 API 发起，证明 Team Cross 能跟随原生状态；不把 Agent 仅在别处执行命令描述为原生 thread 自动迁移。

`scripts/verify-agent-cli.py --provider codex` 通过，范围为单 Mac、三个隔离 Core、两个会话、loopback TLS。实际摘要如下：

```text
currentTurnIncluded: true
allMembersReadAllSessions: true
independentConcurrentExecutionAndDedupe: true
cliJoinNoBrowser: true
mcpAnnotationsAndReply: true
inputRelay: true
directTUI: true
endOnePreservesSpaceAndSibling: true
sameForkResume: true
```

真实模型/TUI 检查后补充了内容刷新序号和按已保存 ProviderHome 读取的保护；最终代码完成全量 Go、相关 race，并用该专用 fixture 的 `TestLiveNativeHandoff` 复验两个会话经成员网关、主机网关恢复和释放，通过，未发送额外模型输入。后一个旧用例最初误用 Go 测试程序启动必要的批注 MCP，先超时、随后原生返回 MCP 握手失败；指定 fixture 已构建的真实 CLI 后通过。测试仍使用原来的 30 秒上下文上限。

## 证据与边界

上面的执行结果与关键回执摘要随本记录保存。原始日志和 fixture 暂存在本机 `/private/tmp/teamcross-native-workspace-*`，包括 `live-b.log`、`cli-20261023/evidence/summary.json`、`handoff-fixed.log`、`go-test-final.log` 与 `race-final.log`；这些临时原件没有作为长期仓库附件保存。

本次未改变前端布局，未新增浏览器截图验收；也未运行真实 Claude、个人/专用 Desktop UI、跨 Mac、Tailcat 或 DMG 安装验收。现有已运行的用户 Core 和协作会话未被替换或操作。验证后按 fixture 路径核对并关闭测试创建的 Core、原生进程和 TUI，不按程序名批量结束进程。
