# MiniKVX-Agent v1 / v1.5 / v2 需求

## 核心业务闭环

1. 用户创建任务。
2. 每个任务包含多个步骤。
3. 用户点击执行任务。
4. 后端按步骤执行。
5. 每个步骤产生执行日志。
6. 前端展示任务状态和日志。

v1 目标是跑通业务闭环，v1.5 加 Redis 防重复执行锁和基础评测；最终 v2 完成 RabbitMQ、简单 DAG、RAG、MiniKV checkpoint 和完整评测。不要一开始就把所有中间件堆进去。

## 数据模型草案

### tasks

- `id`
- `name`
- `description`
- `status`
- `created_at`
- `updated_at`

### task_steps

- `id`
- `task_id`
- `name`
- `step_order`
- `action_type`
- `action_payload`
- `status`
- `created_at`
- `updated_at`

### execution_logs

- `id`
- `task_id`
- `step_id`
- `level`
- `message`
- `created_at`

### 可选增强字段

后续为支持重试和幂等，任务和步骤可以补：

- `retry_count`
- `max_retry`
- `last_error`
- `started_at`
- `finished_at`

## API 草案

- `POST /api/tasks`
  - 创建任务。
- `GET /api/tasks`
  - 查询任务列表。
- `GET /api/tasks/:id`
  - 查询任务详情，包括步骤。
- `POST /api/tasks/:id/run`
  - v1：触发 WorkerPool 执行任务。
  - v2：只投递 taskId 到 RabbitMQ，由 Worker 消费执行。
- `GET /api/tasks/:id/logs`
  - 查询执行日志。
- `GET /api/health`
  - 健康检查，返回 MySQL / Redis / RabbitMQ 可用性。

## v1 执行规则

- v1 可以先按 `step_order` 顺序执行，不强行做复杂 DAG。
- 状态必须通过统一状态机流转，不允许业务代码到处直接改状态。
- 推荐状态流转：
  - `Pending -> Running -> Success`
  - `Pending -> Running -> Failed`
  - `Failed -> Running`，表示重试
  - `Success` 默认不能重新变成 `Running`
- 每个 step 可以先使用 mock action：
  - `sleep`
  - `http_mock`
  - `shell_mock`
- 任一步骤失败，任务状态变成 `Failed`。
- 所有步骤成功，任务状态变成 `Success`。
- 执行日志分级：`INFO`、`WARN`、`ERROR`。

## v1 演示要求

必须提供一条最小 Demo 链路：

1. 创建示例任务。
2. 创建 2-3 个步骤。
3. 点击执行。
4. 前端看到状态从 `Pending` 到 `Running`，最后到 `Success` 或 `Failed`。
5. 前端能看到分级执行日志。

可以通过 README curl 示例、seed 数据或前端 demo button 实现。

## v1.5：Redis 防重复执行锁

目标：

- 防止同一个 taskId 被重复点击执行。
- 避免重复执行步骤和污染执行日志。
- 保留数据库状态校验，Redis 锁不是唯一防线。

建议 key：

```text
task:lock:{taskId}
```

执行流程：

1. 接收到执行请求。
2. 使用 Redis 尝试加锁。
3. 加锁失败，说明任务正在执行，直接返回“任务执行中”。
4. 加锁成功，进入执行流程。
5. 任务结束后释放锁，或依赖过期时间兜底释放。

面试要准备：

- Redis 锁为什么要设置过期时间？
- 只用 `SETNX` 有什么问题？
- 服务执行到一半挂了怎么办？
- 为什么还需要数据库状态校验？

## v1.5：健康检查

目标：

- 提供 `GET /api/health`，方便本地演示和排查。
- 返回服务自身、MySQL、Redis、RabbitMQ 的可用性。
- v1.5 如果 RabbitMQ 未接入，可以先返回 `not_configured`。

评测要求：

- 并发触发同一个 taskId 多次。
- 预期只有 1 个请求真正进入执行流程。
- 其他请求返回“任务正在执行”或等价错误。
- README 记录请求数量、成功执行次数、被拦截次数。

## v2：RabbitMQ 异步执行

目标：

- 接口层快速返回。
- 执行层异步消费任务。
- 解耦任务提交和任务执行。
- Worker 消费消息后必须先做任务状态幂等校验。

流程：

```text
POST /api/tasks/:id/run
        |
        v
投递 taskId 到 RabbitMQ
        |
        v
Worker 消费消息
        |
        v
检查任务状态，避免重复消费导致重复执行
        |
        v
查询任务和步骤
        |
        v
执行步骤，写状态和日志
```

面试要准备：

- 为什么这个项目适合用 MQ？
- 消息投递失败怎么办？
- 消费失败怎么办？
- 重复消费怎么保证幂等？
- Ack 是什么时候确认？
- Worker 消费到重复消息时为什么不能直接执行？

## v2：失败重试

目标：

- 对可重试失败做有限次数重试。
- 建议 `max_retry = 3`。
- 每次失败记录 `retry_count` 和 `last_error`。
- 超过最大次数后标记任务或步骤为 `Failed`。

注意：

- 重试不是无限循环。
- 重试要和日志、状态机、MQ ACK 策略一起设计。

评测要求：

- 对比 v1 同步执行和 v2 RabbitMQ 异步投递的 `POST /api/tasks/:id/run` 响应耗时。
- 记录平均耗时、P95 耗时和样本数量。
- 评测不同 worker 数量下的任务总耗时和吞吐变化。
- 评测结果必须来自真实运行，简历上不能写虚构数字。

## 后续增强

- Redis 任务执行锁。
- MiniKV 保存 runtime checkpoint，例如 `checkpoint:task:{id} -> running_step=3,status=running`。
- 更完整的压测报告和可视化图表。

## 最终 v2：简单 DAG

- 步骤支持声明前置依赖。
- 创建或执行前检查依赖是否合法。
- 使用拓扑排序决定可执行顺序。
- 检测到环时拒绝任务执行并返回明确错误。
- 暂不实现动态 DAG、复杂条件分支和多 Agent 协作。

## 最终 v2：RAG 工作流节点

- 支持 `.txt` 和 `.md` 文档，PDF/OCR 后续再做。
- 固定长度 Chunk + overlap。
- Embedding Provider 接口可配置。
- 使用 Qdrant 保存和检索向量。
- 查询返回 TopK 片段、生成结果和来源引用。
- RAG 节点可以作为工作流步骤被 Worker 执行。
- 详细范围见 `docs/rag-plan.md`。

## 最终 v2：MiniKV Checkpoint

- 每完成一个步骤，保存任务当前步骤和运行状态。
- 服务恢复后读取 checkpoint，展示从最近步骤恢复或进入待恢复状态。
- MySQL 保存业务事实，MiniKV 保存运行时 checkpoint，两者职责分开。

## 最终 v2 完成标准

- 浏览器能完成创建 DAG 工作流、执行、查看状态和日志。
- 至少一条 RAG 工作流能完成知识入库、检索、生成和来源引用。
- RabbitMQ 重复消息不会导致任务重复执行。
- MiniKV checkpoint 至少完成一条恢复演示。
- Docker Compose 能启动所需基础服务。
- README 包含截图、架构、启动方式、评测数据和限制。
