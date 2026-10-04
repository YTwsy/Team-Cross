# Team Cross 开发指南

`main` 以 Codex 原生会话协作为产品主线，当前仍处于原型阶段；Claude Code 原生 TUI 为实验性接入，边界见 [Claude 接入契约](docs/agent-wiki/sources/decisions/claude-native-tui.md)。可以重构，不维护旧 Thread、Round、Evidence、managed Bridge 或旧数据兼容性。

## 进入仓库

1. 遵守本文件的全局约束，按 [Agent Wiki 索引](docs/agent-wiki/wiki/index.md) 定位本次任务相关的来源或代码；已知入口时可直接进入。
2. 需要跨主题理解时选读对应概念页，再按问题追溯来源；不要求每次依次读完 README、概念页和来源。
3. 不熟悉产品、启动方式或支持范围时读 [README.md](README.md)。产品范围以用户已确认的方案为准，代码和测试说明实际能力。

## 产品与工程边界

- 空间托管参与者、已发布材料和讨论，可独立只读分享且支持三人及以上。只有启用共同执行才创建新的原生 fork；原目录保留 Git 现场，新 worktree 从选定 HEAD 检出。结构与范围遵守 [协作空间契约](docs/agent-wiki/sources/decisions/collaboration-spaces.md)。
- 原目录由用户拥有，不能在协作结束、创建失败或清理时删除。新 worktree 同样在结束共享后保留。
- 会话和执行留在 A；本机原生 TUI 与 Codex 专用 Desktop 直接接入，个人 Codex TUI/Desktop 或 Claude Code TUI 通过 MCP 辅助，辅助客户端与目标协作的 Provider 独立。
- 协作模式在创建时固定：默认受限，信任模式沿用邀请者的原生配置与权限；Codex 与实验性 Claude 均支持。恢复沿用模式，不提供切换；细则见 [协作模式](docs/agent-wiki/sources/decisions/runtime-modes.md)。
- 同一空间链接可供多人加入，每位成员凭据独立；启用执行保留链接与成员，新增执行访问由发起者明确开放。关闭或重置链接不撤销已有成员。
- 所有写入入口共享输入归属；读取可并行。发送、接收和完成是不同状态。断线不自动重放写入。
- 不对 Provider 输入自动添加同事身份，不要求问题描述或结果验收。
- 产品不固定模型或推理强度。创建继承原生来源，恢复读取协作会话的持久化设置，后续遵循当前输入者在 Codex 中的选择；界面只报告运行时确认的设置。
- 真实模型验证仅使用 gpt-5.6-luna，使用专门测试会话和仓库；不得把同机实验说成两台 Mac 验证。
- 清理只作用于当前任务明确覆盖的源码或产物。保留 `.local-backups`、归档分支和用户已有 Codex 会话；不要将旧原型文件重新混入主线。
- 菜单栏与 WebGUI 共用本机界面语言偏好；默认跟随 macOS 首选语言，中文语言代码使用简体中文，其他语言使用英文。两处手动选择都会影响本机两套界面。协议字段保持英文，用户内容不翻译。浅色为主要设计基准，支持系统、浅色、深色主题。
- 默认不创建子 Agent；只有用户明确要求才委派。

## 验证与收尾

按 [验证契约](docs/agent-wiki/sources/validation/test-gates.md) 选择检查。产品代码的工程门槛包括 Go test/vet、相关 race test 和 Web check/test/build；修改前端后更新两套嵌入资源。界面是主要改动时进行真实浏览器与截图复核；以服务、协议或生命周期为主且仅附带小型状态文案调整时，用相关交互测试覆盖即可，默认不做浏览器与截图复核。

仅修改文档时检查相对链接、代码路径、旧引用、索引可达性和 `git diff --check`，无需重新启动模型或测试客户端。`sources/validation/` 维护验证要求与[证据入口](docs/agent-wiki/sources/validation/evidence-map.md)；完整执行结果按 [任务记录规则](docs/agent-wiki/README.md#任务记录) 留存，不将旧验收结果自动推广到后续版本。回答支持范围或是否已验收时，核对具体路径的契约、实现及执行记录，分别报告原生能力、接入范围和被测环境；未收录证据不等于不支持。

验证后关闭本次启动的测试客户端、Core、app-server、浏览器页面及其测试辅助进程；先核对 PID、父子关系或测试目录，不按程序名批量终止用户的 Codex 或浏览器。

## 文档维护

- [Agent Wiki 说明](docs/agent-wiki/README.md) 定义维护流程。`sources/` 根目录维护项目级规范与操作说明，`decisions/` 维护当前领域契约与设计取舍，`validation/` 维护验证方法与证据范围；`wiki/index.md` 直接指向来源或代码，`wiki/concepts/` 只按需解释跨主题关系并导航，不重复维护完整规范或验收状态。
- 影响长期判断时，在同一提交中更新该事实的主要维护位置；只有摘要内容或阅读入口也发生变化时，才同步概念页、索引、本文件或 README。未改变长期知识的小改动不新增文档。
- 验收范围扩展、出现阻塞或相关改动影响旧证据时，同步更新证据入口与领域契约中的当前限制；归档前保留从当前来源到相关报告的直接入口。
- [核心词汇](docs/agent-wiki/sources/product-core-and-glossary.md) 负责产品语义，[协议](docs/agent-wiki/sources/protocol.md) 负责字段与路由；短页面不重复维护完整规范。
- 旧现场与 main 迁移见 [IMPLEMENTATION.md](IMPLEMENTATION.md)。归档材料只供考据，不能把旧产品边界重新当作当前要求。
- `sources/` 与 `wiki/` 不保存一次性日志、普通 TODO 或未验证猜测；确有接续价值的内容按任务规则记录。凭据和真实邀请 secret 不写入任何文档；未来 compiler 的 `.llmwiki/` 状态保持本地忽略。

## 任务记录与检索

- 默认不创建任务文件；跨会话接续或交接需要记录时，按 [任务记录规则](docs/agent-wiki/README.md#任务记录) 在 `docs/agent-wiki/tasks/` 先用单文件，内容较多再拆目录。活跃任务只按本次工作需要选读。
- 普通检索遵守根目录 `.ignore`，不默认列举、批量读取 `docs/agent-wiki/tasks/finished_archived/` 和 `docs/releases/`；它们不作为当前功能规范，Wiki 索引不追加历史报告清单。
- 编写发布说明、比较版本、追溯回归或当前资料不足时，可定向读取相关历史。`rg --no-ignore` 只限定到相关目录或文件；不递归展开历史证据链接，使用前核对记录版本与当前实现。
