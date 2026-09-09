# Team Cross 开发指南

`main` 以 Codex 原生会话协作为产品主线，当前仍处于原型阶段。可以重构，不维护旧 Thread、Round、Evidence、managed Bridge 或旧数据兼容性。

先读 README.md、docs/architecture.md、docs/protocol.md。产品范围以用户已确认的方案为准。

- 协作始终创建新的原生 Codex fork。原目录模式保留当前 Git 状态；新 worktree 从选定 HEAD 检出，不恢复未提交内容。
- 原目录由用户拥有，不能在协作结束、创建失败或清理时删除。新 worktree 同样在结束共享后保留。
- 会话和执行留在 A；本机 TUI 与专用 Desktop 直接接入，用户自己的 TUI/Desktop 通过 MCP 辅助。
- 所有写入入口共享输入归属；读取可并行。发送、接收和完成是不同状态。断线不自动重放写入。
- 不对 Provider 输入自动添加同事身份，不要求问题描述或结果验收。
- 产品不固定模型或推理强度。创建继承原生来源，恢复读取协作会话的持久化设置，后续遵循当前输入者在 Codex 中的选择；界面只报告运行时确认的设置。
- 真实模型验证仅使用 gpt-5.6-luna，使用专门测试会话和仓库；不得把同机实验说成两台 Mac 验证。
- 清理只作用于当前任务明确覆盖的源码或产物。保留 `.local-backups`、归档分支和用户已有 Codex 会话；不要将旧原型文件重新混入主线。
- 产品流程以 `docs/product-flows.md` 为准，历史迁移看 `IMPLEMENTATION.md`，验证范围看 `docs/validation.md`。同步更新长期事实，避免分支限定说明重新变成产品约束。
- UI 使用简体中文，协议字段保持英文。浅色为主要设计基准，支持系统、浅色、深色主题。
- 验证：go test ./...、go vet ./...、相关 go test -race、pnpm --filter @teamcross/web check/test/build。
- 修改前端后更新 internal/webassets/dist，进行真实浏览器与截图复核。
- 默认不创建子 Agent；只有用户明确要求才委派。
