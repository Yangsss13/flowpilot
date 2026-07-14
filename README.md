# MiniKVX-Agent

MiniKVX-Agent 是一个基于 Go 的轻量级工作流任务执行平台。用户可以创建包含多个有序步骤的任务，后端负责校验任务定义、持久化状态，并按统一状态机执行和记录过程。

项目当前处于 v1 开发阶段，优先完成可运行、可测试、可解释的后端业务闭环，不提前堆叠未验证的中间件。

## 当前进度

已完成：

- 使用 Gin 提供任务创建、列表和详情接口。
- 提供 WorkerPool 异步任务提交和分级日志查询接口。
- 已实现 GORM + MySQL 持久化层、迁移和连接配置。
- 创建任务与全部步骤使用事务，任一写入失败则整体回滚。
- 实现 `Pending / Running / Success / Failed` 状态机和条件状态更新。
- 实现 `sleep / http_mock / shell_mock` 三种 mock 动作及参数校验。
- 实现 Task Executor：步骤顺序执行、失败即停止、失败任务重试时跳过已成功步骤。
- 状态变化和对应日志使用短事务共同提交。
- 提供 MySQL 8.4 Docker Compose 本地环境。
- 单元测试、race 测试和真实 MySQL 集成测试已于 2026-07-14 重新通过。

已完成的执行能力：

- 固定 4 个 Worker 和容量为 100 的有界内存队列。
- `/run` 入队成功返回 `202 Accepted`，队列满或关闭时返回 `503`。
- Worker 只接收 taskId，执行前重新查询 MySQL 并通过条件状态更新抢占执行权。
- 服务关闭时先停止接收新任务并排空已受理任务；超过关闭期限后才通过 context 取消执行。

开发中：

- 完整执行链路的演示和评测。

Redis 防重复锁、RabbitMQ、LLM、Embedding、Qdrant、RAG、Agent Loop、DAG、前端和 checkpoint 尚未完成，不计入当前功能。

## 技术栈

- Go 1.26
- Gin
- GORM
- MySQL 8.4
- Docker Compose

最终 v2 规划使用但尚未完成：Redis、RabbitMQ、Qdrant、OpenAI-compatible Chat/Embedding Provider、React + Vite。

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

修改 `.env` 中的开发密码。`.env` 已被 Git 忽略，不要提交真实密码。

2. 启动 MySQL：

```bash
docker compose up -d mysql
docker compose ps
```

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

停止 MySQL 容器：

```bash
docker compose down
```

不要随意添加 `-v`；`docker compose down -v` 会删除 MySQL 数据卷。

## 测试

运行单元测试：

```bash
go test ./...
```

运行真实 MySQL 集成测试前，先启动 Compose 并加载 `.env`，再设置：

```text
MINIKVX_INTEGRATION=1
```

集成测试覆盖：

- 创建任务和步骤真实落库。
- 步骤写入失败时任务事务回滚。
- 列表不加载全部步骤，详情按 `step_order` 加载步骤。
- 重复状态抢占只允许一个更新成功。
- 任务级日志与步骤级日志关联正确。
- 异步执行成功、步骤失败即停止、成功任务拒绝重跑和日志查询。
- WorkerPool 并发上限、队列满快速返回、优雅排空、超时取消及并发提交/关闭。

## 下一步

1. 完成 Redis 防重复锁和 RabbitMQ 幂等异步执行。
2. 实现受约束 LLM Agent：结构化 Planner、工具白名单、Observation、有限 replan 和简单 DAG。
3. 实现 RAG：`.txt/.md`、Chunk、Embedding、Qdrant、TopK 和来源引用。
4. 接入 MiniKV checkpoint，完成基础前端、Docker Compose 和浏览器端到端演示。
5. 完成真实依赖复验、固定评测、README、截图和最终 v2 收口，之后原则上不再扩模块。

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
