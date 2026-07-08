# WeKnora 并发和队列配置

本文说明 ictrek 部署模板里的并发配置。实际部署时，把这些值写到目标机 `.env`。

## 三层控制

| 层级 | 作用 | 主要变量 |
| --- | --- | --- |
| Asynq 后台任务池 | 控制后台任务 worker 总数，以及不同任务队列的调度权重。 | `WEKNORA_ASYNQ_CONCURRENCY`、`WEKNORA_ASYNQ_QUEUE_*` |
| 后台 LLM 限流 | 防止 Graph、Wiki、自动问题生成、摘要生成把主 QA 模型并发吃满。 | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`、`WEKNORA_CHAT_RESERVED_CONCURRENCY`、`WEKNORA_GRAPH_LLM_CONCURRENCY`、`WEKNORA_WIKI_INGEST_*` |
| 模型服务容量 | 控制 vLLM、Ollama 或其他 OpenAI-compatible 服务实际能同时处理多少请求。 | `VLLM_MAX_NUM_SEQS`、`BGE_VLLM_MAX_NUM_SEQS`、`CONCURRENCY_POOL_SIZE`、`BATCH_EMBED_SIZE`、`OLLAMA_NUM_PARALLEL` |

队列权重不是硬性的模型并发预留。真正给聊天保留模型槽位的是后台 LLM 限流。

## 主 QA/LLM 并发

对话、Graph 抽取、Wiki 生成、自动问题生成、文档摘要可能共用同一个主 QA/LLM 模型。部署时按模型服务真实容量配置：

```dotenv
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_WIKI_INGEST_MAP_PARALLEL=2
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=2
```

`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` 应该对齐主 QA 模型服务的真实在线并发。vLLM 场景下，通常和 `VLLM_MAX_NUM_SEQS` 保持一致。

`WEKNORA_CHAT_RESERVED_CONCURRENCY` 是给在线聊天保留的最低并发，不让后台 LLM 任务占用。它是 WeKnora 应用侧的后台 LLM 限流，不是 vLLM 自带的硬隔离；所有文档后处理、Graph、Wiki、自动问题生成等后台 LLM 调用都必须走 `acquireBackgroundLLMSlot`，否则会绕过预留直接占满模型服务。后台 LLM 可用槽位近似为：

```text
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

如果两个值都大于 0，且 `main <= reserved`，WeKnora 仍会保留 1 个后台槽位，避免 Graph/Wiki/Question 完全不执行。如果任意一个值为空或为 `0`，后台 LLM 限流不会启用。

## Asynq 队列权重

后台任务队列权重通过 env 读取：

```dotenv
WEKNORA_ASYNQ_CONCURRENCY=4
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_DEFAULT=3
WEKNORA_ASYNQ_QUEUE_LOW=1
WEKNORA_ASYNQ_QUEUE_MULTIMODAL=1
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=1
```

`WEKNORA_ASYNQ_CONCURRENCY` 是后台 worker 总并发。`WEKNORA_ASYNQ_QUEUE_*` 是队列调度权重，权重越高越容易被调度，但不是严格的每队列并发上限，不能用它代替后台 LLM limiter。

## Embedding 并发

文档向量化主要看这几个参数：

```dotenv
BATCH_EMBED_SIZE=4
CONCURRENCY_POOL_SIZE=5
BGE_VLLM_MAX_NUM_SEQS=8
```

`CONCURRENCY_POOL_SIZE` 是应用侧文档 embedding 请求并发上限。想给聊天检索保留余量时，应让文档 embedding 的应用侧并发低于 embedding 服务容量，或单独降低后台解析 worker 数。

## 现场确认

在目标机上看运行中的容器，不要只看 env 文件：

```bash
docker inspect WeKnora-app --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(WEKNORA_MAIN_QA_MODEL_CONCURRENCY|WEKNORA_CHAT_RESERVED_CONCURRENCY|CONCURRENCY_POOL_SIZE|BATCH_EMBED_SIZE)='

docker inspect qwen35-9b-vllm --format '{{range .Config.Cmd}}{{println .}}{{end}}' \
  | grep -E 'max-num-seqs|max-model-len|max-num-batched-tokens|gpu-memory-utilization'

curl -sS http://127.0.0.1:18118/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
```

如果 `waiting > 0` 长时间存在，先降低后台并发或 `CONCURRENCY_POOL_SIZE`，不要只提高模型服务并发。

---

# WeKnora Concurrency And Queue Configuration

This document explains the concurrency settings in the ictrek deployment template. Put these values in the target host `.env`.

## Three Layers

| Layer | Purpose | Main variables |
| --- | --- | --- |
| Asynq background workers | Controls total background workers and queue scheduling weights. | `WEKNORA_ASYNQ_CONCURRENCY`, `WEKNORA_ASYNQ_QUEUE_*` |
| Background LLM limiter | Keeps Graph, Wiki, question generation, and summaries from consuming all main QA model slots. | `WEKNORA_MAIN_QA_MODEL_CONCURRENCY`, `WEKNORA_CHAT_RESERVED_CONCURRENCY`, `WEKNORA_GRAPH_LLM_CONCURRENCY`, `WEKNORA_WIKI_INGEST_*` |
| Model backend capacity | Controls the real concurrency of vLLM, Ollama, or another OpenAI-compatible service. | `VLLM_MAX_NUM_SEQS`, `BGE_VLLM_MAX_NUM_SEQS`, `CONCURRENCY_POOL_SIZE`, `BATCH_EMBED_SIZE`, `OLLAMA_NUM_PARALLEL` |

Queue weights are not hard model reservations. The background LLM limiter is what preserves model slots for chat.

## Main QA/LLM Concurrency

Chat, Graph extraction, Wiki generation, automatic question generation, and document summaries may share the same main QA/LLM model:

```dotenv
WEKNORA_MAIN_QA_MODEL_CONCURRENCY=4
WEKNORA_CHAT_RESERVED_CONCURRENCY=2
WEKNORA_GRAPH_LLM_CONCURRENCY=2
WEKNORA_WIKI_INGEST_MAP_PARALLEL=2
WEKNORA_WIKI_INGEST_REDUCE_PARALLEL=2
```

`WEKNORA_MAIN_QA_MODEL_CONCURRENCY` should match the actual online capacity of the main QA model service. For vLLM, it usually matches `VLLM_MAX_NUM_SEQS`.

`WEKNORA_CHAT_RESERVED_CONCURRENCY` is the minimum concurrency reserved for online chat. Background LLM calls must go through `acquireBackgroundLLMSlot`; otherwise they can bypass the reservation and fill the model backend.

```text
background_llm_slots = WEKNORA_MAIN_QA_MODEL_CONCURRENCY - WEKNORA_CHAT_RESERVED_CONCURRENCY
```

If both values are positive and `main <= reserved`, WeKnora still keeps one background slot so Graph/Wiki/Question jobs are not fully blocked. If either value is empty or `0`, the background LLM limiter is disabled.

## Asynq Queue Weights

```dotenv
WEKNORA_ASYNQ_CONCURRENCY=4
WEKNORA_ASYNQ_QUEUE_CRITICAL=10
WEKNORA_ASYNQ_QUEUE_DEFAULT=3
WEKNORA_ASYNQ_QUEUE_LOW=1
WEKNORA_ASYNQ_QUEUE_MULTIMODAL=1
WEKNORA_ASYNQ_QUEUE_GRAPH=1
WEKNORA_ASYNQ_QUEUE_QUESTION=1
```

`WEKNORA_ASYNQ_CONCURRENCY` is the total background worker concurrency. `WEKNORA_ASYNQ_QUEUE_*` values are scheduling weights, not strict per-queue concurrency limits.

## Embedding Concurrency

```dotenv
BATCH_EMBED_SIZE=4
CONCURRENCY_POOL_SIZE=5
BGE_VLLM_MAX_NUM_SEQS=8
```

`CONCURRENCY_POOL_SIZE` is the application-side concurrency for document embedding requests. To keep room for chat retrieval, keep it below the embedding backend capacity or reduce background workers.

## Live Checks

Inspect running containers on the target host instead of trusting env files only:

```bash
docker inspect WeKnora-app --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep -E '^(WEKNORA_MAIN_QA_MODEL_CONCURRENCY|WEKNORA_CHAT_RESERVED_CONCURRENCY|CONCURRENCY_POOL_SIZE|BATCH_EMBED_SIZE)='

docker inspect qwen35-9b-vllm --format '{{range .Config.Cmd}}{{println .}}{{end}}' \
  | grep -E 'max-num-seqs|max-model-len|max-num-batched-tokens|gpu-memory-utilization'

curl -sS http://127.0.0.1:18118/metrics \
  | grep -E 'vllm:num_requests_(running|waiting)'
```

If `waiting > 0` stays high, lower background concurrency or `CONCURRENCY_POOL_SIZE` before raising model backend concurrency.
