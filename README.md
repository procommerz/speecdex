# Speecdex - Local Markdown Vector Search

Speecdex is a local-first CLI for developers who want semantic and literal
search over Markdown documentation in a project folder. It recursively indexes
Markdown files in place, stores a compact local index, and returns
source-grounded matches with file paths, line ranges, scores, and chunk text.

The MVP is scoped to predictable local developer use: Markdown files under the
current working directory, project/user YAML configuration, OpenAI-compatible
embeddings, and fenced YAML search output. Local embedding models are served
by any OpenAI-compatible runtime the user already runs (for example
`llama-server`, Ollama, or LM Studio).

## Installation

Install the latest macOS release with Homebrew:

```sh
brew tap procommerz/speecdex
brew install --cask speecdex
```

Linux amd64/arm64 binaries and macOS amd64/arm64 binaries are also published
as archives on GitHub Releases.

## Quick Start

Initialize project config files:

```sh
speecdex --init
```

Build or rebuild the local index:

```sh
speecdex
```

Run semantic search:

```sh
speecdex --query "root business object definitions"
```

Run literal-only search without embedding the query:

```sh
speecdex --text "extends BusinessObject"
```

Combine semantic search with one or more literal matches:

```sh
speecdex --query "root business object definitions" --text "extends BusinessObject" --text "implements BusinessObject"
```

## Commands

`speecdex --init` creates `.speecdex/config.yaml` and
`.speecdex/llms.yaml` when missing. Existing `.yaml` or `.yml` config files are
preserved.

`speecdex` indexes `.md` and `.markdown` files under the current working
directory. Matching is case-insensitive. The index is written to
`.speecdex/index.bin`.

Speecdex compares Markdown file checksums against the previous compatible
index. Unchanged files reuse stored chunks and embeddings, changed files are
rechunked and reembedded, and deleted files are retained as deleted index
records so restoring the same content can undelete them without another
embedding call.

`speecdex --force` rebuilds current files without checksum reuse. Deleted
records for files that are still missing are preserved.

`speecdex --query "..."` searches the existing index semantically.

`speecdex --text "..."` searches stored chunk text literally. Multiple
`--text` flags are treated as OR conditions. Literal matching is
case-sensitive for the MVP.

`speecdex --show-branch` prints the git branch stored in the current index.

`speecdex --install-skill` installs the packaged `docs-search` skill for local
Codex or Claude Code project setups. It writes
`.codex/skills/docs-search/SKILL.md` and/or
`.claude/skills/docs-search/SKILL.md` when local agent folders or marker files
are present. Existing skill files are preserved.

`speecdex --version` prints the binary version, commit, and build date. Release
builds receive these values from GoReleaser.

During indexing, Speecdex prints per-file progress and rebuild diagnostics to
stderr, and the final indexing summary to stdout:

```text
Indexing file 1/3: docs/overview.md (2 chunks, mean 843 chars) | elapsed 1s | 2.00 chunks/s | ETA 1s
Index rebuild: reused 4 chunks | embedded 2 chunks | retained deleted 1 chunks
```

Search commands load the existing index and never rebuild it automatically.
Search output is machine-readable fenced YAML on stdout:

````text
```yaml
results:
  - file: docs/example.md
    start_line: 10
    end_line: 24
    score: 0.8123
    match_sources:
      - semantic
      - text
    matched_text:
      - extends BusinessObject
    text: |
      Chunk text appears here.
indexed_branch: main
```
````

## Configuration

Speecdex reads configuration from two scopes:

- User scope: `~/.speecdex/`
- Project scope: `.speecdex/` in the current working directory

Project configuration overrides user configuration. Missing project values
inherit from user configuration, and missing user values fall back to built-in
defaults where available.

Both `.yaml` and `.yml` extensions are supported:

- App config: `config.yaml` or `config.yml`
- Model config: `llms.yaml` or `llms.yml`

When both extensions exist in the same scope for the same config type,
`.yaml` takes precedence over `.yml`.

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
  indexing:
    chunk_size: 1200
    chunk_overlap: 200
```

Embedding configuration is required for indexing and semantic search. The
packaged stub config uses an OpenAI-compatible endpoint with this model name:

```yaml
llms:
  embedding:
    style: openai-compatible
    endpoint: http://127.0.0.1:1234/v1
    model_name: text-embedding-embeddinggemma-300m-qat
    api_key: ""
    default_dims: 768
```

Reranking is optional and is active only when `llms.reranking` is configured.

## Running a Local Embedding Model

Speecdex does not ship its own model runtime. It talks to any server that
exposes an OpenAI-compatible `/v1/embeddings` endpoint, so a local embedding
model is just a matter of pointing `llms.embedding.endpoint` at a process you
run yourself.

The recommended option is [llama.cpp's `llama-server`](https://github.com/ggml-org/llama.cpp), which serves any
GGUF embedding model with an OpenAI-compatible API:

```sh
llama-server \
  --embedding \
  --host 127.0.0.1 \
  --port 8080 \
  -hf CompendiumLabs/bge-small-en-v1.5-gguf
```

Then point `.speecdex/llms.yaml` at it:

```yaml
llms:
  embedding:
    style: openai-compatible
    endpoint: http://127.0.0.1:8080/v1
    model_name: bge-small-en-v1.5
    default_dims: 384
```

Other OpenAI-compatible local runtimes work the same way — for example
[Ollama](https://ollama.com) on `http://127.0.0.1:11434/v1` or
[LM Studio](https://lmstudio.ai) on `http://127.0.0.1:1234/v1`. Whichever
runtime is listening on the configured endpoint is what Speecdex will use.

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

Run it from the repository root:

```sh
./dist/speecdex-darwin-arm64 --init
./dist/speecdex-darwin-arm64
./dist/speecdex-darwin-arm64 --query "root business object definitions"
```

Run tests inside the same pinned Go builder image:

```sh
make docker-test
```

The build can be adjusted with make variables:

```sh
make docker-build GOOS=darwin GOARCH=amd64
```

The MVP build path uses `CGO_ENABLED=0`, which is suitable for the pure-Go
CLI. Local model inference is delegated to an external OpenAI-compatible
runtime, so Speecdex itself does not need cgo.

## Releasing

Tagged releases are built with GoReleaser from GitHub Actions. Push a tag like
`v0.1.0` to publish darwin/linux archives for amd64 and arm64, generate
`checksums.txt`, create the GitHub Release, and update the Homebrew cask in
`github.com/procommerz/homebrew-speecdex`.

The release workflow requires the app repository secret `TAP_GITHUB_TOKEN`,
which must be a PAT with write access to the tap repository. The default
`GITHUB_TOKEN` is used for the app repository release.

Local release checks:

```sh
make docker-test
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```

The first Homebrew cask releases are unsigned and not notarized. If macOS blocks
the binary, explicitly trust the release from System Settings or remove the
quarantine attribute manually only after confirming you trust the downloaded
artifact.
