# Team Cross 实验分支开发指南

这是 Codex 原生会话协作实验。可以重构，不维护旧 Thread、Round、Evidence、managed Bridge 或旧数据兼容性。

先读 README.md、docs/architecture.md、docs/protocol.md。产品范围以用户已确认的方案为准。

- 协作始终创建新的原生 Codex fork。原目录模式保留当前 Git 状态；新 worktree 从选定 HEAD 检出，不恢复未提交内容。
- 原目录由用户拥有，不能在协作结束、创建失败或清理时删除。新 worktree 同样在结束共享后保留。
- 会话和执行留在 A；本机 TUI 与专用 Desktop 直接接入，用户自己的 TUI/Desktop 通过 MCP 辅助。
- 所有写入入口共享输入归属；读取可并行。发送、接收和完成是不同状态。断线不自动重放写入。
- 不对 Provider 输入自动添加同事身份，不要求问题描述或结果验收。
- 真实模型验证仅使用 gpt-5.6-luna，使用专门测试会话和仓库；不得把同机实验说成两台 Mac 验证。
- 只在本实验 worktree 内实施清理。原工作区、.local-backups 和用户已有 Codex 会话不能被清理。
- UI 使用简体中文，协议字段保持英文。浅色为主要设计基准，支持系统、浅色、深色主题。
- 验证：go test ./...、go vet ./...、相关 go test -race、pnpm --filter @teamcross/web check/test/build。
- 修改前端后更新 internal/webassets/dist，进行真实浏览器与截图复核。
- 默认不创建子 Agent；只有用户明确要求才委派。
