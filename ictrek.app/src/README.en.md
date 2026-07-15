# WeKnora

WeKnora is an enterprise knowledge-base, RAG, Wiki graph, and agent platform. This VOS package installs the four WeKnora images in pull mode and starts the selected-profile `ollama_server` images for chat, vision, and embedding.

## Components

- WeKnora web frontend
- WeKnora app API
- DocReader document parser
- Agent Skills sandbox image
- Ollama QA/VLM container
- Ollama embedding container
- Local Postgres and Redis

## Profiles

Choose one profile at install time: `AMD_with_cuda`, `ARM_with_cuda`, `l4t`, `thor_spark`, or `SOPHON_bm1688`.

AMD and ARM generic profiles read image versions from `AMD_with_cuda` and `ARM_with_cuda` respectively. `l4t`, `thor_spark`, and `SOPHON_bm1688` use their own Feishu sheets.

## Models

The package does not bake model rows into images. Add models in the WeKnora UI after installation, or mount a model configuration later. Default in-network endpoints are:

- QA/VLM: `http://weknora-ollama-qa:11535/v1`
- Embedding: `http://weknora-ollama-embedding:11535/v1`

If Model Hub manages the model files, make sure the required models already exist in the Ollama model directories.
