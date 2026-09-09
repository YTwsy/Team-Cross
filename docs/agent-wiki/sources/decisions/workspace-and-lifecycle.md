# 会话、执行目录与生命周期决策

## 决策与原因

会话身份与 Git 工作目录分别处理。协作始终创建新的原生 fork，以保留来源关联；目录由用户选择，满足“在当前现场一起继续”和“从干净提交独立工作”两种需求。

| 选择 | 创建时的约定 | 原因 |
| --- | --- | --- |
| `existing` | 使用来源执行目录，保留当前分支、暂存区和所有现有文件 | 用户选择的是当前现场，不应隐式切分支或替换文件 |
| `worktree` | 从确认的 HEAD 检出，创建 `codex/collab-<短 ID>`，映射来源子目录 | 与普通 Git worktree 的干净检出行为一致，不额外建立 dirty patch 或未跟踪文件搬运流程 |

预览绑定来源、完成轮、模式、目录、HEAD 和分支。起点变化后重新确认，不把旧预览当作创建当前状态的授权。原目录中的未提交内容是实时现场，不宣称已经生成它的内容快照。

创建、预览与读取不发送业务 prompt。恢复已有协作使用保存的 `sessionId` 和 `executionCwd`，不再次 fork 或创建 worktree。

## 保留与结束

原目录属于用户已有目录。`workspaceOwned` 只是归属信息，结束共享时原目录和新 worktree 都保留。创建失败后的已创建资源与错误记录也保留，避免自动重试或清理破坏可追溯现场。

结束共享只关闭远端访问。用户可在本地继续会话、提交代码或使用其他开发工具，不需要先填问题、生成摘要、导出 patch 或完成验收表。

当前需要普通、已有 HEAD 的 Git 仓库。不要将旧原型对 unborn 仓库、只读 Thread 或 patch export 的处理方式移入这条创建路径。

## 重新评估条件

引入多仓库、submodule、内容快照或显式资源删除时，应单独定义目录归属、失败恢复和用户动作，再补对应测试；不能通过扩展 `workspaceMode` 的隐含含义实现。

## 实现与验证

- [工作目录实现](../../../../internal/workspace/workspace.go) 与 [目录测试](../../../../internal/workspace/workspace_test.go)：原目录前后 Git 状态、四类未提交内容、子目录映射。
- [创建与恢复](../../../../internal/collab/app.go) 与 [协作测试](../../../../internal/collab/collab_test.go)：新 fork、来源关联、无隐式 prompt、恢复同一 ID。
- [生命周期与共享](../../../../internal/collab/network.go)：结束访问与重开运行时。

完整字段见 [协议](../protocol.md)，任务入口见 [目录与生命周期](../../wiki/concepts/workspace-and-lifecycle.md)。
