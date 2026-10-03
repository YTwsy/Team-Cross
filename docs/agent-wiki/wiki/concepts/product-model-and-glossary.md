# 产品模型与词汇

本页解释产品对象之间的关系，完整定义由 [核心词汇](../../sources/product-core-and-glossary.md) 维护。Team Cross 从已有 Session 和会话记录入手，让人和各自的 Agent 按需分享经验、讨论材料，必要时共同执行；设计原因和总体范围见 [项目简报](../../sources/project-brief.md)。

空间托管成员、已发布材料和讨论。个人会话是材料的来源，也可以在明确授权下参与空间请求；它不会因为被引用或关联就全部公开。[接收会话](../../sources/product-core-and-glossary.md#会话与接收能力)描述会话用途，已有、新建和 fork 不据此分成不同的投递能力类别。共同执行使用新的原生 fork，目录与输入归属另行确定。空间工作台中的专用会话是可选协调角色，与共同执行分别管理。

## 容易混淆的对象

| 对象关系 | 理解时的区别 | 主要来源 |
| --- | --- | --- |
| 空间、个人会话、已发布材料 | 空间组织协作，个人会话保留原生所有权，材料固定已公开的范围与版本 | [空间对象语义](../../sources/product-core-and-glossary.md#协作空间的对象边界)、[空间契约](../../sources/decisions/collaboration-spaces.md) |
| 来源会话、新 fork、执行目录 | 新建会话与选择工作目录是两个步骤；原目录模式也使用新会话 | [目录与生命周期](../../sources/decisions/workspace-and-lifecycle.md) |
| 成员资格、在线状态、输入归属 | 能访问空间、当前已连接、现在可写入分别判断 | [输入与共享](../../sources/decisions/input-and-sharing.md) |
| 配对目标、空间请求、专用角色 | 配对确定接收路径，请求保存明确要求与回执，角色决定是否承担空间接手职责 | [接收配对](../../sources/decisions/agent-pairing.md)、[空间工作台](../../sources/decisions/space-workbench.md) |
| 批注、发送、读取、完成 | 保存讨论不自动触发输入；发送成功和 Agent 读取、报告完成各有自己的回执 | [产品流程](../../sources/product-flows.md#上下文阅读)、[请求契约](../../sources/decisions/agent-pairing.md#投递与回执) |
| 产品对象与原生协议术语 | Codex Thread/Turn 是 Provider 会话与轮次，不是旧产品的 Thread/Round 模型 | [词汇映射](../../sources/product-core-and-glossary.md#当前实现的词汇映射)、[协议](../../sources/protocol.md) |

## 按问题查来源

定位、目标范围和 [GPL-3.0-only](../../../../LICENSE) 许可看 [项目简报](../../sources/project-brief.md)；用户怎么完成某个动作看 [产品流程](../../sources/product-flows.md)；Provider 和客户端差异看 [原生客户端](../../sources/decisions/native-clients-and-models.md) 与 [Claude 接入](../../sources/decisions/claude-native-tui.md)。产品不固定模型或推理强度，显示语义见 [模型设置](../../sources/product-core-and-glossary.md)。

命名变更从 [协作类型](../../../../internal/collab/types.go)、[页面组件](../../../../packages/web/src/components/) 和 [产品路径测试](../../../../packages/web/src/test/flows.test.tsx) 追踪；主要来源更新后，只有本页解释发生变化时才同步本页。判断能力是否已经验收，另从 [证据入口](../../sources/validation/evidence-map.md) 读取对应路径与版本。

相关导航：[协作空间](collaboration-spaces.md) · [WebGUI 与 MCP](webgui-and-mcp.md) · [运行时架构](runtime-architecture.md)
