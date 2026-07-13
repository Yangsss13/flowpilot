# MiniKVX-Agent

MiniKVX-Agent 是一个基于 Go 的轻量级 Agent / Workflow 任务执行平台，目标是作为 2026 年 8 月初投递简历的主业务项目。

## v1 定位

用户可以在浏览器中创建一个多步骤任务，后端按照步骤执行任务，维护任务状态和执行日志，并支持查看任务执行过程。

## v1 技术栈

- Go
- Gin
- GORM
- MySQL
- React 或 Vue 前端，具体以后端联调方便为准

## v1 功能

- 创建任务。
- 任务包含多个步骤。
- 查询任务列表。
- 查询任务详情。
- 执行任务。
- 查看执行日志。
- 状态流转校验，避免非法状态跳转。
- 执行日志分级：`INFO`、`WARN`、`ERROR`。
- 状态流转：
  - `Pending`
  - `Running`
  - `Success`
  - `Failed`

## 7 月底增强项

- Redis 防重复执行锁。
- 健康检查接口：`GET /api/health`，检查 MySQL / Redis / RabbitMQ 可用性。
- Demo 数据或一键演示脚本，方便 README 和面试演示。
- Docker Compose。
- README、截图、接口文档。
- 基础测试。
- 基础评测：
  - Redis 锁并发防重复验证。
  - 任务提交接口耗时记录。

## 8 月上旬增强项

- RabbitMQ 异步任务执行。
- Worker 消费 taskId 后驱动任务状态流转。
- 消费 ACK。
- 任务执行幂等校验。
- 失败重试：`max_retry = 3`，超过次数标记 `Failed`。
- 死信队列可作为后续增强。
- 异步化评测：
  - 同步执行 vs RabbitMQ 异步投递的接口耗时对比。
  - WorkerPool 不同 worker 数下的任务吞吐对比。

## 最终 v2 规划

v1/v1.5 是业务闭环版和可投递版，当前规划内的最终 v2 还包括：

- 简单 DAG：步骤依赖、拓扑执行和环检测。
- RAG 知识库：文档切块、Embedding、Qdrant、TopK 检索、答案生成和来源引用。
- 工作流节点：至少支持 HTTP Tool 和 RAG Query。
- MiniKV checkpoint：保存任务运行步骤和 runtime state，用于恢复演示。
- RAG 评测：小型问答集、Hit@3、检索耗时和引用命中情况。
- 完整前端、Docker Compose、健康检查、项目截图和一键 Demo。

## 暂缓或后续

- 复杂分支、动态 DAG 和多 Agent 协作。
- 多租户权限。
- WebSocket。
- 复杂前端交互。
- 分布式部署。
- GraphRAG、混合检索、Rerank 和复杂 PDF/OCR。

## 简历表达草稿

```text
MiniKVX-Agent：基于 Go 的轻量级 Agent 工作流任务执行平台
技术栈：Go, Gin, GORM, MySQL, Redis, RabbitMQ, Qdrant, RAG, Docker Compose, React

- 基于 Gin + GORM 实现任务创建、步骤编排、任务执行、状态流转和日志查询接口，使用 MySQL 持久化任务、步骤与执行记录。
- 设计 Pending / Running / Success / Failed 状态机，统一管理任务和步骤状态，避免执行过程状态混乱。
- 引入 WorkerPool 执行任务步骤，将任务提交与具体执行逻辑解耦，提升多任务并发处理能力。
- 基于 Redis 实现任务执行互斥锁，避免同一任务被重复触发导致重复执行和日志污染。
- 引入 RabbitMQ 异步调度任务执行请求，接口层投递 taskId 后快速返回，由 Worker 消费消息并通过状态校验保证重复消费幂等。
- 实现 RAG 工作流节点，支持文档切块、Embedding、Qdrant TopK 检索和来源引用，并通过 Hit@3 与检索耗时评测验证效果。
- 使用 MiniKV 保存任务 runtime checkpoint，为任务恢复提供轻量状态存储。
- 提供前端可视化页面，支持浏览器完成任务创建、执行、状态查看和日志追踪。
- 通过评测对比同步执行与 RabbitMQ 异步投递模式下的接口耗时，并评估不同 Worker 数对任务吞吐的影响。
```

RabbitMQ 未完成前，简历技术栈和亮点里先不要写 RabbitMQ。
RAG、DAG、MiniKV checkpoint 未真实跑通前，同样不要提前写入简历。

## 评测规划

详细评测计划见 [docs/evaluation-plan.md](./docs/evaluation-plan.md)。

优先做三项：

1. Redis 锁并发防重复验证：并发触发同一任务，预期只有一个请求获得执行权。
2. RabbitMQ 前后接口耗时对比：证明任务提交接口从同步等待变为快速返回。
3. WorkerPool 并发度对比：评估 worker 数为 1/2/4/8 时的任务吞吐和总耗时。

## 最小演示闭环

README 和前端演示要覆盖这条链路：

```text
创建任务 -> 配置步骤 -> 执行任务 -> 状态流转 -> 写分级日志 -> 前端查看 -> Redis 防重复 -> RabbitMQ 异步执行 -> 评测证明效果
```
