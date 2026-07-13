# MiniKVX-Agent

MiniKVX-Agent 是一个基于 Go 的轻量级工作流任务执行平台。用户可以创建包含多个有序步骤的任务，后端负责校验任务定义、持久化状态，并按统一状态机执行和记录过程。

项目当前处于 v1 开发阶段，优先完成可运行、可测试、可解释的后端业务闭环，不提前堆叠未验证的中间件。

## 当前进度

已完成：

- 使用 Gin 提供任务创建、列表和详情接口。
- 使用 GORM + MySQL 持久化任务、步骤和执行日志。
- 创建任务与全部步骤使用事务，任一写入失败则整体回滚。
- 实现 `Pending / Running / Success / Failed` 状态机和条件状态更新。
- 实现 `sleep / http_mock / shell_mock` 三种 mock 动作及参数校验。
- 实现 Task Executor：步骤顺序执行、失败即停止、失败任务重试时跳过已成功步骤。
- 状态变化和对应日志使用短事务共同提交。
- 提供 MySQL 8.4 Docker Compose 本地环境。
- 覆盖领域、Service、Handler、Executor 单元测试和真实 MySQL 集成测试。

开发中：

- 固定并发 WorkerPool。
- `POST /api/tasks/:id/run` 异步提交接口。
- `GET /api/tasks/:id/logs` 日志查询接口。
- 完整执行链路的演示和评测。

Redis 防重复锁、RabbitMQ、前端和更复杂的工作流能力尚未完成，不计入当前功能。

## 技术栈

- Go 1.26
- Gin
- GORM
- MySQL 8.4
- Docker Compose

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
| `POST` | `/api/tasks/:id/run` | 开发中 | 提交任务到 WorkerPool |
| `GET` | `/api/tasks/:id/logs` | 开发中 | 查询执行日志 |

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

## 下一步

1. 完成 WorkerPool 和任务提交接口。
2. 完成执行日志查询与端到端演示。
3. 增加 Redis 任务执行锁和并发防重复验证。
4. 补充健康检查、Demo、基础评测和前端页面。
5. v2 再引入 RabbitMQ，评测同步执行与异步提交的接口耗时差异。

## 项目边界

当前实现是用于学习和演示任务编排、状态机、事务、并发控制与异步执行的轻量项目，不是生产级分布式调度系统。
