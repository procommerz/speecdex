# Embeddings Specification

## Embedding Requirement

Embeddings are required for:

- Indexing Markdown chunks.
- Semantic search with `--query`.

Embeddings are not required for:

- Literal-only search with `--text`.
- Reading an existing index for metadata inspection in future commands.

## Provider

The active embedding provider is defined by `llms.embedding`. The only MVP
style is `openai-compatible`. Speecdex does not ship its own model runtime;
local models are served by an external OpenAI-compatible process the user
runs themselves (for example `llama-server`, Ollama, or LM Studio) and are
addressed through the same `endpoint` field as a remote provider.

If `llms.embedding` is missing when semantic work is needed, the command
fails with a clear missing-provider error.

## OpenAI-Compatible Embeddings

Embedding calls use the OpenAI `/v1/embeddings` request and response shape.

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

## Compatibility Checks

The CLI must confirm that an embedding provider is compatible with the index
before semantic search.

Compatibility requires:

- Same embedding dimensions.
- Same model identity when known.
- Same vector distance interpretation.

If compatibility cannot be established, semantic search exits non-zero and
explains that the index must be rebuilt with the active embedding provider.
