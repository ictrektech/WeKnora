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

Choose one profile at install time: `amd`, `arm`, `l4t`, or `thor-spark`.

AMD and ARM generic profiles read `weknora*` and `ollama_server` image versions from `AMD_with_cuda` and `ARM_with_cuda` respectively. `l4t` and `thor-spark` use their own Feishu sheets. This app publishes four profiles only.

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
