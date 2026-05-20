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

Initialize project config files in the current folder: `speecdex --init`

This creates `.speecdex/config.yaml` and `.speecdex/llms.yaml` when missing,
without overwriting existing `.yaml` or `.yml` config files.

Reindex markdown docs in the current folder tree: `speecdex`

Speecdex compares Markdown file checksums against the previous compatible index.
Unchanged files reuse stored chunks and embeddings, changed files are
rechunked and reembedded, and deleted files are retained as deleted index
records so restoring the same content can undelete them without another
embedding call.

Force a fresh rechunk/reembed of current files: `speecdex --force`

During indexing, Speecdex prints per-file progress to stderr and the final
indexing summary to stdout. When run inside a git worktree, the index stores
the active branch and includes it in the summary. Rebuild reuse diagnostics
also print to stderr:

```text
Indexing file 1/3: docs/overview.md (2 chunks) | elapsed 1s | 2.00 chunks/s | ETA 1s
Index rebuild: reused 4 chunks | embedded 2 chunks | retained deleted 1 chunks
```

Search in the index, semantic only: `speecdex --query "root business object definitions"`

Search in the index semantic OR any text match: `speecdex --query "root business object definitions" --text "extends BusinessObject" --text "implements BusinessObject"`

Search output includes the branch that was active when the index was written:

```yaml
results: []
indexed_branch: main
```

Print only the branch stored in the current index: `speecdex --show-branch`

## App Configuration

Speecdex reads optional app settings from `.speecdex/config.yaml` in the current
project, inheriting missing values from `~/.speecdex/config.yaml`.

Use `only_entries` to narrow Markdown discovery before ignores are applied, and
`ignored_entries` to exclude matches afterward:

```yaml
config:
  only_entries:
    - docs
    - README.md
  ignored_entries:
    - .git
    - docs/private
```

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
