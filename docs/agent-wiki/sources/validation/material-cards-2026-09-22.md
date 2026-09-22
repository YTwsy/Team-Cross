# 2026-09-22 材料卡片内阅读验证

## 环境与范围

- `main` 工作区，以 `fe0f32e` 为基线；对应本次材料卡片、阅读工具栏和嵌入资源改动，未发布安装版本。
- macOS 14.8.5 / arm64，Go 1.27.1，Node 24.18.0，pnpm 11.19.0。
- 使用 [独立浏览器 fixture](../../../../internal/collab/library_browser_test.go) 的合成历史和真实 Core/存储。在专用空间中另外发布第二份材料及其修订版，覆盖不同标题与公开轮数；没有调用真实模型。

行为来源见 [上下文阅读](../product-flows.md#上下文阅读)，代码入口见 [Materials.tsx](../../../../packages/web/src/components/Materials.tsx)。

## 工程检查

- Go 全量 test / vet 通过；`internal/collab`、`internal/mcp`、`internal/sharing`、`internal/nativecodex`、`internal/nativeclaude`、`internal/service`、`internal/cliinstall` 的 race test 通过。
- Web check、8 个文件共 110 项测试和 production build 通过；已刷新 `internal/webassets/dist` 并构建当前 CLI。
- 更新既有回归，覆盖多卡片按需读取和切换、唯一标题和收起入口、展开状态及其控制目标、旧版本标题/轮数/选择/收藏一致性；原有分页缓存、批注、阅读标签与资源库回归继续通过。
- Vite 仍提示主 bundle 超过 500 kB；本次没有增加依赖或调整拆包。

## 浏览器结果

1. 正文、目录和阅读操作位于对应材料卡片内部。展开卡片占满列表宽度，其他卡片保留摘要；标题和收起入口不再在列表下方重复出现。
2. 第二份材料从版本 2 切到版本 1 后，卡片标题同步恢复旧标题，公开范围从 2 轮变为 1 轮。选择与收藏旧版本后，再切回版本 2，版本 2 仍保持未选择、未收藏。
3. 从“协作上下文”点击材料批注的“查看原位置”，自动回到材料标签，展开正确卡片并高亮固定版本原文。
4. 在实际 CSS 视口 `1440×900`、`1024×900`、`768×900` 的深浅主题下检查 DOM：页面、卡片和阅读器没有水平溢出，按钮及版本选择未越出卡片，展开卡片与列表同宽。另检查 768 宽度的长标题与收起按钮没有重叠，正文仍使用 15px 阅读排版。
5. 已保存并查看默认视口的深浅截图。指定视口覆盖时，截图工具出现缩放/空白异常，未将这些截图作为对应尺寸的视觉证据；六组尺寸结论限于上述 DOM 实测。浏览器控制台未出现 error。

有效截图和尺寸记录位于忽略目录 `bin/material-card-evidence/`：`materials-light-default.png`、`materials-dark-default.png`、`viewport-checks.json`。

## 边界与收尾

用户原有 Vite 页面已热更新为新布局，其正文请求仍显示原先的“服务暂时不可用”。完整正文流程的成功证据来自专用 fixture，不能据此声称用户运行中的正文请求故障已修复。

本次只修改 WebGUI 与文档，没有更改材料协议、公开范围或访问权限。完成后关闭专用浏览器页和 fixture，恢复临时视口与主题模拟，保留用户原有页面和服务；没有启动原生客户端、执行真实模型或跨设备网络验收。
