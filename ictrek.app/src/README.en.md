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

Choose one profile at install time: `AMD_with_cuda`, `ARM_with_cuda`, `l4t`, or `thor_spark`.

AMD and ARM generic profiles read `weknora*` and `ollama_server` image versions from `AMD_with_cuda` and `ARM_with_cuda` respectively. `l4t` and `thor_spark` use their own Feishu sheets. This app publishes four profiles only.

## Install Settings

The install UI exposes model, resource, and host-path settings. By default HybRAG stores runtime data under `/data/vos_workspace/hybrag` in `files`, `docreader`, and `redis` subdirectories. Ollama models reuse the Model Hub shared directory `/data/vos_workspace/model_hub/ollama` unless `MODEL_HUB_SHARED_MODELS_PATH` is changed during installation.

Postgres is provided by PGV. The default connection is `shared-pgv:5432` with user/password/database `weknora` / `weknora` / `WeKnora`. These fields are exposed in the install UI; if PGV was installed with different credentials or database name, update them in the HybRAG install form.

## Models

The package does not bake model rows into images. Add models in the HybRAG UI after installation, or mount a model configuration later. Default in-network endpoints are:

- QA/VLM: `http://hybrag-ollama-qa:11535/v1`
- Embedding: `http://hybrag-ollama-embedding:11535/v1`

If Model Hub manages the model files, make sure the required models already exist in the Ollama model directories.
