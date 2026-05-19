# Embeddings and Local Service Specification

## Embedding Requirement

Embeddings are required for:

- Indexing Markdown chunks.
- Semantic search with `--query`.

Embeddings are not required for:

- Literal-only search with `--text`.
- Reading an existing index for metadata inspection in future commands.

## Provider Selection

The active embedding provider is defined by `llms.embedding`.

Supported MVP styles:

- `openai-compatible`
- `gguf`

When semantic work is needed, provider selection follows this order:

1. If a localhost Speecdex embedding service is reachable and compatible, use it.
2. Otherwise use the configured `openai-compatible` endpoint when available.
3. Otherwise fail with a clear missing-provider error.

For `gguf` configuration, normal CLI commands do not embed directly in-process for MVP. They use the local service when available. If the service is unavailable, the command fails unless an OpenAI-compatible fallback is also configured.

## OpenAI-Compatible Embeddings

Remote and local service embedding calls use an OpenAI-compatible request and response shape.

Request:

```json
{
  "model": "model-name",
  "input": ["text one", "text two"]
}
```

Response:

```json
{
  "data": [
    {
      "index": 0,
      "embedding": [0.1, 0.2, 0.3]
    }
  ],
  "model": "model-name"
}
```

Required behavior:

- Preserve input order.
- Support batching.
- Validate vector dimensions.
- Include bearer authorization when `api_key` is configured.
- Do not log API keys.

## Local GGUF Service

`speecdex --service` starts a persistent localhost embedding service backed by a GGUF model.

Defaults:

- Host: `127.0.0.1`
- Port: `8248`
- Endpoint: `http://127.0.0.1:8248/v1/embeddings`

Required behavior:

- Serve only on localhost.
- Implement `/v1/embeddings`.
- Load the configured GGUF model.
- Download the configured GGUF model if `model_name` is an HTTPS URL and the model is not already cached.
- Print the listening URL after startup succeeds.
- Return OpenAI-compatible JSON.

## Model Cache

Downloaded GGUF models are stored under the user-level Speecdex data area.

Required behavior:

- Derive a stable local filename from the model URL.
- Reuse existing downloaded models.
- Fail clearly if download or model loading fails.

## Compatibility Checks

The CLI must confirm that an embedding provider is compatible with the index before semantic search.

Compatibility requires:

- Same embedding dimensions.
- Same model identity when known.
- Same vector distance interpretation.

If compatibility cannot be established, semantic search exits non-zero and explains that the index must be rebuilt with the active embedding provider.

