# FlowPilot

FlowPilot 是一个基于 Go 的可追踪 AI 工作流执行平台。用户可以创建包含多个有序步骤的任务，后端负责校验任务定义、持久化状态，并按统一状态机执行和记录过程。

项目当前处于 v1 开发阶段，优先完成可运行、可测试、可解释的后端业务闭环，不提前堆叠未验证的中间件。

## 当前进度

已完成：

- 使用 Gin 提供任务创建、列表和详情接口。
- 使用 RabbitMQ 持久化 taskId 消息，并由 Consumer 通过 WorkerPool 异步执行任务。
- 使用 Redis 执行锁协调同一 taskId 的并发执行，并保留 MySQL 条件状态更新作为最终判断。
- 已实现 GORM + MySQL 持久化层、迁移和连接配置。
- 创建任务与全部步骤使用事务，任一写入失败则整体回滚。
- 实现 `Pending / Running / Success / Failed` 状态机和条件状态更新。
- 实现 `sleep / http_mock / shell_mock` 三种 mock 动作及参数校验。
- 实现 Task Executor：步骤顺序执行、失败即停止、失败任务重试时跳过已成功步骤。
- 状态变化和对应日志使用短事务共同提交。
- RabbitMQ Consumer 使用手动 ACK；瞬时基础设施错误最多重试 3 次，业务失败和重复消息不自动重试。
- 实现结构化 Planner 校验核心：最多 5 步、工具白名单、严格参数、依赖检查、DAG 环检测和有限 replan 决策。
- 实现通用 OpenAI-compatible Chat Provider，可通过环境变量使用硅基流动，协议、异常响应、超时和密钥保护已通过本地 HTTP 测试。
- 提供 MySQL 8.4、Redis 7.4 和 RabbitMQ 4 Docker Compose 本地环境。
- 单元测试、race 测试和真实 MySQL/Redis/RabbitMQ 集成测试已通过。

已完成的执行能力：

- 固定 4 个 Worker 和容量为 100 的有界内存队列。
- `/run` 在 RabbitMQ 确认持久消息后返回 `202 Accepted`，消息服务不可用时返回 `503`。
- 消息只包含 taskId；Consumer 取出后进入 WorkerPool，再依次经过 Redis 锁、MySQL 状态检查和 Task Executor。
- 任务处理完成后才 ACK；关闭期间被取消的消息会 NACK 并重新入队。

开发中：

- 完整执行链路的演示和评测。

Agent API、Embedding、Qdrant、RAG、Agent Loop、前端和 checkpoint 尚未完成。Chat Provider 尚未使用真实硅基流动账号复验，因此当前 API 还不会发起模型调用。

## 技术栈

- Go 1.26
- Gin
- GORM
- MySQL 8.4
- Redis 7.4
- RabbitMQ 4
- Docker Compose

最终 v2 规划使用但尚未完成：Qdrant、OpenAI-compatible Embedding Provider、React + Vite。

## 架构

```text
HTTP Request
    ↓
Handler          解析 JSON、转换 HTTP 状态码
    ↓
Service          校验业务规则、构造领域对象
    ↓
Repository       执行事务和数据库读写
    ↓
MySQL            保存任务、步骤和执行日志

Task Executor    编排任务和步骤状态
    ↓
Step Executor    执行 sleep/http_mock/shell_mock

POST /run → RabbitMQ 持久消息 → Consumer（手动 ACK）
         → WorkerPool → Redis 执行锁 → Task Executor
         → MySQL 条件状态更新与日志
```

核心数据关系：

```text
Task 1 ── N TaskStep
Task 1 ── N ExecutionLog
TaskStep 1 ── N ExecutionLog
```

## 状态机

```text
Pending ──→ Running ──→ Success
                  │
                  └────→ Failed ──→ Running
```

- `Success` 当前为终态，不能直接重新执行。
- 状态更新同时校验旧状态，例如 `WHERE id = ? AND status = 'Pending'`。
- 条件更新可避免多个执行者同时获得同一任务的执行权。

## API

| 方法 | 路径 | 状态 | 说明 |
|---|---|---|---|
| `POST` | `/api/tasks` | 已完成 | 创建任务及有序步骤 |
| `GET` | `/api/tasks` | 已完成 | 查询轻量任务列表 |
| `GET` | `/api/tasks/:id` | 已完成 | 查询任务详情和有序步骤 |
| `POST` | `/api/tasks/:id/run` | 已完成 | 提交 taskId 到 WorkerPool，成功返回 `202` |
| `GET` | `/api/tasks/:id/logs` | 已完成 | 按时间顺序查询执行日志 |

### 创建任务

```bash
curl -X POST http://127.0.0.1:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Daily report",
    "description": "Generate a daily report",
    "steps": [
      {
        "name": "Wait for data",
        "action_type": "sleep",
        "action_payload": {"duration_ms": 100}
      },
      {
        "name": "Build report",
        "action_type": "http_mock",
        "action_payload": {"status": 200}
      }
    ]
  }'
```

创建成功返回 `201 Created`，任务和步骤初始状态均为 `Pending`。

```bash
curl http://127.0.0.1:8080/api/tasks
curl http://127.0.0.1:8080/api/tasks/1
```

## 本地运行

1. 创建本地配置：

```bash
cp .env.example .env
```

修改 `.env` 中的开发密码，并按硅基流动模型广场中当前可用的模型填写 `AI_API_KEY` 和 `AI_CHAT_MODEL`。`.env` 已被 Git 忽略，不要提交真实密码或 API Key。

2. 启动 MySQL、Redis 和 RabbitMQ：

```bash
docker compose up -d mysql redis rabbitmq
docker compose ps
```

RabbitMQ 管理页面默认位于 `http://127.0.0.1:15673`，使用 `.env` 中的本地开发账号登录。

3. 将 `.env` 中的变量加载到当前终端后启动服务。

PowerShell：

```powershell
Get-Content .env | ForEach-Object {
    if ($_ -and -not $_.StartsWith('#')) {
        $pair = $_.Split('=', 2)
        Set-Item -Path ("Env:" + $pair[0]) -Value $pair[1]
    }
}
go run ./cmd/server
```

服务默认监听 `http://127.0.0.1:8080`。

停止依赖容器：

```bash
docker compose down
```

不要随意添加 `-v`；`docker compose down -v` 会删除 MySQL 和 RabbitMQ 数据卷。

## 测试

运行单元测试：

```bash
go test ./...
```

运行真实 MySQL/Redis/RabbitMQ 集成测试前，先启动 Compose 并加载 `.env`，再设置：

```text
FLOWPILOT_INTEGRATION=1
```

集成测试覆盖：

- 创建任务和步骤真实落库。
- 步骤写入失败时任务事务回滚。
- 列表不加载全部步骤，详情按 `step_order` 加载步骤。
- 重复状态抢占只允许一个更新成功。
- 任务级日志与步骤级日志关联正确。
- 异步执行成功、步骤失败即停止、成功任务拒绝重跑和日志查询。
- WorkerPool 并发上限、队列满快速返回、优雅排空、超时取消及并发提交/关闭。
- Redis 锁冲突、过期、安全释放，以及 10 个并发执行者只有一个进入底层执行器。
- RabbitMQ 持久发布确认、手动 ACK、NACK 重新入队、最多 3 次重试和非法消息丢弃。
- 同一 taskId 发布 10 条重复 RabbitMQ 消息时，任务和步骤只执行一次。

当前 Redis 锁的有效期固定为 5 分钟，暂未实现自动续期；当前演示任务应控制在该时间内。重复 taskId 仍可能占用 RabbitMQ 和本地 WorkerPool 的队列空间，但 Redis 锁和 MySQL 条件更新会阻止重复业务执行。

当前 RabbitMQ 客户端未实现断线自动重连；重试耗尽后会确认消息并记录服务端错误，暂未增加死信队列。系统不对任意外部副作用承诺绝对 exactly-once。

## 下一步

1. 接入 Agent API，并跑通结构化 Planner、工具调用、Observation、有限 replan 和简单 DAG 的完整闭环。
2. 实现 RAG：`.txt/.md`、Chunk、Embedding、Qdrant、TopK 和来源引用。
3. 接入 MiniKV checkpoint，完成基础前端、Docker Compose 和浏览器端到端演示。
4. 完成真实依赖复验、固定评测、README、截图和最终 v2 收口，之后原则上不再扩模块。

## 最终 v2 完成标准

- 用户输入目标后，LLM 生成经过校验的结构化计划。
- 系统只允许调用 `rag_query` 和白名单 `http_request`。
- 工具结果形成 Observation，模型决定继续、有限 replan、完成或失败。
- RabbitMQ 重复消息不会造成重复执行。
- RAG 返回 TopK 片段、最终回答和来源引用。
- 简单 DAG 支持依赖校验和环检测。
- MiniKV 保存并演示 runtime checkpoint。
- 浏览器可查看目标、计划、步骤、日志、来源和最终答案。
- 浏览器可导入 `.txt/.md` 知识资料、提交 Agent 目标、查看任务列表，并轮询运行状态。
- 前端具备加载、空数据、失败和后端不可用状态，模型 API Key 不进入浏览器。
- README、测试、评测和 Docker Compose 能支撑他人复现。

前端只承担项目操作和演示，不做登录权限、拖拽编排、可视化 DAG 编辑器、复杂图表或营销首页。

## 项目边界

当前实现是用于学习和演示任务编排、状态机、事务、并发控制与异步执行的轻量项目，不是生产级分布式调度系统。

在 LLM 规划、工具调用和 Observation 闭环真实跑通前，项目只能称为 Workflow 执行平台，不能宣称已完成 LLM Agent。
