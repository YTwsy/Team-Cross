# 原生协作主线迁移记录

2026-09-09，Team Cross 将 Codex 原生会话协作确立为 main 的产品基线。原型阶段的能力边界继续保留，当前功能与接口以 README、docs/product-flows.md、docs/architecture.md 和 docs/protocol.md 为准。

## 来源与归档

- 原 main 基线：`878ce7f`。
- 原生协作重构：`codex/session-collaboration` 的 `9b7f402`，在独立 worktree 中完成。
- 实验阶段保持原工作目录不变；快照保存于 `.local-backups/before-session-collaboration-20260909-002633`。
- 进入 main 前，将原主目录中 24 项已跟踪改动和 18 个未跟踪旧原型文件归档为独立 Git 提交。归档分支为 `codex/archive-before-session-collaboration-20260909`，保留以供追溯。
- main 使用 fast-forward 接纳新实现；旧原型源码和旧 Node Bridge 构建产物退出主工作目录，快照与归档提交保留。

## 新主线

Go Core 直接管理原生 fork、两种执行目录、LAN 邀请、输入协调与生命周期；TUI/Desktop 接入同一共享会话，普通本地会话通过 MCP 辅助。React WebGUI 负责协作管理、轻量上下文和批注。旧 Thread、Round、Evidence、managed Bridge、旧页面及对应文档退出当前源码树。

模型与推理强度继承来源和原生客户端的选择，不再被实验测试配置覆盖。真实模型验证仍只使用 gpt-5.6-luna，固定配置仅保存在测试入口。界面显示原生运行时已确认的设置。

## 验证与交付

首轮在同一台 Mac 上覆盖两种目录模式与四种客户端入口，完成原生审批与并行辅助。模型设置修正另有来源继承、客户端切换、拒绝请求、恢复和界面显示回归测试。工程门槛、真实客户端结果和未完成的跨设备验收分别记录在 docs/validation.md。

合入后从主工作目录重新安装工作区依赖、构建嵌入式 Web 和 Go 二进制并运行工程门槛。测试完成后关闭测试页面、Core 与 app-server，保留测试仓库、会话和截图。
