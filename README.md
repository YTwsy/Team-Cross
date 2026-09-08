# Team Cross

**从已有的 Codex 会话分出协作，让同事用自己的客户端一起继续。**

Team Cross 是 macOS 上的本地协作工具。发起者选择一个来源会话，创建新的原生 Codex fork，再向同事发送临时局域网邀请。会话、模型调用与代码执行保留在发起者的 Mac；参与者可以使用本机 TUI、专用 Desktop，或让自己的 Codex 通过工具辅助协作。

本分支 `codex/session-collaboration` 是独立实验。新界面与新数据目录不读取旧版 Thread/Round/Evidence 数据，也不依赖 Node Agent Bridge。

## 启动

需要 macOS、Go 1.25.3+、Node 24+、pnpm，以及已登录的 Codex。当前验证版本为 `codex-cli 0.153.1`、Codex Desktop `26.901.31953`。实验运行时暂时固定为 `gpt-5.6-luna`；客户端显示的模型选择不会改变共享运行时的模型。

```sh
pnpm install
make build
./bin/teamcross serve --repo /path/to/your/repository
```

打开 `http://127.0.0.1:43210`。浏览器只负责协作管理、上下文查看与批注；完整 Agent 对话在 Codex 客户端中进行。

默认实验数据目录是 `~/Library/Application Support/Team Cross Next`。可用 `--data-dir` 指定另一个目录，`--listen 127.0.0.1:PORT` 改变本机端口，`--no-open` 禁止自动打开浏览器。`--codex-bin`、`--desktop-app` 或界面的设置页可以指定客户端路径。

## 发起协作

1. 在首页选择“发起协作”，搜索并选择一个已有已完成对话的 Codex 会话。
2. 选择执行目录，查看起点，按需修改自动生成的名称，然后创建。

| 目录模式 | 创建时发生什么 | 后续代码在哪里执行 |
| --- | --- | --- |
| 使用原目录 | 保留原 Git 分支、暂存区和全部现有文件，只创建新会话 fork | 来源会话的原目录 |
| 创建新 worktree | 从确认的 `HEAD` 检出独立目录，创建 `codex/collab-<短 ID>` 分支；不复制暂存、未暂存、未跟踪或忽略的内容 | 新 worktree 中对应的来源子目录 |

创建不发送业务指令。来源会话和 Git 起点发生变化时，需要重新查看起点。当前支持普通 Git 仓库；含 submodule 的仓库暂不支持。

## 邀请与加入

发起者在协作详情中选择“邀请同事”，复制邀请。接收者在自己的 Mac 启动 Team Cross，选择“加入协作”，粘贴邀请并确认主机与协作信息。也可运行：

```sh
./bin/teamcross join 'tcx2.…'
```

邀请使用临时 TLS 证书和主机指纹，当前有效期为一小时。双方需在同一局域网；发起者需保持 Team Cross 运行。邀请过期、结束共享或发起者重启服务后，需要重新分享。

加入后可以选择两种参与方式，之后也能同时打开另一种入口：

- **直接操作：** 使用本机 Codex TUI，或专用于该协作的独立 Codex Desktop。客户端连接同一个共享 fork，实际执行在发起者主机。同一协作只保留一个直接客户端，切换前关闭原直接客户端。
- **用自己的 Codex 辅助：** 保持自己的普通 TUI/Desktop 会话，通过 Team Cross 工具选择目标、读取历史、查看文件和改动、参与输入或添加批注。这些会话拥有各自的本地上下文。

输入者由发起者交接或接回；接收者也可以交还输入。直接客户端和辅助工具的写入统一检查输入归属，读取可以并行。当前轮运行中可通过 Codex 或 MCP 补充、中断，审批在原生客户端或辅助工具中回应。

专用 Desktop 第一次打开时可能需要完成登录或跳过引导，之后从左侧打开协作会话。它与原有 Desktop 使用独立应用数据目录；账户登录与客户端偏好在使用者本机处理，共享会话的模型调用仍由发起者运行时承担。此入口面向协作会话的历史、输入、审批与代码操作，Desktop 的其他全局功能不在首轮兼容承诺中。

## 一次性接入 MCP

在“设置与连接”中点击“接入本机 Codex”，或使用该页面给出的准确命令：

```sh
codex mcp add teamcross -- /absolute/path/to/teamcross mcp
```

若使用自定义数据目录，在 `mcp` 后追加 `--data-dir /absolute/path`。TUI 和 Desktop 共用 Codex 配置；已有会话需重新加载工具。

可以告诉自己的 Codex：“使用 Team Cross 列出协作，查看这次协作的上下文和当前状态。”工具支持：

| 工具 | 用途 |
| --- | --- |
| `list_collaborations` | 定位本机发起或已加入的协作 |
| `get_collaboration` | 查看执行主机、目录、输入者与运行状态 |
| `read_context` | 读取历史、事件、相对路径文件、当前 Git 改动 |
| `send_input` | 开始一轮或对当前轮补充输入 |
| `interrupt_turn` | 中断指定当前轮 |
| `respond_to_request` | 回应原生审批或用户输入请求 |
| `add_annotation` | 保存批注，不自动注入模型 |

发送成功和执行完成分别表示不同状态。断线或响应超时时先查询实际结果，不自动重复写入。工具不会自动为输入添加参与者身份。

历史默认返回最近 8 轮；工具可将 `nextCursor` 传入 `read_context` 的 `cursor` 读取更早内容。WebGUI 展示最近一页，完整对话在 Codex 中查看。

## 结束与恢复

“结束共享”关闭同事的原生连接和工具访问，保留会话、目录和代码，不要求提交、导出或填写结论。结束共享后可在本机继续；重新分享生成新邀请。

服务重启后，发起者从协作详情“恢复运行时”，继续已保存的同一个 fork，不再 fork 或创建 worktree。接收者本地保存加入记录；只要原邀请仍有效且发起者仍共享，就可以重新连接。Team Cross 不自动删除任何原目录或协作 worktree。

## 开发与验证

```sh
# 两个终端：前端 HMR 与 Go 服务
pnpm --filter @teamcross/web dev
./bin/teamcross serve --dev-web http://127.0.0.1:5173

# 工程门槛
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

修改 Web 后必须更新 `internal/webassets/dist` 并重新构建 Go 二进制。真实模型测试默认跳过，显式使用独立目录启动；全部固定为 `gpt-5.6-luna`：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

实际验证结果与未完成项目见 [验收记录](docs/validation.md)。原生 Desktop 指定 WebSocket 入口属于当前本机版本的实验接口，不等同于公开稳定的远程产品合同。

工程导航：[架构](docs/architecture.md) · [接口](docs/protocol.md) · [产品流程](docs/product-flows.md)。
