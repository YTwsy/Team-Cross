# 原生协作主线迁移记录

2026-09-09，Team Cross 将 Codex 原生会话协作确立为 main 的产品基线。原型阶段的能力边界继续保留，当前功能与接口以 [README](README.md)、[产品流程](docs/agent-wiki/sources/product-flows.md)、[架构](docs/agent-wiki/sources/architecture.md) 和 [协议](docs/agent-wiki/sources/protocol.md) 为准。

## 来源与归档

- 原 main 基线：`878ce7f`。
- 原生协作重构：`codex/session-collaboration` 的 `9b7f402`，在独立 worktree 中完成。
- 实验阶段保持原工作目录不变；快照保存于 `.local-backups/before-session-collaboration-20260909-002633`。
- 进入 main 前，将原主目录中 24 项已跟踪改动和 18 个未跟踪旧原型文件归档为独立 Git 提交 `aeb0826`。归档分支为 `codex/archive-before-session-collaboration-20260909`，保留以供追溯。
- 模型设置修正和主线文档基线提交为 `30a4ebb`；main 使用 fast-forward 接纳该提交及此前的新实现。
- 旧原型源码退出当前源码树。旧 Node Bridge 的 `dist` 与 `node_modules` 移到 `.local-backups/main-migration-20260909/agent-bridge-artifacts`，不再占用主目录的 `packages/agent-bridge` 路径；原快照与归档提交保留。

## 新主线

Go Core 直接管理原生 fork、两种执行目录、LAN 邀请、输入协调与生命周期；TUI/Desktop 接入同一共享会话，普通本地会话通过 MCP 辅助。React WebGUI 负责协作管理、轻量上下文和批注。旧 Thread、Round、Evidence、managed Bridge、旧页面及对应文档退出当前源码树。

模型与推理强度继承来源和原生客户端的选择，不再被实验测试配置覆盖。真实模型验证仍只使用 gpt-5.6-luna，固定配置仅保存在测试入口。界面显示原生运行时已确认的设置。

## 验证与交付

首轮在同一台 Mac 上覆盖两种目录模式与四种客户端入口，完成原生审批与并行辅助。模型设置修正另有来源继承、客户端切换、拒绝请求、恢复和界面显示回归测试。工程门槛、真实客户端结果和未完成的跨设备验收分别记录在 [2026-09-09 验收记录](docs/agent-wiki/sources/validation/native-collaboration-2026-09-09.md)。

合入后已从主工作目录使用锁文件和仓库 pnpm 缓存重装依赖，重新构建嵌入式 Web 与 Go 二进制，工程门槛全部通过；Web 产物与已提交资源一致。main 二进制恢复测试协作后保留同一 fork、目录、分支及 medium 推理强度，未触发新一轮模型调用。测试完成后关闭测试页面、Core 与 app-server，保留测试仓库、会话和截图。

## 工程知识组织

主线迁移后重新建立 [Agent Wiki](docs/agent-wiki/README.md)：产品流程、架构、协议和指定日期验收迁入 `sources/`，补充产品词汇、决策与验证契约；`wiki/concepts/` 和索引提供按任务阅读的工程上下文。目录采用此前的分层方式，内容保持当前原生协作基线。
