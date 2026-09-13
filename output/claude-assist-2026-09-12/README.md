# Claude Code 个人辅助模式证据

完整通过轮次：`/private/tmp/teamcross-claude-assist-live-20260912-c`，退出码 0；一台 Mac、两套真实 Core、Claude Code 2.1.268，16 次生成请求均为 CliProxyAPI 的 gpt-5.6-luna。

- [结构化验收摘要](acceptance-summary.json)：真实个人 MCP 安装、读写、输入归属、去重、会话隔离和结束访问。
- [布局诊断](browser-layout.json)：三个尺寸、浅色/深色复核，无页面或对话框横向溢出，记录阶段控制台为空。
- `assist-light-*.png`、`assist-dark-*.png`：1440、1024、390 像素宽的真实辅助面板。
- `settings-dark-*.png`：设置页 Claude 配置、工具协议与实际客户端调用分别显示。
- [原生打开结果](native-open-result.png)、[原生进程核对](native-ui-processes.json)：允许桌面自动化的隔离 B Core 通过按钮启动个人 Claude 与 MCP 子进程；未进行 Terminal App 原生窗口截图。
- [结束共享提示](ended-share.png)：旧成员的新读取被拒绝，已取得的历史上下文不作远程抹除。
- [清理核对](cleanup-audit.json)：本次三套实验目录和明确记录的进程核对。

浏览器和原生打开截图来自先行轮次 `...-a`。该轮全部业务操作完成，但验收脚本在 TUI 退出时超时；`...-b` 遇到关闭终端后继续 killpg 的退出竞态。清理逻辑修正后，`...-c` 完整通过，个人 TUI、直接 TUI 和两个 Core 正常关闭。此前轮次的失败记录与数据目录保留，不计入完整通过轮次的 16 次请求；三个轮次都只使用选定的 Luna 路由。

日常 Claude 配置和会话未修改。截图中的路径、会话与输入均属于专用测试；无 API 凭据或邀请 secret。
