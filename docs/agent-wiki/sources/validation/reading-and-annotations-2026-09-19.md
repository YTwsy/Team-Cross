# 2026-09-19 材料阅读与原文旁批注验收

本记录针对 `codex/collaboration-spaces` 上、基于 `cf75ddd` 的本次工作区改动。环境为 macOS 14.8.5 arm64、Go 1.27.1、pnpm 11.19.0，以及 Codex 内置 Chromium 浏览器。它不是安装渠道或跨设备验收。

## 实现范围

- 已发布材料与协作上下文共用 Markdown/GFM 阅读组件，采用 `react-markdown`、`remark-gfm`、本地按需加载的 Shiki 与 Floating UI。提供轮次目录、原文切换、工具折叠、代码复制与专注阅读。
- 正文采用页面滚动；同一消息的连续材料分页合并后排版。当前详情页缓存最多六个已读版本，保留分页及位置；撤回清除对应缓存。协作历史更新先提示，读者确认后显示新内容。
- 选区映射回原始 UTF-16 偏移，保留固定版本的原文校验。宽屏在正文右侧编辑；不超过 1100 CSS 像素时在所选段落、代码块或列表之后展开。草稿、回复与批注面板共用现有状态及 API。
- “引用材料”按标题/作者搜索，最新版本优先，历史版本展开选择；点击预览才读取片段，选中后以固定版本标签展示。发布入口使用“发布会话材料”。

用户流程见 [上下文阅读](../product-flows.md#上下文阅读)，实现入口见 [WebGUI 短页面](../../wiki/concepts/webgui-and-mcp.md)。

## 工程门槛

最终执行结果：

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall` | 通过 |
| `pnpm --filter @teamcross/web check` | 通过 |
| `pnpm --filter @teamcross/web test` | 6 个文件、79 项通过 |
| `pnpm --filter @teamcross/web build` | 通过，已更新 `internal/webassets/dist` |
| `go build -o bin/teamcross ./cmd/teamcross` | 通过 |
| 文档相对链接、代码路径与 `git diff --check` | 通过 |

Go 默认缓存写入受沙箱限制，最终命令使用本次任务在 `/private/tmp` 下的独立 `GOCACHE`。初次全量测试中的 `TestStableAppPathSurvivesCaskCommandRemoval` 出现版本查询超时；单独复跑及最终完整套件均通过，未修改该用例或放宽超时。

[阅读回归](../../../../packages/web/src/test/reading.test.tsx) 覆盖重复中文、emoji、跨 Markdown 节点、转义/实体、CRLF、行内及围栏代码、分页合并、代码 DOM/选区稳定、原始 HTML/外部图片处理、就地草稿失败重试、窄屏焦点与草稿、固定版本搜索及按需预览、材料缓存。既有批注、发布、生命周期、产品流程和上下文刷新测试同步通过。

## 浏览器与 Core 闭环

使用 [材料浏览器 fixture](../../../../internal/collab/materials_browser_test.go) 提供合成 Markdown 会话，浏览器访问实际生产构建；来源/原生运行时为测试替身，材料与讨论由三个独立 Core 经本机 TLS 处理。

实际完成并目视复核截图：

- 1440、1024、768 **实际 CSS 像素**，各自浅色和深色；读取浏览器实际视口校准缩放。六种组合均无页面水平溢出，正文无固定高度的内层滚动。
- 简短轮次目录、正文标题/列表/表格、代码高亮、工具折叠、780 像素正文的专注阅读。
- 选取第二处 `中文🙂` 后保存，Core 保留 UTF-16 `127..131`，另外两个成员读到相同固定版本引用；语法高亮代码中的选区也打开对应原文卡。
- 最终构建中选取加粗原文，在 1024/768 宽度下直接位于该段落之后，并聚焦正文输入框；扩大到 1440 后切换为正文右侧卡片，草稿保留。
- 搜索并附上 v1 材料，保存批注后显示原文高亮与讨论入口；原文旁保存一条回复，三个 Core 均读到一条批注、一条回复和对应材料引用。页面控制台没有 warning/error。

测试结束关闭本次浏览器页面、恢复视口覆盖，并以各 fixture 的 `finish` 文件让其退出，确认所有 fixture 正常完成。没有启动真实模型或用户原有原生客户端。

## 边界

本次不包含全文搜索、版本差异视图、极长历史虚拟滚动、跨设备网络、真实模型或新一轮 TUI/Desktop 验收。原始 HTML 不执行，外部图片不自动请求；无法可靠映射的特殊 Markdown 选区需切换原文。草稿及阅读缓存只在当前详情页保留。材料公开范围、撤回语义和 Agent 的按需读取边界保持现有契约。
