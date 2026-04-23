# Nexus Embedding Models

This directory holds the embedding model and inference binary used by Nexus's builtin embedding provider.

Files (not committed to git — downloaded at install time):
- `nomic-embed-text-v1.5.Q4_K_S.gguf` — 78MB, Apache 2.0 license
- `llama-server` / `llama-server.exe` — from ggml-org/llama.cpp releases, MIT license

To download manually: run `scripts/fetch-embedding-model.ps1` (Windows) or `scripts/fetch-embedding-model.sh` (Linux/macOS).

When `nexus install` runs with `embedding.provider = "builtin"`, these files are downloaded automatically to the config directory.
