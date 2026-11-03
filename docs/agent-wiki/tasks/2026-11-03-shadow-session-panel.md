# 分身会话与任务面板

## 范围与判断

2026-11-03，按用户对当前空间“记忆与任务”的批注改进实际界面，让 S1/S2 与 W1/W2 的关系、内容、状态及创建时机直接可见。实施分支 `codex/shadow-activity`，基线 `c56946a`；没有修改主工作区。

- 入口改为“会话与任务”，打开独立详情弹窗，保留关闭、Esc 与焦点返回。
- 当前 S、已交接的历史段、独立 Work 分开展示。S 的忙碌只读现有 `busy`（Core 仅按当前主 thread 的活动 Run 计算），不借用 W 的状态。
- S/W 内容用分类条展示；真实上下文用量另外使用原生 `usageValid/inputTokens/contextWindow`。不推算各类别 token 占比。
- Work 按创建时间和 ID 稳定排序，排队任务保留编号位置，但只有已有 `threadId` 才展示 W 编号。旧 coordination 不标 W。展开可看完成条件、原生 ID 与结果；任务版本、错误、记忆及本机操作继续保留。
- 文件保留的原文、任务结果、记忆与交接笔记单独说明。列表尚未加载完整详情时显示加载，离线时注明最后同步记录。
- 使用现有共享投影字段；没有增加后端字段、原生会话、任务状态、模型调用或调度流程。

## 普通关注来源错误仍在

本次只解释该问题，没有改变观察协议。重新读取当前测试空间的 owner `attention.json`，讨论和材料的两个 focus 仍为 `active` 且保存错误：`观察结果无效，未推进处理位置: 证据未在本批或此前有效认识中提供`。

普通关注原文已送入观察；模型返回 evidence 时自行增加 `requestId` 或 `turnId/itemIds`，与 Core 提供的整个准确引用不一致，保存被拒绝。处理水位未因此推进；后来的直接 @ 回复成功和 S 中“都已处理”的自然语言不能证明该错误消失。具体来源与修复方向见 [11 月 2 日实测](2026-11-02-shadow-activity-live-demo.md)。后续宜由 Core 提供短引用 ID、模型选择、Core 还原 SourceRef，保留来源授权与版本校验。

## 验证

macOS arm64、Node 24.18.0、Go 1.27.1。以下运行在基线加本次未提交代码上，构建后的安装来源另记：

- `go test ./cmd/... ./internal/...` 与对应 `go vet` 通过。工作树现有 `bin/go-mod` 缓存影响 `./...` 的递归范围；以上覆盖全部产品 Go 包，没有删除用户缓存。
- `go test -race ./internal/collab ./internal/shadow ./internal/mcp` 全部通过。
- Web check 1403 条中英消息通过；完整 Web 29 文件、260 项通过，最后的离线说明／加载保护后复跑分身 9 项通过。
- 两套嵌入资源构建成功；最终资源额外通过 `internal/mcpassets` 和 `internal/webassets` 检查。
- 合成状态浏览器 fixture：真实 Core、实际 production 资源，S2 空闲、W1 执行、W2 等待、W3 完成、排队但未建会话；历史 S1、任务条件、原生 ID 和结果展开准确。Esc 关闭恢复“会话与任务”焦点，聊天输入可用。
- 实际浏览器默认约 566 × 712 窗口，中／英文与浅／深色检查无横向溢出。没有改变用户正式 Core 的语言或主题。
- 本次未调用真实模型；上述合成状态不证明新的调度能力或两台 Mac 行为。

复现入口见 [验证契约](../sources/validation/test-gates.md#本机数字分身)。截图在 `output/acceptance/shadow-session-panel-20261103/screenshots/`（仅本地保存、Git 忽略），包含浅色总览、任务详情、结果展开和英文深色。测试日志在 `.local-backups/shadow-session-panel-20261103/`（仅本地保存）。测试目录已写入 `finish` 并关闭测试页面。

## 安装

已从干净提交 `d89d96a58f98b11941163a0ca27bfa4c39f06db4` 构建并安装 `0.2.6-dev.shadow-session-panel.20261103`，build 216，arm64 / macOS 14+，ad-hoc 签名、未公证。`make release` 和 `make verify-release` 通过；安装检查覆盖 checksum、DMG、图标、签名完整性、App/CLI 版本一致、双副本与生命周期，不包含真实插件宿主验证。

- 当前安装：`/Applications/Team Cross.app`；其 helper 与产物 helper SHA-256 完全一致，`/opt/homebrew/bin/teamcross` 继续指向该 helper。
- 产物：`dist/release/0.2.6-dev.shadow-session-panel.20261103/`；清单确认 `dirty:false`。
- DMG SHA-256：`3d36f1c7aef1ecd5901f5e98666855d3893ec8ac5fa36cd677cd071e8040846c`。
- CLI tar SHA-256：`69288854170854cf2cdf5fa26757518975ae9239ec0bda66f8dd851710f146fe`。
- 原 App 备份：`.local-backups/install/2026-11-03-shadow-session-panel/original-installed.app`；只停止已核对 PID 的菜单栏壳，Core 使用自身升级协议。原空间和数据目录保留。
- 安装后通过 Core 读取与实际浏览器核对：原测试分身当前 S1 的原生 ID 保持 `01a19c5a-d1cd-7d82-a3c0-f136a864f74d`，10 条共享消息保留，Work 为 0，在线且主轮空闲；模型仍为原生确认的 `gpt-5.6-luna / openai / low`。面板正确显示 S1、49,512 / 258,400 tokens、尚未创建 W；本人提问与本机诊断入口仍可见。
- 当前两个普通关注来源错误仍存在。本次未发送模型输入、创建 Work 或修改关注处理结果。
- `installed-s1.png` 留于上述本地截图目录；浏览器已为用户打开真实新版面板。两个隔离浏览器 fixture 均正常退出，测试页已关闭，仅保留正常安装服务和交付页面。

本次仅本地提交和安装，没有推送、tag 或发布远端 Release。

## 同日：任务提问区块修正

用户指出“提问与接续”的复选框独占一行，且无法理解是否控制任务执行。原因是该复选框继承全局输入框的 `width: 100%`，同时原标题和“包含已结束任务”没有说明这是只读筛选。

改为“任务中的提问”，用“未结束任务 / 全部记录”两项筛选，解释登记提问、等待条件及后续动作，并明确筛选不启停任务。默认仍调用 `history=false`，全部记录调用 `history=true`；切换清空分页，不改变后端流程。空状态区分“未结束任务暂无提问”和“还没有任务提问记录”。详情标题改为“提问与关联任务”。实际读取原测试分身，两种范围均为 0 条；不是有任务被此开关暂停。

验证：Web check 1409 条消息、29 文件 262 项测试、两套嵌入构建，以及全部产品 Go 包 test/vet 和 collab/shadow/mcp race 通过。新增交互回归覆盖两种筛选、分页重置、仅 GET、两个空状态；既有 Core 回归继续覆盖任务结束后进入历史。复用提问浏览器 fixture，增加一条真实保存的测试讨论回复及合成已完成任务关联，确认历史记录按筛选出现／消失，原任务状态不变，详情与 Esc 返回正常。中英文、浅深色、566 × 712 窗口检查通过；本次未启动模型或更改真实空间的任务。

截图保存在 `output/acceptance/shadow-question-filters-20261103/screenshots/`，日志保存在 `.local-backups/shadow-question-filters-20261103/`（均仅本地保存）。隔离 fixture 已结束，测试标签已关闭，临时窗口尺寸已恢复。

已从干净提交 `3691d4a2dc868e9e3b58559ccbf77917dd5902a1` 构建并安装界面修正版 `0.2.6-dev.shadow-session-panel.20261103.1`，build 218。`make release` 与 `make verify-release` 均通过，仍为 arm64、ad-hoc、未公证。DMG SHA-256 为 `4e908177947a14760db66840fb1336798d23fcc4020d14a1ea54e5241163d44a`，CLI tar SHA-256 为 `16d0a3fb2dc605512ddc02070a5d0f285d02f27a42399c27f2f48fcecc0aae74`。

前一版 App 保存在 `.local-backups/install/2026-11-03-shadow-question-filters/original-installed.app`。安装后 helper 字节与产物一致，Core 正常运行；原测试空间的 S1、共享消息和任务保持。实际安装页面已核对两种空状态和筛选切换，并为用户打开新版区块；`installed-empty.png` 保存在本节截图目录。无新增模型输入或任务，无远端推送。
