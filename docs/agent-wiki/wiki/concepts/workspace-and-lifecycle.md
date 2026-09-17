# 目录与生命周期

这页用于修改 Git 预览、目录模式、fork、恢复或结束行为。先读 [目录与生命周期决策](../../sources/decisions/workspace-and-lifecycle.md) 和 [产品流程](../../sources/product-flows.md)。

## 需要分别验证的行为

| 动作 | 必须保留的语义 |
| --- | --- |
| 原目录创建 | 新会话 ID；原分支、HEAD、暂存区和文件保持不变 |
| 新 worktree 创建 | 从确认 HEAD 检出，新分支自动命名；不复制任何未提交内容 |
| 仓库子目录映射 | `executionCwd` 指向来源子目录在所选工作目录中的对应位置 |
| 起点变化 | 重新预览，不用旧的确认结果创建另一份现场 |
| 当前会话分享 | 核对原生调用身份，登记后等待固定本轮完成再 fork；取消、漂移或重启均不自动重放 |
| 恢复 | 继续同一 fork 和目录，不再次创建资源 |
| 结束共享 | 立即撤销远端访问；空闲且专用客户端关闭后释放原生会话占用，保留会话和代码 |

原目录创建不改文件，不代表后续 Agent 不能按用户输入修改原目录。`workspaceOwned` 不能当成结束共享时删除 worktree 的许可。

## 代码与检查

[workspace.go](../../../../internal/workspace/workspace.go) 负责 Git 与文件路径；[app.go](../../../../internal/collab/app.go) 负责预览、创建和恢复；[network.go](../../../../internal/collab/network.go) 负责共享生命周期；[lifecycle.go](../../../../internal/collab/lifecycle.go) 负责空闲释放与旧连接隔离。

优先运行 [目录测试](../../../../internal/workspace/workspace_test.go) 与 [创建恢复测试](../../../../internal/collab/collab_test.go)。涉及真实原生 fork 时使用 [专用 Luna 测试](../../sources/validation/test-gates.md)，不得拿用户已有工作目录当证明文件测试仓库。

相关任务：[输入与共享](input-and-sharing.md) · [模型设置](native-clients-and-models.md) · [验证](validation-gates.md)

Claude 的后台 fork 可能在首条输入前只保存来源引用；恢复时优先重新绑定仍存活的 worker，否则按原生 job 参数继续同一 ID。不能复用创建时的完整命令行。见 [Claude 接入契约](../../sources/decisions/claude-native-tui.md)。
