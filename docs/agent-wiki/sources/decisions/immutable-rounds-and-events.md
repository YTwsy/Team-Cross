---
title: 不可变 Round 与 durable event
kind: decision
status: accepted
---

# 不可变 Round 与 durable event

## 决策

Thread 是一条追加式交接历史。Round 创建后不可修改；Round 0 保存初始 capture，后续
完成的 Agent Turn 和 Provider 切换追加新 Round。细粒度实时活动写入 durable event log，
每条 event 使用 SQLite 单调 `seq`，并携带事务完成后的 Thread `revision`。

## 原因

多轮交接需要回答“这条结论基于哪版代码和哪些 evidence”。重写最新状态会丢失因果链；
每轮都重复完整 bundle 又会造成体积和上下文浪费。不可变 Round + 追加 event 能同时提供
可追溯历史、实时 UI 和确定性的恢复 cursor。

## 不变量

- Round sequence 在同一 Thread 内单调递增。
- 已存在的 Round 不提供 update 路径。
- 完成或中断的 managed Turn 都可以封存 Round；运行中的 token delta 不直接生成 Round。
- Provider 切换先封存 outgoing summary、patch、事件范围与 evidence 引用，再创建目标 Run。
- summary 失败时使用确定性 context manifest，Team Cross Core 不自行编造语义摘要。
- event `seq` 是 SSE cursor；Thread `revision` 是乐观写入版本，两者不能互换。
- SSE 重连按 `Last-Event-ID`/`after` 补发持久化事件，而不是依赖进程内消息队列。

## 影响

UI 可以把 Round 呈现为连续时间线，同时按需展开细粒度 event。主机重启后历史与 cursor
仍存在；临时 Share runtime 虽失效，Thread 档案不会消失。

## 重新打开条件

如果未来需要 compaction，只能增加派生摘要或索引，不能破坏原 Round 与 event 的可追溯
身份。任何归档格式都必须保留 sequence、revision 和对象引用关系。

## 事实来源

- `internal/storage/schema.go`
- `internal/storage/events.go`
- `internal/storage/threads.go`
- `internal/server/rounds.go`
- `internal/server/collaboration.go`
