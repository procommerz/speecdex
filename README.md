# Speecdex - In-Place Local Markdown Files Indexer and Vector Search Tool

## LLMs for Embedding and Reranking

LLMs are configured in the config file in `~/.speecdex/llms.yml`, which can be overridden with a similar config dir and file at the call site directory.

Embedding is required. It can be either a remote endpoint or a built-in GGUF model.

Reranking is optional and is only active when an LLM configuration is provided.

## Using Local Models

When configured to use a local model, the app will try to download this configured model upon startup, if it's not found. The default model is BGE Small EN v1.5 (F32):

`https://huggingface.co/CompendiumLabs/bge-small-en-v1.5-gguf/resolve/main/bge-small-en-v1.5-f32.gguf`

GGUF is the model file format used by `llama.cpp`.

You can run `speecdex --service` to launch a persistent service that will provide a localhost embedding endpoint (on a configurable port, default=8248) using that local GGUF model (or another model configured in `llms.yml`). When users run the speecdex with search arguments, the app will first check if this endpoint is available and use it, if not - will fallback to the configurated endpoint.


## CLI Interface

Reindex markdown docs in the  current folder tree: `speecdex`
Search in the index, semantic only: `speecdex --query "root business object definitions"`
Search in the index semantic OR any text match: `speecdex --query "root business object definitions" --text "extends BusinessObject" --text "implements BusinessObject"`

## Building

Builds run inside Docker so the host machine does not need to execute the Go
toolchain or downloaded module code directly.

Build the default macOS Apple Silicon binary:

```sh
make docker-build
```

The binary is written to:

```text
dist/speecdex-darwin-arm64
```

Run tests inside the same pinned Go builder image:

```sh
make docker-test
```

The build can be adjusted with make variables:

```sh
make docker-build GOOS=darwin GOARCH=amd64
```

The MVP build path uses `CGO_ENABLED=0`, which is suitable for the current pure
Go CLI. If future local GGUF runtime work requires cgo, the cross-compilation
strategy will need to be revisited.
