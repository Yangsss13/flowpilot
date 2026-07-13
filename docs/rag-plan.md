# MiniKVX-Agent RAG 最小闭环

RAG 是最终 v2 的核心 Agent 能力，但必须控制范围，避免项目变成另一个独立大坑。

## 业务闭环

```text
上传文档
-> 文本切块
-> Embedding
-> 写入 Qdrant
-> 工作流执行 RAG Query 节点
-> TopK 检索
-> LLM 根据上下文生成结果
-> 返回答案和来源片段
```

## v2 必做

- 文档格式：先支持 `.txt`、`.md`。
- Chunk：固定长度 + overlap，参数可配置。
- Embedding：定义 Provider 接口，真实实现使用可配置 API。
- Vector Store：Qdrant，通过 Docker Compose 启动。
- Retrieval：TopK 向量检索。
- Generation：将检索片段作为上下文调用 LLM。
- Citation：结果返回来源文件、chunk id 和片段文本。
- Workflow：RAG Query 作为一种可执行步骤类型。
- Test：Embedding 和 Qdrant 使用 fake/mock 覆盖 service 测试，集成测试验证真实链路。

## 暂不做

- PDF/OCR。
- 混合检索、BM25、Rerank。
- GraphRAG。
- 多轮记忆。
- 多 Agent 协作。
- 自研向量数据库。

## 建议数据模型

- `knowledge_bases`：知识库。
- `documents`：文件元数据、状态、chunk 数。
- Qdrant payload：`knowledge_base_id`、`document_id`、`chunk_id`、`source`、`text`。

## RAG 评测

- 准备 10-20 条问题和预期来源文档。
- 统计 `Hit@3`：Top3 是否命中预期来源。
- 记录平均检索耗时和 P95。
- 检查回答是否返回来源引用。
- 暂不使用复杂 LLM-as-a-Judge，避免评测成本失控。

## 面试钩子

- 为什么需要 chunk 和 overlap？
- TopK 太大或太小有什么问题？
- Embedding 模型变化后为什么需要重新入库？
- 向量库和 MySQL 分别保存什么？
- 如何减少幻觉并让答案可追溯？
- Hit@3 能证明什么，不能证明什么？

