# 实施记录

实验分支 `codex/session-collaboration`；实现目录为原仓库下 `.worktrees/session-collaboration`。
原工作区未修改；已保存快照 `.local-backups/before-session-collaboration-20260909-002633`。

## 已实现

- 新 Go 协作核心、JSON 持久化、原生 app-server 复用网关、TLS 邀请、本机代理、MCP STDIO。
- 原目录与干净 worktree 两种模式，原生 fork、同一会话恢复、输入交接、审批和断线写入状态。
- 新 React WebGUI：首页、两步创建、协作详情、加入、设置、主题与响应式布局。
- TUI 与独立 Desktop 启动；Desktop 账户操作和偏好落到客户端本机，不经远端共享传输。
- 原有 Node Bridge、Evidence、Round、旧 Web 与旧文档从实验分支移除。

## 已完成验证

- Go 测试、vet、collab/MCP race；前端类型、6 项交互测试、production build。
- 独立测试仓库中两种模式的原生 fork 和真实 Luna 文件操作。
- 两种目录模式分别覆盖直接 TUI、直接 Desktop、辅助 TUI、辅助 Desktop；用户操作 Desktop，API 和目标文件核实结果。
- 直接客户端保持连接期间，普通 Desktop 经 MCP 读取两种目录并发送、读取各自结果。
- 原生审批在 Desktop 显示，经 MCP 允许一次并完成，输出 TEAMCROSS_APPROVAL_OK。
- 本机 A/B 独立服务经 TLS 加入、输入交接；真实两台 Mac 不在上述结论中。
- 首页、创建、详情、加入、设置在三种实际 CSS 窗口宽度、深浅主题下截图复核；检查键盘与关键失败状态。

## 交付边界

- 详细结果、复现命令与跨设备验收步骤见 docs/validation.md。
- Desktop 私有指定运行时入口按当前安装版本验证，不承诺其他全局菜单和后续版本的完整兼容。
- 提交前核对原工作区快照；结束后关闭本次测试的客户端、服务和浏览器页面，保留测试材料。

全部真实模型调用固定使用 gpt-5.6-luna。两台 Mac 的 LAN 验收需要第二台真实设备，必须单列。
