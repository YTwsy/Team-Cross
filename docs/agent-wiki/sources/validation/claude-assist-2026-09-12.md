# 个人 Claude Code 辅助 MCP 验收（2026-09-12）

对应 `Codex/feature/claude-native-tui`。一台 macOS 14.8.5（arm64）、真实 Claude Code `2.1.268`、两个隔离 Core，A / B 分别使用独立测试目录和 Claude 配置。真实请求只允许本机 CliProxyAPI 的 `gpt-5.6-luna`；个人配置安装没有写入用户日常 `~/.claude.json` 或 `~/.claude/settings.json`。

完整通过轮次为本机 `/private/tmp/teamcross-claude-assist-live-20260912-c`，脚本退出码 0，16 次生成请求全部为 `gpt-5.6-luna`。先行 UI 轮次的业务验证完成，但退出清理超时；另一轮在终端已关闭后遇到进程组退出竞态。修正 PTY 关闭与等待顺序，并保证各资源独立清理后，完整复跑通过。先行轮次保留单独记录，不计入这 16 次请求。

## 安装、调用与输入归属

[verify-claude-assist.py](../../../../scripts/verify-claude-assist.py) 通过原生 `claude mcp add --transport stdio --scope user` 安装。个人 TUI 从保存后的配置加载工具，不使用 `--mcp-config` 或 `--strict-mcp-config` 注入服务器；这些隔离参数仅用于独立的来源会话生成。

| 检查 | 实际结果 |
| --- | --- |
| MCP 安装和检测 | 连续两次接入均成功，命令和数据目录准确；其他 MCP 条目、主题和项目配置保留 |
| 无模型配置检查 | 安装、协议探测、创建和加入阶段不触发额外生成；协议探测不计为个人客户端调用 |
| 个人 TUI 真正加载工具 | native TUI 调用列举协作、读取文件/历史/批注；Claude 客户端调用时间更新，Codex 仍未观测 |
| 读取的执行位置 | A/B 存在内容不同的同名文件；个人 TUI 读到 A 的内容和来源历史，未把 B 本地文件当成共享文件 |
| 非输入者辅助 | 可以读取和写批注；测试发送被权限拒绝，A 的协作历史没有出现该输入 |
| 交接后发送 | 同一套个人工具向 A 的现有 Claude worker 发送，得到共享模型的实际回复 |
| 同一请求 ID | 再次调用返回既有结果，A 历史中只出现一次对应输入 |
| 两种参与模式 | 个人与共享 session ID 不同；个人 TUI 保持打开时，B 的直接 TUI 可接入并显示共享回复；A worker PID 不变 |
| 访问撤销 | 结束共享后，先前打开的个人 TUI 再次读取得到工具错误；不会通过旧 MCP 连接保留新的读取权限 |
| 来源保护 | A 的来源 JSONL 哈希不变，原工作目录与测试材料保留 |

个人 TUI 验收只允许明确列举的五个 Team Cross 工具，不授予本地 shell/file 工具，也不使用全局跳过权限。工具操作仍经过 Core 的成员与输入归属校验。个人模型请求和 A 共享模型请求均受测试 guard 的模型名约束。

## UI 与工程结果

真实浏览器检查个人客户端选择、两种 Provider 状态隔离、诊断按钮、复制启动命令、Claude 专用辅助入口与结束共享提示。保存 `1440×1000`、`1024×768`、`390×844` 的浅色/深色辅助面板，以及设置和打开结果截图；未发现页面或对话框横向溢出。

最初沙箱内 Core 无法访问 macOS 自动化，按钮显示失败。将同一套隔离 B Core 重启到允许桌面自动化的宿主环境后，真实按钮返回启动成功，进程及 cwd 核对确认个人 Claude 与其 Team Cross MCP 子进程位于 B 测试目录。没有用启动返回值代替原生模型验证：实际 MCP 读写由前述真实 PTY TUI 完成。Terminal App 的原生窗口读取不在本次工具允许范围，未声称完成该窗口截图验收。

Go 全项目 `cmd/...`、`internal/...` test/vet 通过，相关七个包 race test 通过；补充版本和跨 Provider 回归后对受影响包再次执行 test/race 与全项目 vet。Web check、36 项测试、production build 通过，嵌入式资源已更新。根目录已有忽略的 `bin/go-mod` 缓存，所以使用项目包路径运行 Go 门槛，未删除该缓存。

新增回归覆盖数值最低版本、实际 TUI 版本响应、个人配置路径、相对配置目录、同名覆盖/禁用、配置失败恢复、客户端调用证据隔离，以及 MCP 对 Codex 的发送/补充/中断/审批和输入归属。Codex 控制操作在这一轮使用模拟运行时回归；真实个人 Claude 模型验收的目标为 Claude 协作。

## 边界与复核

最低版本现为 `>= 2.1.268`，较新正式版不会仅因版本不同被拒绝；`2.1.268` 是本次实际运行版本，较新版本目前只有版本比较与启动计划回归，没有真实模型兼容保证。

本次补齐的是个人 Claude TUI 的 MCP 配置、检测、打开和引导。共享 Claude worker 仍未加载 Team Cross MCP，其命令审批、补充、中断与模型选择仍由当前输入者在直接原生 TUI 完成。没有新增 Claude Desktop、Computer Use、组织策略完整检测、OAuth/Keychain 验收或两台 Mac LAN 结论。

[实现契约](../decisions/claude-native-tui.md#个人-claude-code-辅助模式) · [门槛](test-gates.md#个人-claude-辅助-mcp) · [摘要、截图与清理证据](../../../../output/claude-assist-2026-09-12/README.md)。
