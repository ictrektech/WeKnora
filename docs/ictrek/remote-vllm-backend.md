# 远程 vLLM 后端

本文件记录 WeKnora 可用的 OpenAI-compatible vLLM 后端启动方式。中文说明在上方，英文原文在下方。

`tc232` 是操作员本机 SSH config alias，不是 API hostname。外部访问需要单独的公网映射、反向代理、VPN 或 SSH tunnel。

## 当前后端形态

- 模型仓库：`QuantTrio/Qwen3.5-9B-AWQ`
- 远程模型路径：`/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ`
- Docker 镜像：`vllm/vllm-openai:v0.19.0-cu130-ubuntu2404`
- 容器名：`qwen35-9b-awq-vllm`
- 容器端口：`8000`
- 远程宿主机端口：`18118`
- 端口映射：`18118:8000`
- 对外模型名：`qwen3.5-9b-awq`
- 量化：`awq_marlin`
- 最大上下文：`32768`
- GPU 显存比例：`0.50`
- 最大并发序列：`4`

WeKnora 不默认内置这些模型行。需要使用该后端时，在 Web UI 或运维挂载的 `config/builtin_models.yaml` 中添加：

```text
KnowledgeQA  source=remote  name=qwen3.5-9b-awq  base_url=http://host.docker.internal:18118/v1
VLLM         source=remote  name=qwen3.5-9b-awq  base_url=http://host.docker.internal:18118/v1
```

## 下载模型

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

PY=/data/jhu/dev/envs/conda/bin/python3
$PY -m pip install -q -U huggingface_hub

HF=/data/jhu/dev/envs/conda/bin/hf
mkdir -p /data/jhu/models/hf /data/jhu/.cache/huggingface

export HF_HOME=/data/jhu/.cache/huggingface
export HF_XET_HIGH_PERFORMANCE=1

MODEL_ID='QuantTrio/Qwen3.5-9B-AWQ'
LOCAL_DIR='/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ'

$HF download "$MODEL_ID" --local-dir "$LOCAL_DIR" --max-workers 8
du -sh "$LOCAL_DIR"
EOF
```

## 启动 vLLM

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

NAME='qwen35-9b-awq-vllm'
MODEL_DIR='/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ'
IMAGE='vllm/vllm-openai:v0.19.0-cu130-ubuntu2404'

docker rm -f "$NAME" >/dev/null 2>&1 || true

docker run -d \
  --name "$NAME" \
  --gpus all \
  --ipc=host \
  -p 18118:8000 \
  -v "$MODEL_DIR:/model:ro" \
  -e HF_HOME=/tmp/huggingface \
  "$IMAGE" \
  --model /model \
  --served-model-name qwen3.5-9b-awq \
  --host 0.0.0.0 \
  --port 8000 \
  --trust-remote-code \
  --quantization awq_marlin \
  --gpu-memory-utilization 0.50 \
  --max-num-seqs 4 \
  --enforce-eager \
  --max-model-len 32768
EOF
```

`--gpu-memory-utilization 0.50` 将 vLLM 控制在大约一半显存。`--max-num-seqs 4` 控制并发，避免 KV cache 预算失控。`--enforce-eager` 用于避免该主机上图编译带来的较重启动开销。

## 验证

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

curl -fsS http://127.0.0.1:18118/v1/models

curl -fsS http://127.0.0.1:18118/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen3.5-9b-awq",
    "messages": [
      {"role": "user", "content": "用一句中文说明你是谁。"}
    ],
    "max_tokens": 128,
    "temperature": 0.2
  }'
EOF
```

外部调用时，把 `127.0.0.1:18118` 替换为实际外部映射地址。不要把 `tc232:18118` 当成 API URL。

---

# Remote vLLM Backend

This note records the Qwen3.5 9B AWQ vLLM backend prepared on the remote machine reached from this workstation with `ssh tc232`.

`tc232` is an SSH config alias on the operator workstation. It is not a network hostname for API clients. External access requires a separately managed public mapping, reverse proxy, VPN route, or SSH tunnel.

## Current Backend

- Remote SSH target: `ssh tc232`
- Model repo: `QuantTrio/Qwen3.5-9B-AWQ`
- Remote model path: `/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ`
- Docker image: `vllm/vllm-openai:v0.19.0-cu130-ubuntu2404`
- Container name: `qwen35-9b-awq-vllm`
- Container port: `8000`
- Remote host port: `18118`
- Docker port mapping: `18118:8000`
- Served model name: `qwen3.5-9b-awq`
- Quantization: `awq_marlin`
- Max model length: `32768`
- GPU memory utilization: `0.50`
- Max concurrent sequences: `4`
- Optional WeKnora model rows, if the operator chooses to register this backend:
  - `KnowledgeQA` row with `source=remote`, `name=qwen3.5-9b-awq`,
    `base_url=http://host.docker.internal:18118/v1`
  - `VLLM` row with the same model name and base URL when this backend is used
    for vision-language calls

These rows are not shipped by default in the WeKnora image or ictrek compose
files. Add them through the Web UI or an operator-created
`config/builtin_models.yaml` only for deployments that intentionally use this
vLLM backend.

## Download Model

Run on the remote target:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

PY=/data/jhu/dev/envs/conda/bin/python3
$PY -m pip install -q -U huggingface_hub

HF=/data/jhu/dev/envs/conda/bin/hf
mkdir -p /data/jhu/models/hf /data/jhu/.cache/huggingface

export HF_HOME=/data/jhu/.cache/huggingface
export HF_XET_HIGH_PERFORMANCE=1

MODEL_ID='QuantTrio/Qwen3.5-9B-AWQ'
LOCAL_DIR='/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ'

$HF download "$MODEL_ID" --local-dir "$LOCAL_DIR" --max-workers 8
du -sh "$LOCAL_DIR"
EOF
```

## Start vLLM

Run on the remote target:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

NAME='qwen35-9b-awq-vllm'
MODEL_DIR='/data/jhu/models/hf/QuantTrio--Qwen3.5-9B-AWQ'
IMAGE='vllm/vllm-openai:v0.19.0-cu130-ubuntu2404'

docker rm -f "$NAME" >/dev/null 2>&1 || true

docker run -d \
  --name "$NAME" \
  --gpus all \
  --ipc=host \
  -p 18118:8000 \
  -v "$MODEL_DIR:/model:ro" \
  -e HF_HOME=/tmp/huggingface \
  "$IMAGE" \
  --model /model \
  --served-model-name qwen3.5-9b-awq \
  --host 0.0.0.0 \
  --port 8000 \
  --trust-remote-code \
  --quantization awq_marlin \
  --gpu-memory-utilization 0.50 \
  --max-num-seqs 4 \
  --enforce-eager \
  --max-model-len 32768
EOF
```

`--gpu-memory-utilization 0.50` keeps vLLM around half of the prepared GPU. `--max-num-seqs 4`
caps request concurrency so the reduced KV cache budget stays predictable. `--enforce-eager`
avoids the heavier startup warmup observed with graph compilation on this host.

## Verify On Remote Host

These commands verify the OpenAI-compatible API from inside the remote host only:

```bash
ssh tc232 'bash -s' <<'EOF'
set -euo pipefail

curl -fsS http://127.0.0.1:18118/v1/models

curl -fsS http://127.0.0.1:18118/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen3.5-9b-awq",
    "messages": [
      {"role": "user", "content": "用一句中文说明你是谁。"}
    ],
    "max_tokens": 128,
    "temperature": 0.2
  }'
EOF
```

For callers outside the remote machine, replace `127.0.0.1:18118` with the external endpoint created by the operator. Do not use `tc232:18118` as an API URL.

## Runtime Notes

- The current AWQ model may emit `Thinking Process:` text directly in `content`; this is model/template behavior, not an API compatibility failure.
- `GET /v1/models` and `POST /v1/chat/completions` were verified successfully after the container initialized.
- On the prepared RTX 5880 Ada host, the 50% / 4-sequence configuration keeps vLLM far below the previous roughly 46 GiB startup footprint.
