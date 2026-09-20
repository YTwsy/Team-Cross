# 2026-09-20 内容寻址材料与按条读取验收

本记录对应 `codex/release-v0.2.0-rc.1` 上与本文同一提交的实现。环境为 macOS 14.8.5 arm64、Go 1.27.1、Node 24.18.0、pnpm 11.19.0、Vite 7.1.7 与 Vitest 3.2.4。没有更新安装渠道，没有使用用户已有会话，也没有启动真实模型。

## 实现范围

- 协作持久化切换到 `schema:3`。`collaboration.json` 只保存成员、执行、批注和材料版本元数据；不可变清单放在 `manifests/<sha256>.json`，大正文放在空间内的 `blobs/<prefix>/<sha256>`。小条目内联，清单和 blob 都在落盘时校验内容哈希。旧 `schema:2` 原样留盘但不加载，也不迁移。
- 第一版保持“一条 item 一个 blob”：单 blob 最多 8 MiB；超长条目保存头尾、明确省略标记以及原始/省略 UTF-16 长度。单版本唯一 blob 上限 32 MiB，空间活动版本唯一 blob 上限 256 MiB。
- 远端发布使用 `negotiate → PUT blob → commit`。缺失正文只写入 24 小时 staging，提交时才提升到正式 CAS；每空间最多 32 个未完成上传、256 MiB 暂存正文。协商只复用当前成员自己未撤回版本仍引用的 blob，撤回或跨作者内容不会成为存在性探针；提交时再次检查复用资格。
- `read_material` 默认完整返回用户/助手消息，超过 1500 个 UTF-16 字符的工具输出只返回精确前缀和总长。正文流以 16000 为软预算、24000 为轮对齐硬上限；`turnId + itemId + startOffset` 可按条每页读取 16000 字且游标不跨 item。轮次目录只在首页返回。
- 私有冻结草稿使用独立 CAS，并向个人 MCP 增加 `read_publication_draft`；共享运行时仍只有 `read_annotations`、`reply_to_annotation`、`list_materials`、`read_material` 四个工具。
- 活协作历史从 Codex app-server 或 Claude JSONL 页面投影成同一种 outline/segment 读取结构，不持久化为材料版本。`read_context kind=history` 通过 `pageCursor + turnId + itemId + startOffset` 按条读取，服务端剔除 reasoning 和白名单外字段，并返回 `contentHash` 供 WebGUI 避免无变化重渲染。
- WebGUI 的材料和最近对话共用轮次阅读、折叠预览、按条展开和批注定位。按条全文加载后会清除临时折叠提示；超过单页的 item 保留“继续读取这条输出”。

完整限额与路由见 [协议](../protocol.md#已发布会话材料)，长期取舍见 [协作空间契约](../decisions/collaboration-spaces.md)。

## 自动化门槛

最终执行结果：

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过，包括 `internal/materialstore`、材料上传、范围隔离和活历史投影回归 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall` | 通过 |
| `pnpm --filter @teamcross/web check` | 通过 |
| `pnpm --filter @teamcross/web test` | 6 个文件、83 项通过 |
| `pnpm --filter @teamcross/web build` | 通过，已更新 `internal/webassets/dist` |
| `go build -o bin/teamcross ./cmd/teamcross` | 通过 |
| `git diff --check` | 通过 |

[材料存储与协议回归](../../../../internal/collab/materials_test.go) 和 [CAS 单元测试](../../../../internal/materialstore/store_test.go) 覆盖确定性 hash、损坏/缺失 blob、幂等提交、staging 后提交、跨作者与撤回后的协商、恢复上传、8 MiB 头尾省略、轮对齐、目录只返回一次、按条游标范围和 `schema:2` 忽略。[活历史回归](../../../../internal/collab/history_projection_test.go) 覆盖工具折叠、reasoning 剔除、pageCursor 定位及按条游标不跨消息；[MCP 回归](../../../../internal/mcp/server_test.go) 覆盖历史 item 坐标透传与草稿工具。Web 回归覆盖材料和活历史两条展开流程、分页合并、折叠提示清理、批注定位与刷新保持。

## 真实浏览器闭环

使用扩展后的 [材料浏览器 fixture](../../../../internal/collab/materials_browser_test.go) 启动嵌入式生产 Web 资源、三个独立 Core、真实本机 TLS 路由和一个合成执行 fork。Provider 历史是测试替身，工具正文为 2394 个 UTF-16 字符。

在隔离的 Playwright Chromium 中实际完成：

1. 已发布材料首页完整显示两轮对话，工具条显示“共 2394 字 · 已显示 1487 字”，正文默认折叠，并提供“读取完整输出”。
2. 点击后由材料 item 端点加载剩余正文；按钮消失，临时“已折叠” notice 清除。展开过程可同时读到“工具日志开头”和尾部“200 次请求完成，测试汇总已保存”。
3. 活协作“最近对话”展示三轮完整对话和相同的折叠预览；点击后通过 `pageCursor + turnId + itemId + startOffset` 读取同一条全文，状态清理与已发布材料一致。
4. 1440×1000 和 768×900 CSS 像素下检查生产布局。768 宽度时 `innerWidth=768`、`documentScrollWidth=753`、`bodyScrollWidth=753`，没有页面级水平溢出；工具过程自身保留有界滚动。
5. 浏览器控制台为 0 error、0 warning。折叠、完整材料和活历史展开截图均已人工复核；截图保存在本次被 Git 忽略的 `output/playwright/` 验收目录，不作为长期产品资产提交。

验收结束后关闭隔离浏览器，并通过每个 fixture 的显式 `finish` 文件正常结束测试服务器、三个 Core 和运行时替身；未按进程名批量终止用户客户端。

## 边界与后续

本次是同一台 Mac 上的合成历史、三个 Core 与真实浏览器闭环，不是三台 Mac、真实 LAN/Tailcat、真实 Codex/Claude 模型或原生 TUI/Desktop 验收。32 MiB 来源冻结仍在内存中规范化；边分页边写草稿 blob 是后续优化。首版不做 item 隐式分块、材料全文搜索、逐字版本 diff 或撤回后的自动 blob 回收。撤回会立即停止读取授权并释放活动配额，但不承诺立即删除物理 blob。

内容 hash 不是读取能力：所有 blob 读取仍须先由当前空间、未撤回固定版本或本机私有草稿解析引用。活协作历史只统一读取投影，不获得材料的不可变版本、去重或撤回语义。
