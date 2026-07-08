# WeKnora 并发和队列配置

本文说明 ictrek 部署模板里的并发配置。实际部署时，把应用侧变量写到目标机 `.env`；模型服务侧变量写到对应模型容器的 env。

## 三层控制

| 层级 | 作用 | 主要变量 |
| --- | --- | --- |
| Asynq 后台任务池 | 控制后台任务 worker 总数，以及不同任务队列的调度权重。 | `WEKNORA_ASYNQ_CONCURRENCY`、`WEKNORA_ASYNQ_QUEUE_*` |
| 后台 LLM 限流 | 防止 Graph、Wiki、自动问题生成、摘要生成、多模态 VLM 把主 QA 模型并发吃满。 | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`、`WEKNORA_CHAT_RESERVED_CONCURRENCY`、`WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` |
| 模型服务容量 | 控制 vLLM、Ollama 或其他 OpenAI-compatible 服务实际能同时处理多少请求。 | `VLLM_MAX_NUM_SEQS`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE`、`OLLAMA_NUM_PARALLEL` |

队列权重不是硬性的模型并发预留。真正给聊天保留模型槽位的是后台 LLM 限流。`WEKNORA_CHAT_RESERVED_CONCURRENCY` 是 WeKnora 应用侧限制，不是 vLLM/Ollama 自带的硬隔离；后台 LLM 调用必须经过代码里的 `acquireBackgroundLLMSlot` 才会被限制。

## 机器资源评估流程

给一台新机器定模型、上下文、模型并发和聊天预留时，按下面顺序做，不要只按显存大小或 `max-num-seqs` 猜。

1. 先定在线体验目标。明确是否必须跑 VLM/Graph/Wiki、是否需要 16k 以上上下文、是否要在文档入库时还能稳定聊天。聊天必须最高优先级时，先预留 `2-3` 个主 QA 槽；多人同时使用再继续提高。
2. 选候选模型。优先用目标硬件已经验证能稳定启动的量化模型；同等效果下先选更小模型或更低显存量化。模型启动后显存不能长期贴近上限，至少留出 KV cache、embedding、数据库和系统余量。
3. 定上下文。上下文越大，KV cache 越多，满长并发越低。先用业务必须值，例如 16k、18k、20k；如果聊天或 Graph 变慢，优先把上下文从 20k 降到 18k/16k，而不是直接抢聊天预留。
4. 启动 vLLM 做实测。先设置保守 `--gpu-memory-utilization`，再设置 `--max-model-len` 和候选 `--max-num-seqs`。启动日志里的这行是关键依据：

```text
Maximum concurrency for 18,000 tokens per request: 4.75x
```

这个数表示满长请求下的有效并发。`VLLM_MAX_NUM_SEQS` 可以略高于它，用于短请求调度弹性；但 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 不应明显高于这个有效并发，否则后台长任务会把聊天压住。

5. 定 WeKnora 应用侧并发。推荐公式：

```text
WEKNORA_MAIN_QA_MODEL_CONCURRENCY = min(VLLM_MAX_NUM_SEQS, floor(vLLM 满长有效并发) 或略高 1)
WEKNORA_CHAT_RESERVED_CONCURRENCY = 2-3
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

如果 `background_llm_slots < 1`，说明模型/上下文/显存组合不足以同时跑后台增强和聊天，应降低上下文、换小模型，或关闭/降低 Graph、Wiki、VLM 后台任务。

6. 定 Embedding 并发。Embedding 模型最好独立服务。vLLM embedding 场景下，`CONCURRENCY_POOL_SIZE` 是文档 embedding 应用侧上限；如果希望聊天检索保留 2-3 个槽，就让 `CONCURRENCY_POOL_SIZE` 低于 embedding 服务侧总并发。Ollama 场景下优先分成 QA/VLM 容器和 embedding 容器。

7. 用线上指标回验。文档入库时看：

```bash
curl -sS http://127.0.0.1:<vllm-metrics-port>/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
docker logs --tail 50 <qwen-vllm-container> 2>&1 \
  | grep -E 'Running:|Waiting:|GPU KV cache usage'
```

如果没有聊天时 qwen `Running` 长期等于或高于 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY`，这是正常后台占用；如果聊天时 `Waiting > 0` 持续出现，优先降低后台槽、Graph/Wiki/VLM 并发或上下文。不要用提高 Asynq worker 数解决模型排队。

## 主 QA/LLM 并发

对话、Graph 抽取、Wiki 生成、自动问题生成、文档摘要、多模态 VLM 可能共用同一个主 QA/LLM 模型。部署时按模型服务真实容量配置：

```dotenv
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_WIKI_INGEST_MAP_PARALLEL=2
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=2
```

`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 应该对齐主 QA 模型服务的真实在线并发。vLLM 场景下通常和 `VLLM_MAX_NUM_SEQS` 保持一致；Ollama 场景下通常和 QA Ollama 容器的 `OLLAMA_NUM_PARALLEL` 保持一致。

后台 LLM 可用槽位近似为：

```text
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

如果两个值都大于 0，且 `main <= reserved`，WeKnora 仍会保留 1 个后台槽位，避免 Graph/Wiki/Question/Multimodal 完全不执行。如果任意一个值为空或为 `0`，后台 LLM 限流不会启用。

`WEKNORA_GRAPH_LLM_CONCURRENCY` 限制单文档 Graph 抽取中的 LLM 并发，并且代码会把它限制到 `WEKNORA_MAIN_QA_MODEL_CONCURRENCY/2` 以内。

Wiki map/reduce 并发先读知识库 `wiki_config.ingest_map_parallel` 和 `wiki_config.ingest_reduce_parallel`；知识库未设置时，使用 `WEKNORA_WIKI_INGEST_MAP_PARALLEL` 和 `WEKNORA_WIKI_INGEST_REDUCE_PARALLEL` 作为部署级默认值；env 也为空时才回退代码默认值。小机器建议 env 默认设为 `1` 或 `2`，个别大知识库再通过 KB 配置提高。

## 单文档任务和队列权重

文档解析、Graph、Wiki、自动问题生成、摘要等后台任务都走 Asynq。队列权重通过 env 读取：

```dotenv
WEKNORA_ASYNQ_CONCURRENCY=4
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_PARSE=5
WEKNORA_ASYNQ_QUEUE_DEFAULT=3
WEKNORA_ASYNQ_QUEUE_LOW=1
WEKNORA_ASYNQ_QUEUE_MULTIMODAL=3
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=1
WEKNORA_REPARSE_INCOMPLETE_ON_START=true
```

`WEKNORA_ASYNQ_CONCURRENCY` 是后台 worker 总并发。`WEKNORA_ASYNQ_QUEUE_*` 是队列调度权重，权重越高越容易被调度，但不是严格的每队列并发上限。

`parse` 队列承载文档解析和批量重解析，默认高于 default/multimodal/graph/question；多模态 VLM 队列默认权重为 3，排在文本解析之后、图谱和问题生成之前。

小机器上不要把 Graph、Question、Multimodal 队列权重调太高。聊天请求本身不走这些后台队列，但后台任务仍可能竞争同一个 LLM 或 Embedding 模型服务。`WEKNORA_REPARSE_INCOMPLETE_ON_START=true` 会在服务启动时把 failed/pending/processing/finalizing 的文档重新入队，适合部署后补救解析失败；部署模板默认开启，代码默认值仍是关闭，只有 env 显式开启才会执行。

## Embedding 并发

文档向量化主要看这几个参数：

```dotenv
BATCH_EMBED_SIZE=4
CONCURRENCY_POOL_SIZE=2
```

`BATCH_EMBED_SIZE` 是单次 embedding 请求里打包的 chunk 数。

`CONCURRENCY_POOL_SIZE` 是应用侧文档 embedding 请求并发上限。它如果低于文档 worker 数，后台解析可能看起来卡在 embedding 阶段；它如果高于 embedding 服务容量，聊天检索和文档入库会同时排队。

如果 embedding 模型是单独 vLLM 服务，让 `CONCURRENCY_POOL_SIZE` 低于该服务的 `max-num-seqs`，给在线检索留余量。如果 embedding 用 Ollama，`OLLAMA_NUM_PARALLEL` 控制 Ollama 服务侧并发。

## Orin NX / L4T 纯 Ollama 推荐值

Orin NX 这类机器上，如果 Ollama 一个实例同时跑 QA/VLM 和 embedding，`OLLAMA_NUM_PARALLEL` 只能限制整个实例，不能分别给聊天和 embedding 保留槽位。推荐分成两个容器：

| 容器 | 用途 | Ollama 并发 | WeKnora 侧配置 |
| --- | --- | ---: | --- |
| `ollama-qa` | KnowledgeQA 和 VLM | `OLLAMA_QA_NUM_PARALLEL=4` | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4`、`WEKNORA_CHAT_RESERVED_CONCURRENCY=2` |
| `ollama-embedding` | bge-m3 embedding | `OLLAMA_EMBEDDING_NUM_PARALLEL=4` | `CONCURRENCY_POOL_SIZE=2`、`BATCH_EMBED_SIZE=4` |

这样聊天/VLM 至少保留 2 个槽位，文档 embedding 只消耗 `ollama-embedding` 容器。机器稳定、显存和等待队列都有余量时，可以把 QA 提到 `5`，聊天保留提到 `3`；先不要提高 embedding 并发。

分离容器时，模型行使用 OpenAI-compatible gateway：

```text
KnowledgeQA  source=remote  base_url=http://ollama-qa:11535/v1
VLLM         source=remote  base_url=http://ollama-qa:11535/v1
Embedding    source=remote  base_url=http://ollama-embedding:11535/v1  dimension=1024
```

只有单 Ollama 容器时，才把三类模型都建成 `source=local` 并统一使用 `OLLAMA_BASE_URL`。单实例降级值：

```dotenv
OLLAMA_NUM_PARALLEL=4
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
CONCURRENCY_POOL_SIZE=1
BATCH_EMBED_SIZE=4
```

## 推荐值

| 机器类型 | QA 服务并发 | 聊天保留 | Graph | Embedding 并发 | 说明 |
| --- | ---: | ---: | ---: | ---: | --- |
| Orin NX / L4T 分离 Ollama | 4 | 2 | 2 | 2 | 首选。QA/VLM 与 embedding 分容器。 |
| 通用 4 并发主机 | 4 | 1-2 | 2 | 2 | 优先降低 Wiki 和 embedding，不要先压缩聊天保留。 |
| 9B vLLM 主机 | 按 `VLLM_MAX_NUM_SEQS` | 2-3 | 2 | 按 embedding 后端容量 | QA/Graph/Wiki/Question 共用主 QA 模型。 |

## 调参判断

| 现象 | 优先调整 |
| --- | --- |
| 文档入库时聊天变慢 | 增大 `WEKNORA_CHAT_RESERVED_CONCURRENCY`，或降低 Graph/Wiki/Question 的并发和队列权重。 |
| Graph 或 Wiki 很慢，但聊天正常 | 只有在模型服务还有余量时，才提高 `WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` 或知识库级 wiki map/reduce 并发。 |
| 卡在 embedding 阶段 | 先检查 embedding 服务是否 ready，再对比 `WEKNORA_ASYNQ_CONCURRENCY`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE`、Ollama `OLLAMA_NUM_PARALLEL`。 |
| Ollama 单实例聊天被文档入库拖慢 | 改成 QA/VLM 和 embedding 两个 Ollama 容器；单实例只能做 best-effort。 |
| GPU 显存接近打满 | 先降低模型服务侧并发、上下文长度或显存占用率，再把应用侧并发同步降下来。 |

## 现场确认

在目标机上看运行中的容器，不要只看 env 文件：

```bash
docker inspect <app-container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(WEKNORA_MAIN_QA_MODEL_CONCURRENCY|WEKNORA_CHAT_RESERVED_CONCURRENCY|WEKNORA_GRAPH_LLM_CONCURRENCY|WEKNORA_WIKI_INGEST_MAP_PARALLEL|WEKNORA_WIKI_INGEST_REDUCE_PARALLEL|CONCURRENCY_POOL_SIZE|BATCH_EMBED_SIZE)='

docker inspect <ollama-qa-container> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(OLLAMA_NUM_PARALLEL|OLLAMA_KEEP_ALIVE|OLLAMA_CONTEXT_LENGTH)='

curl -fsS http://127.0.0.1:<qa-gateway-port>/v1/models
curl -fsS http://127.0.0.1:<embedding-gateway-port>/v1/embeddings \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":["中文 embedding 测试"]}'

docker exec <ollama-qa-container> ollama ps
docker exec <ollama-embedding-container> ollama ps
```

vLLM 场景看 waiting：

```bash
curl -sS http://127.0.0.1:<vllm-metrics-port>/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
```

如果 `waiting > 0` 长时间存在，先降低后台并发或 `CONCURRENCY_POOL_SIZE`，不要只提高模型服务并发。

修改后要同步 env、compose 里的模型服务参数和模型行配置。只改 app env 时，重新执行对应 compose `up -d app` 即可让 app 读取新值。
