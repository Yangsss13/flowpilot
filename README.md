# FlowPilot

## 前端控制台

前端位于 `web/`，使用 React、Vite 和 TypeScript。开发服务器会把 `/api` 代理到 `http://127.0.0.1:8080`，因此请先按下文启动后端，再运行：

```bash
cd web
npm install
npm run dev
```

浏览器访问 `http://127.0.0.1:5173`。前端不读取任何模型密钥；`AI_API_KEY` 及模型配置只保留在后端 `.env`。

提交前可执行完整前端检查：

```bash
npm run lint
npm run typecheck
npm run build
```

FlowPilot 是一个基于 Go 的可追踪 AI 工作流执行平台。用户可以创建包含多个有序步骤的任务，后端负责校验任务定义、持久化状态，并按统一状态机执行和记录过程。

项目当前处于 v1 开发阶段，优先完成可运行、可测试、可解释的后端业务闭环，不提前堆叠未验证的中间件。

## 当前进度

已完成：

- 使用 Gin 提供任务创建、列表和详情接口。
- 使用 RabbitMQ 持久化 taskId 消息，并由 Consumer 通过 WorkerPool 异步执行任务。
- 使用 Redis 执行锁协调同一 taskId 的并发执行，并保留 MySQL 条件状态更新作为最终判断。
- 已实现 GORM + MySQL 持久化层、迁移和连接配置。
- 创建任务与全部步骤使用事务，任一写入失败则整体回滚。
- 实现 `Pending / Queued / Running / Success / Failed` 状态机和条件状态更新。
- Workflow 支持真实 `rag_query` 知识库检索，并保存文档片段、相似度和页码/幻灯片/时间轴等 Observation；`sleep / http_mock / shell_mock` 保留为执行引擎测试动作。
- 实现 Task Executor：步骤顺序执行、失败即停止、失败任务重试时跳过已成功步骤。
- 状态变化和对应日志使用短事务共同提交。
- RabbitMQ Consumer 使用手动 ACK；瞬时基础设施错误最多重试 3 次，业务失败和重复消息不自动重试。
- 实现结构化 Planner 校验核心：最多 5 步、工具白名单、严格参数、依赖检查、DAG 环检测和有限 replan 决策。
- 实现通用 OpenAI-compatible Chat Provider，可通过环境变量使用硅基流动，协议、异常响应、超时和密钥保护已通过本地 HTTP 测试。
- 实现 Agent 任务创建 API：目标经模型规划和服务端校验后，任务、步骤及 DAG 依赖使用同一个 MySQL 事务落库。
- 实现异步知识摄取：支持 `.txt/.md/.pdf/.docx/.pptx`，原文件受控存储，RabbitMQ 独立队列负责解析、结构化切块、批量 Embedding 和 Qdrant 索引。
- 实现异步音视频摄取：支持 `.mp3/.wav/.m4a/.mp4/.mov/.webm`，由 FFmpeg/ffprobe、whisper.cpp 和 Tesseract 完成校验、转写、关键帧 OCR、时间轴合并与向量索引。
- 文档、不可变版本和摄取 Job 分表管理；支持 checksum 去重、失败重试、重启恢复、新版本原子切换、旧向量清理和最终一致删除。
- 文档解析器 v2 会合并同一页、幻灯片或章节内相邻的短段落，使标题与正文保留在同一检索 Chunk；解析器版本升级后允许相同原文件重新索引。
- 音视频 Job 支持进度、超时、并发限制和取消；首次摄取被取消时文档进入 `Canceled`，已有可检索版本的新摄取被取消时仍保持 `Ready`。取消、版本替换及删除会清理派生音频、关键帧、转写文件和向量。
- 实现 Agent Loop：RabbitMQ 异步运行、任务类型分发、工具 Observation、continue/replan/finish/fail 决策和最终答案持久化。
- Agent Decision 会拒绝重复执行已有成功 Observation 的步骤；模型返回不合法业务决策时，最多进行一次携带校验原因的修复请求，不会无限重试。
- 接入 MiniKV Agent checkpoint：保存计划、Observation、replan 次数、决策位置和当前工具步骤，并在重启后区分安全恢复与外部副作用歧义。
- MiniKV 使用 WAL 恢复和操作系统级目录锁；进程异常退出后锁自动释放，不需要人工删除残留标记文件。
- 提供 MySQL 8.4、Redis 7.4、RabbitMQ 4 和 Qdrant 1.18 Docker Compose 本地环境。
- 单元测试、race 测试和真实 MySQL/Redis/RabbitMQ/Qdrant 集成测试已通过。

已完成的执行能力：

- 固定 4 个 Worker 和容量为 100 的有界内存队列。
- `/run` 先原子占用为 `Queued`，再在 RabbitMQ 确认持久消息后返回 `202 Accepted`；同一任务并发提交只有一个请求成功，其余返回 `409`。
- 消息只包含 taskId；Consumer 取出后进入 WorkerPool，再依次经过 Redis 锁、MySQL 状态检查和 Task Executor。
- 任务处理完成后才 ACK；关闭期间被取消的消息会 NACK 并重新入队。

浏览器端控制台已经覆盖任务总览、Workflow 创建、Agent 创建与运行、知识资料异步上传、Job 进度/取消/重试、语义检索、任务步骤 Observation 和执行日志。真实硅基流动 Chat/Embedding、RabbitMQ、MySQL、Redis 和 Qdrant 端到端链路已完成验收。

## 技术栈

- Go 1.26
- Gin
- GORM
- MySQL 8.4
- Redis 7.4
- RabbitMQ 4
- Qdrant 1.18
- MiniKV
- Docker Compose
- React + Vite + TypeScript

FFmpeg、whisper.cpp 和 Tesseract 是音视频摄取的可选本地运行时，不随仓库下载大型模型。

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
Step Executor    执行 rag_query 或测试动作，并返回结构化 Observation
    ↓
RAG Service      Query Embedding → Qdrant 检索 → 当前版本过滤

POST /run → RabbitMQ 持久消息 → Consumer（手动 ACK）
         → WorkerPool → Redis 执行锁 → Task Dispatcher
         → Workflow Executor / Agent Runner
         → MySQL 条件状态更新与日志

POST /api/agent/tasks → Chat Provider → Planner 校验
                      → MySQL 事务保存 Agent Task / Steps / Dependencies

POST /api/knowledge/documents → 受控对象存储 → MySQL Document/Version/Job → 202
                              → RabbitMQ knowledge queue
                              → 文档：Parser 子进程 → 结构化 Chunk
                              → 媒体：ffprobe → 音轨 → ASR → 关键帧/OCR → 时间轴合并
                              → Embedding → Qdrant Upsert
POST /api/knowledge/search    → Query Embedding → Qdrant threshold search
                              → MySQL 当前版本过滤 → Sources

Agent Runner → Decision → rag_query / allowlisted http_request
             → Observation → continue / replan / finish / fail
             → MiniKV runtime checkpoint
```

核心数据关系：

```text
Task 1 ── N TaskStep
Task 1 ── N ExecutionLog
TaskStep 1 ── N ExecutionLog

Document 1 ── N DocumentVersion
Document 1 ── N IngestionJob
DocumentVersion 1 ── N IngestionJob
DocumentVersion 1 ── N DocumentArtifact
```

## 状态机

```text
Pending ──→ Queued ──→ Running ──→ Success
              │             │
              │             └────→ Failed ──→ Queued
              └── 发布失败时回到 Pending / Failed
```

- `Success` 当前为终态，不能直接重新执行。
- 状态更新同时校验旧状态，例如 `WHERE id = ? AND status = 'Pending'`。
- 条件更新可避免多个执行者同时获得同一任务的执行权。

## API

| 方法 | 路径 | 状态 | 说明 |
|---|---|---|---|
| `POST` | `/api/tasks` | 已完成 | 创建任务及有序步骤 |
| `GET` | `/api/tasks` | 已完成 | 查询轻量任务列表 |
| `GET` | `/api/tasks/stats` | 已完成 | 查询全量任务状态与类型统计 |
| `GET` | `/api/tasks/:id` | 已完成 | 查询任务详情和有序步骤 |
| `POST` | `/api/tasks/:id/run` | 已完成 | 提交 taskId 到 WorkerPool，成功返回 `202` |
| `GET` | `/api/tasks/:id/logs` | 已完成 | 按时间顺序查询执行日志 |
| `POST` | `/api/agent/tasks` | 已实现 | 根据目标生成并持久化受约束计划 |
| `POST` | `/api/agent/tasks/:id/run` | 已实现 | 将 Agent 任务持久化发布到 RabbitMQ，成功返回 `202` |
| `POST` | `/api/knowledge/documents` | 已实现 | 上传文档并返回 `202`、document/version/job ID |
| `POST` | `/api/knowledge/documents/:id/versions` | 已实现 | 为已有文档创建异步摄取的新版本 |
| `POST` | `/api/knowledge/documents/:id/reindex` | 已实现 | 为 Ready 文档的当前版本创建幂等的重新索引 Job |
| `GET` | `/api/knowledge/documents` | 已实现 | 分页并按状态、格式、文件名筛选文档 |
| `GET` | `/api/knowledge/documents/:id` | 已实现 | 查询当前版本、Chunk 数和最近 Job |
| `DELETE` | `/api/knowledge/documents/:id` | 已实现 | 标记删除并最终一致清理原文件、元数据和向量 |
| `GET` | `/api/knowledge/jobs/:id` | 已实现 | 查询阶段、进度和安全错误信息 |
| `POST` | `/api/knowledge/jobs/:id/retry` | 已实现 | 仅将 Failed Job 原子重置为 Queued |
| `POST` | `/api/knowledge/jobs/:id/cancel` | 已实现 | 取消 Queued/Running Job；运行中的子进程会收到取消信号 |
| `POST` | `/api/knowledge/search` | 已实现 | 按最低相似度返回 TopK 片段和页码/幻灯片/章节/媒体时间轴 |
| `GET` | `/api/capabilities` | 已完成 | 返回当前启用的 Agent 工具和知识能力 |
| `GET` | `/health` | 已完成 | HTTP 进程存活检查 |
| `GET` | `/ready` | 已完成 | MySQL、Redis、RabbitMQ、Qdrant 和启用能力的就绪检查 |

任务列表支持 `page`、`page_size`、`task_type`、`status` 和 `query`：

```json
{
  "items": [{"id": 1, "name": "task", "status": "Queued", "step_count": 2}],
  "total": 123,
  "page": 1,
  "page_size": 20
}
```

列表不返回步骤、Observation 或完整结果；任务详情仍通过 `/api/tasks/:id` 查询。默认每页 20 条，最大 100 条。

### 创建 Workflow

```bash
curl -X POST http://127.0.0.1:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "生成项目面试报告",
    "description": "按固定问题收集知识证据，再生成可交付报告",
    "steps": [
      {
        "name": "检索项目架构",
        "action_type": "rag_query",
        "action_payload": {"query": "FlowPilot 的核心后端链路是什么？", "top_k": 3, "min_score": 0.5}
      },
      {
        "name": "检索并发控制",
        "action_type": "rag_query",
        "action_payload": {"query": "项目如何避免任务被重复执行？", "top_k": 3, "min_score": 0.5}
      },
      {
        "name": "生成最终报告",
        "action_type": "llm_summarize",
        "action_payload": {
          "instruction": "基于前面全部证据生成结构清晰的面试报告，保留来源引用；证据不足时明确说明。"
        }
      }
    ]
  }'
```

创建成功返回 `201 Created`，任务和步骤初始状态均为 `Pending`。`rag_query` 只有在 Embedding 与知识检索能力实际启用时才允许创建；`llm_summarize` 还要求 Chat 模型可用。能力不足时 Service 返回安全的 `400`，前端也不会展示对应动作。

真正有业务价值的 Workflow 不是让用户手工填写“成功或失败”，而是把可重复的工作固化为确定流程：前序 `rag_query` 收集真实证据，最后一个 `llm_summarize` 只依据成功的检索 Observation 生成带引用报告。汇总步骤必须位于最后且前面至少有一个检索步骤；最终报告与步骤成功状态在同一个 MySQL 短事务中写入 `task.result`。模型只负责整理证据，不负责临时改变步骤，因此同一流程比 Agent 更可控、更容易复现和审计。

Workflow 与 Agent 可以使用相同的知识检索能力，但控制权不同：Workflow 的查询和顺序由用户固定，任一步失败后停止；Agent 的计划和下一步由模型在服务端校验规则内决定。两者保持独立任务类型和执行器。

Workflow 步骤成功时，状态与 Observation 在同一个 MySQL 短事务中提交，避免步骤已经是 `Success` 却没有可解释结果。检索 Observation 示例：

```json
{
  "query": "FlowPilot 的核心后端链路是什么？",
  "results": [{
    "source": "README.md",
    "section": "架构",
    "page": 2,
    "text": "POST /run 经过 RabbitMQ、WorkerPool、Redis 锁和任务执行器。",
    "score": 0.91
  }]
}
```

Workflow 创建接口限制任务名 100 字符、描述 500 字符、步骤名 100 字符、最多 100 步、单步 action payload 最大 64 KiB、请求体最大 1 MiB。未知 JSON 字段和非法 action 配置返回 `400`，不会再进入 MySQL 后变成 `500`。

```bash
curl http://127.0.0.1:8080/api/tasks
curl http://127.0.0.1:8080/api/tasks/1
```

配置 AI 环境变量后，可以创建 Agent 任务：

```bash
curl -X POST http://127.0.0.1:8080/api/agent/tasks \
  -H "Content-Type: application/json" \
  -d '{"goal":"根据已导入资料总结退款政策"}'
```

未同时配置 `AI_API_KEY` 和 `AI_CHAT_MODEL` 时，Agent API 不注册；普通 Workflow API 仍可正常使用。Agent 任务使用独立类型，当前旧的 `/api/tasks/:id/run` 会拒绝执行它。

创建后可异步运行 Agent 任务：

```bash
curl -X POST http://127.0.0.1:8080/api/agent/tasks/1/run
```

配置 `AI_API_KEY` 和 `AI_EMBEDDING_MODEL` 后，可以导入并检索知识资料：

```bash
curl -X POST http://127.0.0.1:8080/api/knowledge/documents -F "file=@policy.md"
curl -X POST http://127.0.0.1:8080/api/knowledge/documents -F "file=@interview.mp4"
curl -X POST http://127.0.0.1:8080/api/knowledge/search \
  -H "Content-Type: application/json" \
  -d '{"query":"退款期限是什么？","top_k":5,"min_score":0.55}'
```

上传接口不再同步等待向量化，成功受理返回：

```json
{
  "document_id": 12,
  "version_id": 19,
  "job_id": 35,
  "status": "Queued",
  "deduplicated": false
}
```

前端应轮询 `/api/knowledge/jobs/35`，只有 Job 进入 `Success` 才表示资料可检索。音视频 Job 会依次经过 `probe / extract_audio / transcribe / keyframes / ocr / merge / embedding / indexing`；可通过 `POST /api/knowledge/jobs/35/cancel` 取消排队中或运行中的摄取。

媒体搜索结果会携带 `start_ms`、`end_ms`、`start_time` 和 `end_time`，可展示为 `interview.mp4 + 00:03:12–00:03:40`。`GET /api/capabilities` 的 `knowledge` 字段会返回异步摄取标记、当前实际支持格式、各格式大小上限及最大媒体时长。只有 FFmpeg、whisper.cpp、Whisper 模型、Tesseract 和所需语言数据全部可用时，服务才会发布媒体摄取能力。

未配置 Embedding 模型时，Knowledge API 不注册，不影响普通 Workflow 和 Agent 计划创建接口。

## 本地运行

1. 创建本地配置：

```bash
cp .env.example .env
```

修改 `.env` 中的开发密码，并按硅基流动模型广场中当前可用的模型填写 `AI_API_KEY`、`AI_CHAT_MODEL` 和 `AI_EMBEDDING_MODEL`。如需 `http_request`，使用逗号分隔的 `HTTP_TOOL_ALLOWED_HOSTS` 明确允许目标域名。`.env` 已被 Git 忽略，不要提交真实密码或 API Key。

当前已验证的开发配置是 `Qwen/Qwen3-30B-A3B-Instruct-2507` 和 `BAAI/bge-m3`。模型可用性可能随账号和平台策略变化，应以硅基流动控制台为准。`CHECKPOINT_DIR` 默认为 `./data/checkpoints`；知识原文件默认保存到 `./data/knowledge/objects`。整个 `data/` 目录已被 Git 忽略，不会把原文件提交到仓库。

### 本地媒体运行时

媒体摄取不引入云服务，需要自行安装并配置：

- FFmpeg/ffprobe：探测容器、编码、时长和分辨率，提取单声道 WAV 与定时关键帧。
- whisper.cpp + `ggml-small.bin` 多语言模型：离线 ASR 并输出时间戳；不要把模型提交到 Git。
- Tesseract + `chi_sim`、`eng` 语言数据：识别关键帧中的中英文文字。

Windows 下建议将可执行文件、模型、语言数据和临时目录放在不含中文的纯 ASCII 路径（例如 `%LOCALAPPDATA%/FlowPilotMedia`），再设置 `.env.example` 中的 `FFMPEG_PATH`、`FFPROBE_PATH`、`WHISPER_CPP_PATH`、`WHISPER_MODEL_PATH`、`TESSERACT_PATH`、`TESSERACT_DATA_DIR` 和 `KNOWLEDGE_MEDIA_TEMP_DIR`。部分 whisper.cpp Windows 构建在中文路径下可能崩溃。

默认上传上限为音频 100 MiB、视频 500 MiB，最大时长 2 小时、最大分辨率 3840×2160。`KNOWLEDGE_MEDIA_CONCURRENCY`、`KNOWLEDGE_ASR_CONCURRENCY`、`KNOWLEDGE_OCR_CONCURRENCY` 控制并发，线程数、阶段超时、关键帧间隔和最大帧数也都可通过 `.env.example` 中的变量调整。HTTP 上传只创建异步 Job，不等待这些 CPU 密集步骤完成。

2. 启动 MySQL、Redis 和 RabbitMQ：

```bash
docker compose up -d mysql redis rabbitmq qdrant
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

不要随意添加 `-v`；`docker compose down -v` 会删除 MySQL、RabbitMQ 和 Qdrant 数据卷。

## 测试

运行单元测试：

```bash
go test ./...
```

运行真实 MySQL/Redis/RabbitMQ/Qdrant 集成测试前，先启动 Compose 并加载 `.env`，再设置：

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
- Workflow `rag_query` 能力开关、严格 payload 边界、真实检索输出、Observation 原子持久化，以及检索失败后后续步骤不执行。
- WorkerPool 并发上限、队列满快速返回、优雅排空、超时取消及并发提交/关闭。
- Redis 锁冲突、过期、安全释放，以及 10 个并发执行者只有一个进入底层执行器。
- RabbitMQ 持久发布确认、手动 ACK、NACK 重新入队、最多 3 次重试和非法消息丢弃。
- 同一 taskId 发布 10 条重复 RabbitMQ 消息时，任务和步骤只执行一次。
- Agent 目标校验、计划失败不落库、HTTP 错误映射、任务类型隔离，以及真实 MySQL 中任务、步骤和 DAG 依赖的事务落库。
- Unicode 安全切块、Embedding 批处理和响应校验、Qdrant 建库/写入/查询协议，以及真实 Qdrant 中两份资料的 TopK 检索。
- 知识文件格式、大小、MIME 欺骗、文件名路径穿越和 ZIP Bomb 边界。
- PDF 页码、DOCX 标题层级、PPTX 幻灯片/备注解析和结构化切块。
- 20 个并发 Worker 抢占同一 Job 只有一个成功、20 个并发 retry 只有一个成功，以及 Running Job 重启恢复。
- 真实 MySQL/RabbitMQ/Qdrant 异步摄取、checksum 去重、失败重试、新版本替换、旧向量清理和删除后不可检索。
- FFmpeg 真实探测、音轨与关键帧提取，whisper.cpp 时间戳转写，Tesseract OCR，媒体 Job 并发抢占、运行中取消、重启恢复、派生物持久化与删除清理。
- Agent Loop 正常执行、工具失败观察、依赖阻止、有限 replan、最终答案落库、Workflow/Agent 分发，以及白名单 HTTP 与跨域重定向阻止。
- MiniKV checkpoint 关闭重开后的恢复、终态清理、安全点续跑、执行中断不自动重放，以及中断状态在 MySQL 中原子失败。

当前 Redis 锁的有效期固定为 5 分钟，暂未实现自动续期；当前演示任务应控制在该时间内。重复 taskId 仍可能占用 RabbitMQ 和本地 WorkerPool 的队列空间，但 Redis 锁和 MySQL 条件更新会阻止重复业务执行。

当前提交链路采用“MySQL 原子占用 `Queued` → RabbitMQ Publisher Confirm”的最小方案；发布明确失败时会条件回退到原状态。MySQL 与 RabbitMQ 仍不是同一个事务：确认结果不确定或回退失败时，需要客户端重试或人工处理 `Queued` 任务。若要做到无人值守的可靠最终投递，下一阶段仍需要 Transactional Outbox 和后台投递器。

`/ready` 会真实 Ping 本地基础设施并检查启用能力是否完成装配，但不会每次调用付费 AI 接口，因此不能替代模型供应商的外部可用性监控。响应只包含组件的 `ok/unavailable`，不返回连接串、密码或 API Key。

当前 RabbitMQ 客户端未实现断线自动重连；重试耗尽后会确认消息并记录服务端错误，暂未增加死信队列。系统不对任意外部副作用承诺绝对 exactly-once。

知识摄取默认限制为 txt/md 5 MiB、PDF 25 MiB、DOCX/PPTX 50 MiB，全部可通过环境变量调整。Office ZIP 会限制文件数、展开大小、压缩比和路径深度；PDF 页数、PPT 幻灯片数和解析时间也有限制。解析运行在独立子进程中，具备超时终止、最小环境变量和输出上限，但当前仍不是容器或操作系统账户级的强沙箱。PDF 只提取文本，不包含 OCR；当前也不包含 rerank、混合检索或扫描件识别。

知识 Job 采用“MySQL 持久状态 + RabbitMQ 至少一次投递 + CAS 抢占”。Dispatcher 会重新投递 Queued Job，因此 RabbitMQ 发布确认不确定时仍可恢复，但可能产生可安全丢弃的重复消息；当前没有完整 Transactional Outbox。删除采用最终一致：先标记 `Deleting` 并立即从搜索结果过滤，再由后台清理 Qdrant、对象存储和 MySQL 元数据。

媒体摄取当前面向单机演示：ASR 使用 CPU 时速度取决于机器；关键帧按固定时间间隔抽取，Tesseract 只识别画面文字，不理解无文字画面；解析子进程有超时、并发和资源参数限制，但不是容器/独立系统账户级强沙箱。取消是协作式的，已完成的短阶段可能先落盘，再由清理流程删除。系统对摄取和索引提供可恢复的至少一次处理，不承诺外部系统级 exactly-once。

Agent Loop 最多执行 20 次决策并最多 replan 2 次。replan 会用新活动计划替换旧步骤，旧过程保留在日志中。Agent 在安全 checkpoint 上中断时会以 MySQL 业务事实对账后继续；若中断时工具可能已经产生外部副作用且 MySQL 尚未确认完成，系统会把当前步骤和任务标记为 `Failed`，不会自动重放。Redis 旧锁未释放时，RabbitMQ Consumer 会保留未确认消息并等待锁释放或过期。

Checkpoint 不能把外部 HTTP 动作变成绝对 exactly-once。对于支付、发消息等不可幂等动作，生产系统仍需要业务幂等键、外部系统去重或人工确认。HTTP 工具只允许配置的主机，但当前未实现生产级 DNS rebinding 防护。

## 下一步

1. 固化一组可重复的 Workflow、Agent、文档和视频演示用例并保存截图。
2. 整理面试讲解、架构取舍和已知限制，完成最终 v2 收口，之后原则上不再扩模块。

## 最终 v2 完成标准

- 用户输入目标后，LLM 生成经过校验的结构化计划。
- 系统只允许调用 `rag_query` 和白名单 `http_request`。
- 工具结果形成 Observation，模型决定继续、有限 replan、完成或失败。
- RabbitMQ 重复消息不会造成重复执行。
- RAG 返回 TopK 片段、最终回答和来源引用。
- 简单 DAG 支持依赖校验和环检测。
- MiniKV 保存并演示 runtime checkpoint。
- 浏览器可查看目标、计划、步骤、日志、来源和最终答案。
- 浏览器可异步导入十一种知识资料、查看摄取进度、取消媒体摄取、提交 Agent 目标、查看任务列表，并轮询运行状态。
- 前端具备加载、空数据、失败和后端不可用状态，模型 API Key 不进入浏览器。
- README、测试、评测和 Docker Compose 能支撑他人复现。

前端只承担项目操作和演示，不做登录权限、拖拽编排、可视化 DAG 编辑器、复杂图表或营销首页。

## 项目边界

当前实现是用于学习和演示任务编排、状态机、事务、并发控制与异步执行的轻量项目，不是生产级分布式调度系统。

项目已经跑通 LLM 规划、工具调用、Observation 和最终答案闭环，但仍定位为单机学习与面试演示项目，不宣称具备生产级多租户、安全隔离或分布式 exactly-once 能力。
