# 接收会话统一配对：本轮实现与验证

- 日期：2026-10-03，macOS，本机单设备。
- 基线：`9abee024a415c1ee115224cf1d5776b2cdce165a`，`docs: consolidate wiki sources and make reading task driven`。
- 开发分支：`codex/session-pairing`；验证对象为该基线加本次提交中的实现，运行验证时包含未提交改动。
- 交付范围：统一配对界面、固定引用请求、接收/完成回执、本机共享原生投递及实验性个人 Claude Channel 适配。归档表示这一轮收尾，不代表所有宿主验收通过。
- 长期约定见[配对契约](../../../sources/decisions/agent-pairing.md)，路由字段见[协议](../../../sources/protocol.md#接收会话配对与请求)。

## 工程检查

以下检查均通过：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall
pnpm --dir packages/web check
pnpm --dir packages/web test
pnpm --dir packages/web build
go build -o bin/teamcross ./cmd/teamcross
python3 -m py_compile scripts/verify-agent-pairing.py scripts/verify-agent-channel.py
git diff --check
```

Go 1.27.1；Node 24.18.0。前端 11 个文件、130 项测试通过，778 条翻译检查通过；Vite 直接更新 `internal/webassets/dist`。保留已有的主包超过 500 kB 提示，构建成功。补充 Channel 队列容量保护后，重新通过完整 Go test/vet 与 collab/mcp race；最后的英文文案调整重新通过前端 check/test/build。

后端覆盖精确会话绑定、配对码过期与隔离、Channel 随机挑战、原生输入权、忙碌与审批、引用范围、移除配对、并发同 ID 去重、早到回执不回退、分页读取、Core 重启不重放及队列容量。STDIO 测试验证通知序列化和 EOF 取消。前端测试包含未完成配对、目标选择、分析/回复意图、丢失 POST 响应后只查询原请求。

## 真实 Codex TUI

[结构化报告](native-codex.json) 来自 `scripts/verify-agent-pairing.py`。客户端 `codex-cli 0.159.0-alpha.12.1`，模型仅 `gpt-5.6-luna`。使用专用 Git 仓库、独立 `CODEX_HOME` 和 Core；认证文件只在测试 home 引用，未修改用户配置。

实际模型在新共享 fork 内调用配对工具，精确绑定该 Session；用户请求通过原生通道投递后调用 `read_agent_request`、`read_annotations` 和 `finish_agent_request`。完成摘要为 `PAIRING_ANALYSIS_DONE`。核对了同请求 ID 返回原记录、分析模式未回复原批注、测试文件未变化和移除配对。仅接受该测试会话中三个已知 Team Cross 工具的原生审批。

原始本地日志保留在 `/private/tmp/teamcross-pairing-native-20261003-04/evidence/`；可移植的结构化结果保存在本目录。该结果不能推广为专用 Desktop 或两台 Mac 验证。

## 真实 Claude Channel 探测

[结构化报告](claude-channel.json) 记录的是未通过的真实探测。Claude Code 2.1.270 明确提示 `Channels are not currently available`，开发 Channel 参数被忽略；测试所用现有本机模型代理也拒绝连接，未完成模型请求。专用 guard 只允许 `gpt-5.6-luna`。

首次运行最终表现为等待配对超时，之后脚本增加了对宿主不可用提示的提前识别；没有重新假定成功，没有伪造通知挑战。协议测试通过，个人 Claude 的真实主动接收仍未验收。原始本地日志保留在 `/private/tmp/teamcross-pairing-channel-20261003-01/evidence/`。

## 浏览器

使用独立 Chromium 会话 `teamcross-pairing` 和 `TestLibraryBrowserFixture`。HTTP、权限校验与存储使用真实 Core，原生 runtime 为测试替身；浏览器回执通过该 fixture 的运行时凭据模拟，不能当作模型验证。真实模型证据以上一节为准。

[浏览器检查](browser-checks.json) 记录窗口、主题、语言和交互。检查了生成配对提示、分钟精度到期时间、等待状态、选择目标、明确分析意图、发送后锁定、提交与完成分开显示、最近请求、忙碌原因、键盘焦点与 Escape 恢复焦点。1024 与 768 宽度无横向溢出；主验证页面 console 为 0 errors / 0 warnings。初始 fixture 关闭时的旧页轮询错误不计为新页面结果。

- [首次配对，浅色](pairing-1440-light.png)
- [交给 Agent，浅色](send-to-agent-1440-light.png)
- [768 宽度，深色](send-to-agent-768-dark.png)
- [768 宽度，英文](send-to-agent-768-en-light.png)

## 清理与后续边界

已关闭本轮专用 Codex/Claude TUI、Core、Channel guard、专用 Claude daemon 与测试浏览器；浏览器 fixture 通过专用 `finish` 文件结束。清理只针对已核对的本轮 PID、父子关系与测试目录，未按进程名称关闭用户客户端。测试报告和工作树保留。

个人 Codex 的接收适配、跨 Core 配对、自动关注讨论，以及 ChatGPT Work Cloud/dots 的 MCP 2.0 发现、订阅和签名 webhook 均未实现。下一轮需要补齐对应宿主的实际接收通道并进行真实回执验证；不能因本轮本机路径通过而标记云端 MCP Events 已完成。
