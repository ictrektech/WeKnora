# HybRAG

HybRAG is an enterprise knowledge-base, RAG, Wiki graph, and agent platform. This VOS package installs the HybRAG runtime components in pull mode and starts the selected-profile `ollama_server` images for chat, vision, and embedding. To avoid extra rebuilds and Feishu column migration, image repositories still use the published `weknora*` names; the VOS app name, app id, routes, and container services use HybRAG.

## Components

- HybRAG web frontend
- HybRAG app API
- DocReader document parser
- Agent Skills sandbox image
- Ollama QA/VLM container
- Ollama embedding container
- Redis
- External PGV/Postgres dependency

## Profiles

Choose one profile at install time: `amd`, `amd-no-cuda`, `arm`, `arm-no-cuda`, `l4t`, or `thor-spark`.

`amd-no-cuda` and `arm-no-cuda` reuse `weknora*` and `ollama_server` image versions from `AMD_with_cuda` and `ARM_with_cuda`, but run Ollama without `runtime: nvidia` for AMD64/ARM64 hosts without GPU. `l4t` and `thor-spark` use their own Feishu sheets.

## Install Settings

The install UI exposes model, resource, and host-path settings. By default HybRAG stores runtime data under `/data/vos_workspace/hybrag` in `files`, `docreader`, and `redis` subdirectories. Ollama models reuse the Model Hub shared directory `/data/vos_workspace/model_hub/ollama` unless `MODEL_HUB_SHARED_MODELS_PATH` is changed during installation.

Postgres is provided by PGV. The default connection is `shared-pgv:5432` with user/password/database `weknora` / `weknora` / `WeKnora`. These fields are exposed in the install UI; if PGV was installed with different credentials or database name, update them in the HybRAG install form.

## VOS SSO

This package currently ships a transitional VOS iframe SSO adapter and does not require VOS-side changes. The frontend first reads a future-style `window.__VOS_APP_CONTEXT__`, then falls back to the current same-origin VOS access-token store. The backend verifies that token through the `/v1000/user/check` endpoint configured by `HYBRAG_VOS_USERINFO_URL`.

After verification, HybRAG provisions or logs in `username@local` and creates the user's personal workspace. VOS `admin` maps to `admin@local`, which is promoted to HybRAG system admin with cross-tenant administration rights.

This is intentionally replaceable. When VOS provides standard OIDC or official iframe user injection, disable `HYBRAG_VOS_SSO_ENABLED` or replace only the identity adapter; the local user and workspace provisioning path can stay unchanged.

## Models

The package does not bake model rows into images. Add models in the HybRAG UI after installation, or mount a model configuration later. Default in-network endpoints are:

- QA/VLM: `http://hybrag-ollama-qa:11535/v1`
- Embedding: `http://hybrag-ollama-embedding:11535/v1`

If Model Hub manages the model files, make sure the required models already exist in the Ollama model directories.

On startup, both Ollama containers start local `ollama serve`, ask `MODEL_HUB_BACKEND_URL` to pull the required model through Model Hub, and fall back to local `ollama pull` if Model Hub is unavailable or the pull task fails. After the pull check, each container sends a warmup request with `OLLAMA_KEEP_ALIVE=-1` so the model stays resident before the OpenAI-compatible gateway starts. The default `MODEL_HUB_BACKEND_URL` is `http://model-hub-backend:5005`, the Model Hub backend alias on the `vos_default` network.

QA/VLM defaults to `qwen3.5:2b`, and embedding defaults to `bge-m3`. Non-thor profiles default to 8 QA slots with 2 reserved for chat and 6 shared by background tasks; embedding defaults to 4 total slots with 2 used by document embedding. `thor-spark` uses higher defaults: 20 QA slots, 6 chat-reserved slots, 14 background slots, 16 embedding slots, and 8 document-embedding slots.

See `docs/ictrek/vos-ollama-prewarm.md` in the repository for detailed warmup, residency, and troubleshooting notes.
