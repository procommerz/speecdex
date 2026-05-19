# Configuration Specification

## Locations

Speecdex reads configuration from two scopes:

- User scope: `~/.speecdex/`
- Project scope: `<project-root>/.speecdex/`

The project root is the current working directory for MVP.

Project configuration overrides user configuration. Missing project values inherit from user configuration. Missing user values fall back to built-in defaults where defined.

## File Names

Speecdex supports both `.yaml` and `.yml` extensions:

- App config: `config.yaml` or `config.yml`
- Model config: `llms.yaml` or `llms.yml`

When both extensions exist in the same scope for the same config type, `.yaml` takes precedence over `.yml`.

## App Config Schema

App config uses this top-level shape:

```yaml
config:
  ignored_entries:
    - .git
    - ignored.md
    - "*/plans/*.md"
  service:
    port: 8248
  indexing:
    chunk_size: 1200
    chunk_overlap: 200
```

All fields are optional.

### `config.ignored_entries`

List of path patterns excluded from Markdown discovery.

Required behavior:

- Match entries relative to the project root.
- Support exact file or directory names such as `.git` and `ignored.md`.
- Support slash-separated relative paths.
- Support glob-style `*` matching.
- Always ignore the local Speecdex data directory.

### `config.service.port`

Port for `speecdex --service`.

Default: `8248`.

Required behavior:

- Bind to `127.0.0.1:<port>`.
- Reject invalid ports outside `1..65535`.

### `config.indexing.chunk_size`

Target chunk size.

Default: `1200`.

MVP interprets this value as approximate characters, not tokens.

### `config.indexing.chunk_overlap`

Target overlap between adjacent chunks.

Default: `200`.

Required behavior:

- Must be less than `chunk_size`.
- Applied on a best-effort basis while preserving line range metadata.

## LLM Config Schema

LLM config uses this top-level shape:

```yaml
llms:
  embedding:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    model_name: text-embedding-model
    api_key: ""
    default_dims: 768
  reranking:
    style: openai-compatible
    endpoint: http://localhost:1234/v1
    model_name: reranker-model
    api_key: ""
```

Embedding config is required for indexing and semantic search. Reranking config is optional.

For compatibility with early examples, implementations should accept `apiKey` and `name` as aliases for `api_key` and `model_name`. Specs and new docs must use `api_key` and `model_name`.

## Embedding Styles

### `openai-compatible`

Required fields:

- `endpoint`
- `model_name`
- `default_dims`

Optional fields:

- `api_key`

The client posts to `<endpoint>/embeddings` when `endpoint` already ends in `/v1`, matching the OpenAI-compatible URL shape `/v1/embeddings`.

### `gguf`

Required fields:

- `model_name`
- `default_dims`

`model_name` may be either:

- A local filesystem path to a GGUF model.
- An HTTPS URL to a GGUF model.

Default GGUF model:

```text
https://huggingface.co/CompendiumLabs/bge-small-en-v1.5-gguf/resolve/main/bge-small-en-v1.5-f32.gguf
```

Default dimensions for that model: `384`.

## Error Handling

Configuration errors must:

- Name the invalid file path.
- Name the invalid field when known.
- Exit with code `1` for runtime config failures.
- Exit with code `2` only when the failure is caused by invalid CLI usage.

Secrets such as API keys must never be printed in full.

