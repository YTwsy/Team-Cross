# Claude 原生 TUI 验收材料

日期：2026-09-12。分支：`Codex/feature/claude-native-tui`。

[正式验收记录](../../docs/agent-wiki/sources/validation/claude-native-tui-2026-09-12.md) 描述环境、检查结果与边界；[可重复运行的实测脚本](../../scripts/verify-claude-native.py) 需要新建专用测试目录。

## 真实执行摘要

[acceptance-summary.json](acceptance-summary.json) 来自完整通过轮次：一台 Mac、两个真实 Core、Claude Code 2.1.268、CliProxyAPI gpt-5.6-luna。包含原目录/worktree、输入归属、审批接力、实际 STDIO MCP 发送去重、原生中断、推理强度恢复和 Core 异常恢复。完整轮次 10 次模型请求全部使用 Luna；B 客户端没有持久化 A 的 provider 历史。

原始日志与原生终端记录保存在本机 `/private/tmp/teamcross-claude-feature-live-20260912-k/evidence`。额外的零模型生成验收位于 `/private/tmp/tcx-claude-checkpoint-rkf95bxf`：原目录/worktree 各三次真实 TUI 打开，中间两次停止并恢复，始终显示来源历史并保持 ID。

[清理核对](cleanup-audit.json) 确认本轮测试目录和二进制没有剩余匹配进程，也没有进程继续打开测试目录中的文件。页面复核额外模型请求为零。

## 页面截图

- [首页，1440 浅色](home-1440-light.png)
- [Claude 来源，1440 浅色](create-claude-1440-light.png)
- [worktree 预览，1440 浅色](create-worktree-1440-light.png)
- [详情，1440 浅色](detail-1440-light.png) / [1440 深色](detail-1440-dark.png)
- [详情，1024 浅色](detail-1024-light.png) / [1024 深色](detail-1024-dark.png)
- [详情，768 浅色](detail-768-light.png) / [768 深色](detail-768-dark.png)
- [Claude 原生客户端与启动命令，1440 深色](clients-1440-dark.png)
- [Claude CLI 设置，1024 深色](settings-1024-dark.png)
- [加入页面，768 浅色](join-768-light.png)
- [浏览器视口与日志检查](browser-layout.json)

截图中的 Codex CLI 为刻意设置的 `/usr/bin/false`，其错误提示验证 Claude 入口不会依赖 Codex 安装。截图以实际测试数据复核，不包含邀请 secret 或 API 凭据。构建后对加入、批注提示和单一 TUI 卡片进行了文案/布局收尾并再次截图；详情的六种视口主题组合用于验证布局边界。浏览器检查不替代原生客户端和真实双 Mac LAN 验收。
