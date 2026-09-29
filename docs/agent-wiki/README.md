# Team Cross Agent Wiki

这里是面向 coding agent 的工程知识层，帮助后续任务快速找到产品约束、相关代码和验证方法。当前采用手工维护，沿用 OpenSurge 与 Team Cross 早期 Agent Wiki 的目录组织方式。

从 [任务索引](wiki/index.md) 开始。面向用户的产品概览与快速开始见根目录 [README](../../README.md)，详细操作见 [使用指南](../user-guide.md)，本地构建与检查见 [开发与验证](../development.md)。

## 目录与职责

```text
docs/agent-wiki/
  README.md
  sources/
    project-brief.md
    product-core-and-glossary.md
    product-flows.md
    architecture.md
    protocol.md
    decisions/
      workspace-and-lifecycle.md
      native-clients-and-models.md
      input-and-sharing.md
    validation/
      test-gates.md
  wiki/
    index.md
    concepts/
      ...
```

- `sources/` 保存完整的稳定事实：产品范围、流程、架构和协议。每类事实有一个主要维护位置。
- `sources/decisions/` 记录取舍、原因、影响及何时需要重新评估。
- `sources/validation/` 保存当前可复用的验证方法、通过标准与结论边界；指定版本的执行报告按 [任务记录与历史归档](../tasks/README.md) 管理。
- `wiki/concepts/` 提供短小的任务上下文，说明先读什么、改哪里、守住什么、如何验证，并链接回来源和实现。
- `wiki/index.md` 负责检索与导航；根目录 [AGENTS.md](../../AGENTS.md) 负责首次进入仓库的阅读顺序。

当前没有启用自动 wiki compiler。目录形状为未来刷新、审查和上下文包工作流保留空间；若接入 compiler，本地状态使用 `.llmwiki/`，该目录已被 Git 忽略。

## 事实来源

用户已确认的产品决策确定目标；代码和测试说明当前实际能力。二者不一致时，应明确记录差异并修正相关实现或说明，不能把计划写成已经验收。

[产品核心与词汇](sources/product-core-and-glossary.md) 负责语义，[产品流程](sources/product-flows.md) 负责用户动作，[架构](sources/architecture.md) 负责职责，[协议](sources/protocol.md) 负责字段与路由。Wiki 只保留任务所需摘要，不另行复制完整协议表。

本次恢复的是知识组织方式。当前内容以 main 的 Codex 原生协作为基准；旧 Thread、Round、Evidence、managed Bridge、旧 Claude Bridge 和旧传输方案仍属于历史实现。新的实验性 Claude 原生 TUI 以 [当前接入契约](sources/decisions/claude-native-tui.md) 为准。需要考据时查看 [迁移记录](../../IMPLEMENTATION.md) 指向的归档提交。

## 更新流程

1. 判断变化是否会影响未来任务的产品或工程判断。
2. 更新对应来源；涉及新的取舍时补充决策的原因和重新评估条件。
3. 同步更新关联的短页面，检查任务索引、AGENTS 和 README 的入口。
4. 按 [验证门槛](sources/validation/test-gates.md) 执行与变更相关的检查，将实际结果与待验收范围分开记录。
5. 在同一提交中检查相对链接、代码路径和已经移除的引用。

这些更新随开发分支中的代码推进，不集中拖到合入时。修复既有行为、样式微调等未改变长期知识的工作，不必为每个 fix 新增决策、概念页或任务文件。

Markdown 默认使用简体中文；API、命令、协议字段和代码标识符保持英文。实际验证结果注明日期、版本、环境和未覆盖范围，按需保存在任务记录中；收尾后移入 `docs/tasks/finished_archived/`。已有单份报告可直接按原文件名归档，不强制补目录与 README。

Wiki 索引和概念页聚焦当前知识，不持续追加历史报告清单。当前来源可以保留必要的历史证据链接，但归档不作为默认阅读材料，也不自动证明后续版本通过。普通搜索通过仓库根目录 `.ignore` 排除归档，具体追溯方法见 [任务记录规则](../tasks/README.md)。

一次性日志、临时命令输出、普通 TODO、未验证猜测、账户凭据和真实邀请 secret 不进入 Wiki。文档整理本身不需要启动 Codex、调用模型或打开测试客户端。
