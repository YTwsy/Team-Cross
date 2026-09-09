# Team Cross 开发指南

`main` 以 Codex 原生会话协作为产品主线，当前仍处于原型阶段。可以重构，不维护旧 Thread、Round、Evidence、managed Bridge 或旧数据兼容性。

## 进入仓库

1. 读 [README.md](README.md)，了解用户能做什么、如何启动以及当前支持范围。
2. 读 [Agent Wiki 索引](docs/agent-wiki/wiki/index.md)，按任务选择短页面。
3. 需要完整依据时，沿短页面进入 `sources/` 和相关代码；产品范围以用户已确认的方案为准，代码和测试说明实际能力。

| 当前任务 | 优先阅读 |
| --- | --- |
| 产品范围、命名、用户流程 | [产品模型与词汇](docs/agent-wiki/wiki/concepts/product-model-and-glossary.md) |
| 模块、启动、数据路径 | [运行时架构](docs/agent-wiki/wiki/concepts/runtime-architecture.md) |
| Git、worktree、fork、恢复与结束 | [目录与生命周期](docs/agent-wiki/wiki/concepts/workspace-and-lifecycle.md) |
| TUI/Desktop、登录、模型与原生接入 | [原生客户端与模型](docs/agent-wiki/wiki/concepts/native-clients-and-models.md) |
| 邀请、输入交接、审批、断线与共享 | [输入协调与共享](docs/agent-wiki/wiki/concepts/input-and-sharing.md) |
| 页面、上下文、批注与辅助工具 | [WebGUI 与本地 MCP](docs/agent-wiki/wiki/concepts/webgui-and-mcp.md) |
| 选择测试与判断验收结论 | [验证门槛](docs/agent-wiki/wiki/concepts/validation-gates.md) |

完整说明：[产品流程](docs/agent-wiki/sources/product-flows.md) · [架构](docs/agent-wiki/sources/architecture.md) · [协议](docs/agent-wiki/sources/protocol.md)。

## 产品与工程边界

- 协作始终创建新的原生 Codex fork。原目录模式保留当前 Git 状态；新 worktree 从选定 HEAD 检出，不恢复未提交内容。
- 原目录由用户拥有，不能在协作结束、创建失败或清理时删除。新 worktree 同样在结束共享后保留。
- 会话和执行留在 A；本机 TUI 与专用 Desktop 直接接入，用户自己的 TUI/Desktop 通过 MCP 辅助。
- 所有写入入口共享输入归属；读取可并行。发送、接收和完成是不同状态。断线不自动重放写入。
- 不对 Provider 输入自动添加同事身份，不要求问题描述或结果验收。
- 产品不固定模型或推理强度。创建继承原生来源，恢复读取协作会话的持久化设置，后续遵循当前输入者在 Codex 中的选择；界面只报告运行时确认的设置。
- 真实模型验证仅使用 gpt-5.6-luna，使用专门测试会话和仓库；不得把同机实验说成两台 Mac 验证。
- 清理只作用于当前任务明确覆盖的源码或产物。保留 `.local-backups`、归档分支和用户已有 Codex 会话；不要将旧原型文件重新混入主线。
- UI 使用简体中文，协议字段保持英文。浅色为主要设计基准，支持系统、浅色、深色主题。
- 默认不创建子 Agent；只有用户明确要求才委派。

## 验证与收尾

按 [验证契约](docs/agent-wiki/sources/validation/test-gates.md) 选择检查。产品代码的工程门槛包括 Go test/vet、相关 race test 和 Web check/test/build；修改前端后更新 `internal/webassets/dist`，进行真实浏览器与截图复核。

仅修改文档时检查相对链接、代码路径、旧引用、索引可达性和 `git diff --check`，无需重新启动模型或测试客户端。实际验证日期、版本与环境保存在 `sources/validation/` 的对应记录中，不将旧验收结果自动推广到后续版本。

验证后关闭本次启动的测试客户端、Core、app-server、浏览器页面及其测试辅助进程；先核对 PID、父子关系或测试目录，不按程序名批量终止用户的 Codex 或浏览器。

## 文档维护

- [Agent Wiki 说明](docs/agent-wiki/README.md) 定义维护流程。`sources/` 保存完整事实、决策与验证契约，`wiki/concepts/` 保存短小的任务上下文，`wiki/index.md` 负责导航。
- 影响长期判断的改动在同一提交中同步来源与对应短页面，并检查本文件和 README 的入口。
- [核心词汇](docs/agent-wiki/sources/product-core-and-glossary.md) 负责产品语义，[协议](docs/agent-wiki/sources/protocol.md) 负责字段与路由；短页面不重复维护完整规范。
- 旧现场与 main 迁移见 [IMPLEMENTATION.md](IMPLEMENTATION.md)。归档材料只供考据，不能把旧产品边界重新当作当前要求。
- 不把一次性日志、普通 TODO、未验证猜测、凭据或真实邀请 secret 写入 Wiki；未来 compiler 的 `.llmwiki/` 状态保持本地忽略。
