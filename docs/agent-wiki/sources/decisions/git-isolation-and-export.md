---
title: Git 隔离与 patch 导出
kind: decision
status: accepted
---

# Git 隔离与 patch 导出

## 决策

Team Cross 捕获原始 repository 的 HEAD、branch、porcelain status、staged/unstaged
binary patch 和用户明确选中的 untracked 文件。随后从 baseline commit 创建一个 detached
worktree，并把捕获内容恢复到其中。所有 managed Agent 只在这个 worktree 中工作。

Team Cross 只提供查看 diff、下载 patch 和显示 worktree 路径；不会自动打开目录，也不会
把结果 apply、commit、cherry-pick 或 merge 回原始 checkout。

## 原因

交接工具需要同时保留真实 Git 基线与未提交上下文，但接收者或 Agent 的操作不应污染
发起者仍在使用的工作区。独立 worktree 让 Codex 与 Claude 看到同一份连续文件状态，
同时让最终结果可以相对固定 baseline 确定性导出。

## 不变量

- 有 baseline commit 的 Thread 只有一个隔离 worktree；unborn Thread 没有 worktree。
- worktree 从 capture 时的 HEAD 建立，不从随后变化的 branch tip 建立。
- staged 与 unstaged patch 使用 `--binary` 语义捕获。
- untracked 文件只有在用户选择且未超限时携带内容；超限项保留元数据。
- 单个 untracked 文件最多 5 MiB，单轮携带总量最多 20 MiB。
- 最终 export 包含相对 baseline 的 tracked diff 和仍存在的 untracked regular file。
- unborn repository 可以创建只读 Thread，但在首个 commit 出现前不创建 managed Agent。
- Git 子进程忽略继承的 Git 路由/全局/系统配置，禁用 hooks、fsmonitor、textconv
  与内容 filters；不能让不可信 `.gitattributes` 在导入、查看或导出时执行主机程序。
  依赖 LFS/自定义 filter 的内容必须先由用户明确物化，Team Cross 不自动运行它们。
- Share 的代码为已封存 Round 的冻结投影，不使用实时 export 混入后来工作；历史 Round
  继续与离线包建立新 Thread，遵循
  [审阅与接力](session-review-and-continuation.md)中的对象闭包与物化安全门槛。

## 影响

Agent 的文件写入会立即反映在 Thread diff 中，但原 checkout 的 `git status` 不会因这些
写入变化。用户若要采纳结果，必须显式下载 patch、依据显示的路径进入 worktree，或自行
执行 Git 操作。

## 重新打开条件

只有在产品明确引入经过确认的 apply/commit 工作流，并能证明不会覆盖原工作区、不会
误用陈旧 baseline、且有独立回滚与验收契约时，才重新讨论自动回写。

## 事实来源

- `internal/gitstate/capture.go`
- `internal/gitstate/worktree.go`
- `internal/server/threads.go`
- `internal/gitstate/capture_test.go`
