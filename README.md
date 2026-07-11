# MiniKVX-Agent

MiniKVX-Agent 是一个基于 Go 的轻量级 Agent / Workflow 任务执行平台
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

## 暂缓或后续

- 复杂 DAG。
- 多租户权限。
- WebSocket。
- 复杂前端交互。
- 分布式部署。

## 评测规划

优先做三项：

1. Redis 锁并发防重复验证：并发触发同一任务，预期只有一个请求获得执行权。
2. RabbitMQ 前后接口耗时对比：证明任务提交接口从同步等待变为快速返回。
3. WorkerPool 并发度对比：评估 worker 数为 1/2/4/8 时的任务吞吐和总耗时。

## 最小演示闭环

README 和前端演示要覆盖这条链路：

```text
创建任务 -> 配置步骤 -> 执行任务 -> 状态流转 -> 写分级日志 -> 前端查看 -> Redis 防重复 -> RabbitMQ 异步执行 -> 评测证明效果
```
