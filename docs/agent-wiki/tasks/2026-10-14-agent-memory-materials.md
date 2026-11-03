# Agent 记忆浏览与材料发布方案

状态：待评审方案。用户已确认继续设计，尚未进入实现。

参与者在自己的 Core 上浏览 Codex、Claude Code 的记忆，选择原文或片段，预览后发布为协作空间材料。其他成员、人和 Agent 共用材料的固定版本、读取与引用能力。首期以文件型记忆完成闭环，Codex 内部数据库和子 Agent 专属记忆作为后续适配。

调查日期：2026-10-14。Team Cross 基线为 `8d366ccac30c9b636d05651c621e69d0fe503662`；Wake 参考提交为 [`7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d`](https://github.com/iAmCorey/Wake/tree/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d)。下文“现有”指该基线，“拟”或各方案章节描述待实现行为。本次仅检查源码和文档，没有运行 Wake、启动模型或验证真实记忆数据库。

## 从 Wake 采用的设计

Wake 将来源发现、记忆文档、索引与阅读界面分开。`MemoryDoc` 保存 Agent、来源、用户级或项目级或会话级范围、项目关联、标题、路径、更新时间和正文；GUI 与 MCP 共用这些数据。文件标题从 frontmatter 的 description、name 取得，失败时使用文件名。[文档模型](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/models.rs#L762)、[通用读取](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/mod.rs#L1137)

| Wake 的实际实现 | Team Cross 的取舍 |
| --- | --- |
| 按 Provider 派生来源，支持来源启停与自定义位置 | 使用独立的本机记忆适配器；路径配置只影响本机发现 |
| 常规目录只列直属指定扩展名文件；Claude 按项目目录展开 memory 子目录 | 明确列出各来源的深度，避免将整个 Agent home 视为记忆 |
| 按路径、mtime、size 等缓存；刷新记忆与刷新会话分开 | 初次打开与主动刷新触发记忆扫描，不为阅读启动原生执行进程 |
| 一处读取失败只冻结该来源的旧索引，区分失败与确实为空 | 列出 unavailable、missing、unsupported 等状态；失败不覆盖已发布版本 |
| 按 Agent、项目、用户级与未知项目筛选，列表和正文分栏 | 复用 Team Cross 资源库布局，增加明确的私人浏览范围 |

扫描与失败隔离见 [Wake scanner](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/scanner.rs#L737)，界面见 [Memory 页](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake/src/workbench/memory.rs#L340)。

有三处需要按发布场景重新设计：

- Wake 的 `memory_body` 现场读取失败时会回退索引正文；Team Cross 可以显示标明时间的旧预览，但创建新发布快照时须成功读取选定来源，不能把缓存当成当前源。
- Wake 的 `wake_get_session` 读取记忆采用字符截断，最大 100000 字符，没有记忆正文续页游标；Team Cross 应提供真正的有界分页。上一轮讨论中的“分页读取”对 Wake 记忆本身并不准确。
- Wake 的 Codex `stage1_outputs` 适配注明 schema、类型和时间字段部分来自推断，且本机零行、未经真实数据验证；不能据此承诺 Team Cross 已掌握稳定的 Codex 记忆 API。

对应实现：[现场读取与回退](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/mod.rs#L1237)、[MCP 记忆读取](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/mcp/tools.rs#L995)、[Codex 数据库适配](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/codex.rs#L1009)。

采用其设计经验，在现有 Go Core 中实现文件读取，不要求用户安装 Wake，也不增加对 Wake 索引服务的运行时依赖。

## 首期读取范围

### 来源根目录

Codex 默认根沿用 `nativecodex.Home()`，即有效的 `CODEX_HOME`，未配置时使用 `~/.codex`。Claude 沿用 `providerHome("claude")`，依次使用 Team Cross 显式 `ClaudeHome`、`CLAUDE_CONFIG_DIR`、`~/.claude`。设置中显示实际来源位置，并允许添加文件型记忆位置和停用来源。显式路径不存在时报告 missing，不悄悄改读另一套 home。

原生 Agent 的本机记忆读取独立于其 CLI 是否安装、登录或可启动。只读文件不需要 app-server、Claude TUI、原生 fork、接收配对或执行输入权。个人来源与 Team Cross 创建的 worker、attach-client、隔离 home 分开；默认不扫描后者。现有路径解析见 [Codex home](../../../internal/nativecodex/process.go)、[Provider home](../../../internal/collab/claude.go)。

### Codex

| 来源 | 首期行为 | 类型和边界 |
| --- | --- | --- |
| `<codexHome>/memories/*.md` | 默认发现、可搜索与读取；仅直属 Markdown | 记忆文档，包括存在的 `MEMORY.md`、`memory_summary.md`、`raw_memories.md` 等；逐文件发布，文件名不构成格式保证 |
| `<codexHome>/memories/rollout_summaries/*.md` | 在“来源摘要”中按需展开；单独选择才进入发布 | 记忆来源摘要；仅直属 Markdown，不递归读取历史目录 |
| `<codexHome>/AGENTS.md`、`AGENTS.override.md`；选定项目根的同名文件 | 在“指令文件”中按需发现、单独选择 | 指令原文，展示作用位置；不声称这是某个运行会话实际加载的全部指令 |
| `<codexHome>/memories_1.sqlite` | 后续阶段；首期显示为未接入的可选来源，不查询正文 | Wake 从 `stage1_outputs` 取 `thread_id/rollout_slug/rollout_summary/raw_memory/generated_at`，属于需版本验证的内部存储 |
| `memories/skills/`、`extensions/`、`rules/*.rules`、任意其他子目录 | 不自动扫描 | 用户以后可显式添加其中的 Markdown 文件或目录；技能与执行规则不默认混入自动记忆 |
| `sessions/`、`archived_sessions/`、任意 rollout JSONL、`state_*.sqlite` | 不作为记忆正文来源 | 会话原文继续走现有会话材料流程；本功能不为补齐记忆而抓取完整会话 |

Wake 的默认 Codex 文件来源是 `memories/*.md`、`memories_1.sqlite`、全局 `AGENTS.md` 与 `rules/*.rules`，并通过通用适配发现项目根 `AGENTS.md`；上表的来源摘要与 override 文件是 Team Cross 拟增加的显式文件来源。[Wake Codex 来源](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/codex.rs#L1305)、[项目指令发现](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/mod.rs#L924)

Codex 的用户级文件可能同时包含多个项目。默认归入“跨项目记忆”，不因正文出现一个路径就把整份文件归为某项目。搜索可以命中具体段落；发布者可以为所选片段填写适用项目，这属于发布者声明。即使首期未接入数据库，也应清楚呈现“当前支持文件型记忆”，避免空列表被误解为 Codex 没有任何记忆。

### Claude Code

| 来源 | 首期行为 | 类型和边界 |
| --- | --- | --- |
| `<claudeHome>/projects/<projectKey>/memory/*.md` | 默认发现，各项目 memory 目录只读直属 Markdown | 自动记忆，包括 `MEMORY.md` 和主题文件；保留 frontmatter 原文 |
| 自定义 `autoMemoryDirectory` | 通过用户明确添加的记忆位置支持 | 不自动解释所有 settings scope、启动参数与工作区信任规则；添加后按所选文件或目录读取 |
| `<claudeHome>/CLAUDE.md`；选定项目根 `CLAUDE.md`、`CLAUDE.local.md`、`.claude/CLAUDE.md`、`AGENTS.md` | 在“指令文件”中单独选择 | 保存原文，不展开 `@import`、父目录链、子目录链或 rules 依赖，不宣称还原实际有效指令 |
| 主 Agent 与子 Agent 的其他 memory 目录、`.claude/rules/`、组织策略目录 | 不自动发现 | 子 Agent 的专属记忆发现与作用域解释后续再做；明确添加的 Markdown 可作为用户指定来源读取 |
| 项目 transcript、认证文件、设置全文、日志与缓存 | 不作为记忆正文来源 | 不沿记忆中的路径或链接自动收集 |

Claude 官方说明默认目录为 `projects/<project>/memory/`，同仓库 worktree 共享记忆，且支持 `CLAUDE_CODE_PROJECT_DIR_NAME` 和 `autoMemoryDirectory` 等覆盖。因此不能简单从“当前会话 cwd”反推唯一目录，也不能把最新会话当成记忆作者。先枚举真实存在的 memory 目录，再用已知项目元数据或用户选择关联；无法准确关联时保留“项目未识别”，仍可浏览与发布。[Claude 官方记忆存储说明](https://code.claude.com/docs/en/memory#storage-location)

Wake 用同项目目录的最新会话作为项目关联锚点，没有会话时尝试从编码目录名反推文件系统路径。Team Cross 首期不采用从文件系统根逐层反推的扫描；不把缺少会话等同于缺少记忆，也不把目录名解码猜测显示为已确认项目。[Wake Claude 项目关联](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/claude.rs#L380)

### 共同规则与限额

记忆来源配置只接受本机明确的文件或目录。自定义目录默认仅扫描直属 `.md`；需要子目录时另行选择。项目指令只在已选项目根发现，不递归扫描整个工作区。添加项目目录只登记根与固定候选文件，不自动启动该项目的 Agent。

私有 API 使用 Core 生成的不透明来源 ID，不能凭任意绝对路径读文件。读取时核对规范化路径及已登记边界；符号链接仅在目标仍位于已登记根内时跟随，外部目标需要独立登记。正文中的本地链接、图片、指令导入和历史引用都不触发自动读取或网络请求。

初始限额建议：单文件 8 MiB、单来源最多 10000 份候选、一个浏览缓存最多 64 MiB；超出时显示明确原因，按需淘汰正文缓存，缩小来源或搜索范围，不静默给出“完整结果”。目录每页 50 条，最多 200；正文分页沿用现有 Agent 响应 24 KiB 默认预算、64 KiB 上限，并保留 Web 阅读的 UTF-16 定位方式。发布继续受现有清单、单 blob、版本与空间配额约束；文件型记忆超限时拒绝冻结，不套用会话工具输出的头尾截断。

首期采用内存中的元数据索引与有界正文缓存，按需读取、搜索；不持久化整库正文，不引入全文数据库。mtime、size、文件身份用于刷新提示，内容哈希用于快照一致性。搜索覆盖不完整或来源读取失败时明确显示状态。缓存数量和性能需用合成数据验证后调整，上述数字是建议初值。

## 界面流程

### 本机浏览

在现有“资源库”下增加“协作资源”和“我的 Agent 记忆”两个范围。原有资源库接口与选择清单仍服务协作资源；私人记忆有独立数据源和选择状态。浏览页显示“仅本机可见”，可按 Codex、Claude Code、记忆或指令类型、项目或跨项目范围筛选，并搜索标题及正文。

宽屏使用列表与阅读器两栏；窄屏使用列表进入详情后返回。列表显示标题、文件名、Provider、类型、项目关联、更新时间与读取状态；正文支持 Markdown 和原文视图、标题导航、匹配位置与选段。frontmatter 字段是来源内容，不能被当成 Team Cross 自己确认的事实。系统修改时间只说明文件变化，不标成内容已验证。

设置入口为“设置与连接 → 记忆来源”，展示各来源根、类型、读取状态、启停、刷新与添加位置。停用来源移出本机索引，不撤回已经发布的材料。缺少文件、无匹配结果、读取被拒绝、格式不支持分别显示。

### 选择与发布

空间材料区将“发布会话材料”改为“发布材料”，在来源选择中提供“会话”和“Agent 记忆”。从“我的 Agent 记忆”点击发布则先选择已发起或已加入的可用空间，也可复用现有创建只读空间入口。两条路径进入同一发布流程。

1. **选择来源。** 明确选中文件，默认不选正文；点击“全文”或选取标题下的段落、代码块，也可在原文中拖选连续文本。一个文件允许多个不连续片段。
2. **冻结来源。** 携带阅读时的 sourceRevision，读取本次选定文件，核对内容哈希后生成本机快照。若先前已经选段，只能将这些坐标应用到同一修订；源变化则返回 changed，重新阅读和选择，不平移套用旧偏移。后续编辑选区基于冻结快照，后台刷新不能移动选区或替换正文；多文件分别拥有冻结时间，不宣称形成 Provider 全局原子快照。
3. **准备材料。** 可改材料标题、适用项目、备注，移除片段或遮盖选中文字。遮盖生成发布副本，原始文件保持原样。首期不开放任意改写原文；另写的说明独立于来源摘录。
4. **预览。** 展示最终将上传的全部正文与元数据、目标空间、可读成员范围、摘录或脱敏标记。只发送该预览的内容；未选部分、原文件路径、未选 frontmatter 和完整源哈希留在本机。
5. **发布。** 复用 `previewId + previewHash + requestId` 提交和发布回执。状态不明时查询原请求；没有输入权也能发布，不自动启动其他 Agent。

首期采用“一份源文件对应一份材料”；用户可以一次选择多份文件，批量操作为它们分别创建材料。每份有独立预览、请求 ID、回执和失败状态；某份失败不撤销已成功的其他份，也不自动重传。这样可以沿用同源版本规则，避免多文件集合更新时来源身份不明确。一个文件内多个片段属于同一材料的多个正文块。

发布后从结果页进入空间材料；从讨论或回复入口发起时，将成功发布的固定版本加入当前回复草稿，回复由用户另行提交。发布成功而回复未提交时，材料仍保留在空间，不声称已经完成回复。

### 空间阅读与更新

空间材料列表增加“会话”“记忆”“指令”类型标记。文档材料展示段落目录和来源说明，阅读、选中文本批注、回复附件、资源库收藏、TC 读取编号、空间请求与“交给 Agent”沿用已有动作。私人记忆不直接进入跨空间选择清单或共享运行时；发布后才获得这些协作入口。

本机刷新检测到来源变化时，只提示发布者“来源已变化”。点击更新后重新冻结并选择范围，显示增改删片段及新预览；旧范围不能准确定位时要求重新选择，不默认扩大到全文。`baseVersion` 防止并发覆盖，新版本不改旧引用。源文件消失不影响已发布内容；文件移动不猜测同源，首期按新来源处理。批注始终指向发布副本的固定版本与偏移，脱敏后不会回到未脱敏源坐标。

资源中的历史指令仅供参考，发布不授予读取发布者目录、原生会话或自动写入接收者 memory 的能力。撤回及已读副本的边界继续采用现有空间契约。

## 数据与接口设计

### 三类对象

```mermaid
flowchart LR
  S[本机记忆来源] -->|选择并读取| D[本机冻结草稿]
  D -->|摘录和脱敏| P[最终发布预览]
  P -->|明确发布| M[空间固定版本材料]
  M --> R[阅读与原文批注]
  M --> A[Agent 按需读取]
```

| 对象 | 拟保存的关键内容 | 可访问方 |
| --- | --- | --- |
| 本机来源 | 随机来源 ID、Provider、adapter、规范路径、来源种类、作用范围、项目关联依据、读取状态 | 当前 Core 的私人管理入口 |
| 本机冻结草稿 | 随机草稿 ID、来源 ID、冻结时间、完整源哈希、原文和选区映射 | 发布者本机 |
| 空间材料版本 | 材料类型、发布者、Provider、可公开来源标签、适用范围、冻结时间、正文块、已发布清单哈希 | 获准空间成员 |

本机路径映射与草稿记录以现有私有数据权限保存。新增记忆冻结草稿建议 24 小时有效，过期只清理本功能拥有的草稿及缓存，保留更新所需的最小来源映射，不动 Provider 文件、原生会话和既有会话草稿。正文哈希仅用于本机校验时不公开；公开清单只对实际发布的内容和元数据求哈希，防止未选内容通过元数据流出。

### 材料模型

现有 [MaterialContent](../../../internal/collab/material_types.go) 和 [Manifest](../../../internal/materialstore/store.go) 强制携带来源会话与 turns；[buildMaterialBundle](../../../internal/collab/material_bundle.go) 还要求来源 UUID。因此不能仅新增一个文本类型并伪造用户或助手轮次。

拟将清单设计为带类型的联合结构：`kind=conversation|document`。会话继续使用 turns/items；文档使用 source/blocks。文档 subtype 为 `agent_memory|instruction|memory_summary`，正文格式支持 Markdown 和纯文本。首期一个文档只对应一个来源文件，后续数据库一行也能映射为一个来源。

每个发布正文块有 `blockId`、可选标题、正文 Body、摘录或脱敏标记。标题不能取自未选段落，或者必须作为独立公开元数据出现在预览中。保留 `materialId/version/hash/author`、现有 inline/blob 机制与配额；同源更新对文档使用 Provider 加不透明来源关系标识，不使用原生 sessionId。来源关系标识限定到该发布者和空间，私有 sourceId 到它的映射留在本机；它是发布者的来源声明，不是文件真实性证明。

引用继续以 `kind=material` 表达，定位改为类型明确的两种坐标：会话为 `turnId/itemId`，文档为 `blockId`；二者都使用已保存文本的 UTF-16 `startOffset/endOffset/quote`。服务端严格验证偏移、引文与固定版本，拒绝混用两种坐标。原文件行号只作为本机选区辅助，不作为发布后的权威坐标。

增改删统计改为按材料类型计算；文档用块或摘录数量，不能继续显示“轮次”。版本、撤回、作者检查、请求去重、空间存储配额和上传三阶段语义保持一致，但遍历 Body 的存储与上传代码需要覆盖 document 分支。

清单 schema 应显式升级，并在协商阶段报告不支持的版本。按当前原型工程边界，不自动迁移历史 schema、不静默改写旧材料；旧文件保留，版本不支持应明确显示。实现时统一决定使用新的清单版本还是同步提升空间记录版本；不能让旧 reader 把文档材料解码成空会话。这一项是跨版本部署检查点。

### 私人读取与公共读取

以下为拟新增或扩展的接口，尚不存在：

| 入口 | 用途 |
| --- | --- |
| `GET /api/memories` / `list_local_memories` | 按 Provider、作用范围、类型、项目、关键词列出本机候选；分页只返回元数据和必要匹配片段 |
| `POST /api/memories/read` / `read_local_memory` | 以来源 ID 读取本机原文；游标绑定内容修订，源变更时明确拒绝旧游标 |
| `POST /api/publications/memory-source` / `freeze_memory_source` | 以来源 ID 和已读 sourceRevision 冻结一份记忆；变更时拒绝旧选区，不上传 |
| `POST /api/publications/preview` / `preview_publication` | 扩展类型分支；文档接收明确的选区及遮盖操作，会话保持连续轮次选择 |
| 现有 `read_publication_draft`、`publish_material`、`get_publication_status` | 共用草稿读取与最终发布，不新增第二条上传协议 |
| 现有 `read_material` | 按材料类型分派读取和坐标；所有入口只读获准版本 |

本机目录、原文、冻结入口只进入个人管理 MCP 和本机 UI 受控路由，不进入共享运行时、空间远端路由、公开 resource 或事件正文。插件面板通过 app-only 桥使用必要的私人 UI 路由，不能因为面板可浏览就将全机记忆目录注册为模型可自动枚举的资源。已发布版本继续兼容现有 material mention 与选择编号。

现有 Agent 读取默认 `answers`，并由 MCP 提前注入最近 3 轮等参数。文档没有最终答复和轮次，须在 Core 确定材料类型后选默认视图：会话保持 answers，文档默认 content，从开头顺序分页；文档提供 outline 与 block 定位。`maxBytes` 共用，turnLimit/toolOutputs 不适用于文档，显式传入时返回可理解的参数错误。不可只修改 UI 而让 Agent 默认读取结果为空。

## 改动位置

| 层次 | 现有入口或拟新增位置 | 具体改动 |
| --- | --- | --- |
| 本机来源 | 拟新增 `internal/memorysource/`；复用 Provider home 解析 | Codex 和 Claude 文件发现、项目关联、来源登记、读取限额、内容修订、缓存与搜索；不依赖运行时进程 |
| Core 私人接口 | 拟新增 `internal/collab/memory_sources.go`；[http.go](../../../internal/collab/http.go)、[types.go](../../../internal/collab/types.go) | 来源配置、列表、按需读取、错误状态；设置保存需保留已有语言和客户端字段 |
| 发布草稿 | [publication.go](../../../internal/collab/publication.go)、[publication_read.go](../../../internal/collab/publication_read.go)、[material_http.go](../../../internal/collab/material_http.go) | 文件冻结、文档选区、遮盖、预览哈希、草稿到期；保持私人路由边界 |
| 材料存储与上传 | [material_types.go](../../../internal/collab/material_types.go)、[material_bundle.go](../../../internal/collab/material_bundle.go)、[store.go](../../../internal/materialstore/store.go)、[material_upload.go](../../../internal/collab/material_upload.go) | 清单类型、通用 Body 遍历、哈希、配额、上传协商与同源更新 |
| 材料读取与批注 | [materials.go](../../../internal/collab/materials.go)、[agent_read.go](../../../internal/collab/agent_read.go)、[readview](../../../internal/readview/options.go)、AnnotationTarget | 文档正文流、分页、目录、block 定位、quote 校验和类型化变化统计 |
| 个人与共享 MCP | [materials.go](../../../internal/mcp/materials.go)、[ui.go](../../../internal/mcp/ui.go)、共享运行时工具注册 | 私人记忆工具、按类型默认读取；公共工具只扩展已发布材料能力，检查工具描述与参数校验 |
| 资源库与选择引用 | [library.go](../../../internal/collab/library.go)、[Library.tsx](../../../packages/web/src/components/Library.tsx)、[MaterialReferences.tsx](../../../packages/web/src/components/MaterialReferences.tsx) | 私人浏览范围、材料类型、文档定位、选择编号及原文跳转；检查请求与简报里的引用校验 |
| 发布与阅读界面 | [Publisher.tsx](../../../packages/web/src/components/Publisher.tsx)、[PublicationReader.tsx](../../../packages/web/src/components/PublicationReader.tsx)、[Materials.tsx](../../../packages/web/src/components/Materials.tsx)；拟新增 MemoryBrowser、DocumentReader | 来源种类选择、文件列表、片段选择、脱敏预览、批量结果、更新流程 |
| 共用前端与插件 | [types.ts](../../../packages/web/src/types.ts)、[reading.ts](../../../packages/web/src/reading.ts)、[MarkdownText.tsx](../../../packages/web/src/components/MarkdownText.tsx)、[App.tsx](../../../packages/web/src/App.tsx)、i18n 与 plugin bridge | 联合类型、UTF-16 选区、路径安全、导航、语言、主题、app-only 路由白名单 |

MarkdownText 已对链接和图片做限制，应复用其渲染边界，记忆来源里的附件路径不获得额外访问能力。前端实现后同步构建 `internal/webassets/dist/` 和 `internal/mcpassets/dist/`。菜单栏首期继续展示已发布协作资源，私人记忆完整阅读从 WebGUI 进入，无需增加 Swift 文件系统浏览器。

实现确认后，长期规则进入 [空间契约](../sources/decisions/collaboration-spaces.md)、[资源库契约](../sources/decisions/resource-library.md)、[协议](../sources/protocol.md)、[产品流程](../sources/product-flows.md) 与 [核心词汇](../sources/product-core-and-glossary.md)；概念页只补材料来源与私人来源的关系。实际验证范围进入证据入口，本设计稿不充当验收结果。

## 实施顺序与验收

第一步完成材料类型与引用协议，用合成文档贯通本机草稿、远端上传、阅读、批注、更新、撤回和现有会话回归。第二步接入 Codex 与 Claude 文件型来源和私人 API。第三步完成 WebGUI、个人 MCP、插件面板和批量发布体验。三步共同构成首期可交付闭环；不把只做出一个文件列表算作功能完成。

| 验收范围 | 必须覆盖的行为 |
| --- | --- |
| 来源发现 | 默认 home、显式覆盖、多位置、缺失、禁用、未知项目、混合项目记忆、无会话仍有 memory、自定义 Claude 路径 |
| 文件读取 | UTF-8、中文和 emoji、frontmatter、换行、长文件、空文件、权限错误、符号链接、路径穿越、读取过程中替换文件；源前后内容和元数据无非预期写入 |
| 选区与预览 | 多段、不连续片段、遮盖后的 UTF-16 坐标；未选正文、路径、标题和 frontmatter 不通过清单、错误或搜索结果流入公共接口 |
| 固定版本 | 预览后源变化不改变发布内容；刷新不改变既有选区；同源更新、baseVersion 冲突、源删除与重命名、旧版引用不漂移 |
| 三成员隔离 | 同机三个隔离 Core，A/B/C 各自发布；A 无法枚举 B 的私人目录；加入、撤销、撤回和作者权限仍成立；记录为同机环境 |
| 发布失败 | 丢响应、重复请求、hash 不一致、上传部分失败、Core 重启、批量部分成功；不重复材料、不自动扩大范围、不自动重放写入 |
| 人与 Agent | Web 原文、个人 MCP、共享运行时、TC 编号、material mention、讨论回复与空间请求都读取同一固定版本；文档默认视图不会因 answers 过滤而变空 |
| 界面 | 1440/1024/768 宽度、中英文、浅深主题、键盘选段、Markdown/原文切换、批注定位、来源异常与状态不明；真实浏览器截图复核 |

工程门槛遵循 [验证契约](../sources/validation/test-gates.md)：Go test/vet、相关 race，Web check/test/build 与两套嵌入资源。适配器用隔离合成目录验证，不为测试读取用户整份记忆。产品闭环如需真实模型读取记忆材料，仅在专门会话和仓库使用 `gpt-5.6-luna`；文件读取本身不需要模型调用。测试后关闭本次精确启动的进程。

## 后续范围

Codex 数据库单独作为后续适配：先核对目标版本的 schema、真实有内容的测试记录与 WAL 并发行为，再决定支持的表与字段。只读读取失败则明确不可用，不能假装为空。Wake 使用只读打开失败后复制 DB/WAL/SHM 的降级方式；并发复制的一致性需要另行设计和测试，不能直接作为发布快照保证。[Wake SQLite 读取](https://github.com/iAmCorey/Wake/blob/7b1fb3053e4cd9c1920b4f4445cc1e9bbbc6be6d/crates/wake-core/src/adapters/sqlite_ro.rs#L19)

子 Agent memory 自动发现、递归指令依赖解析、完整原生有效记忆还原、跨主机个人目录镜像、持久全文索引和跨 Provider 写回都不属于首期。是否推进这些项目，依据文件型闭环的实际使用反馈与各 Provider 的可验证接口决定。

当前建议集中在四个可评审取舍：首期文件型优先；记忆与指令分别标记；一份源文件对应一份材料并支持批量操作；私人来源先形成明确的发布副本，再进入现有协作资源体系。
