# Speecdex MVP Test Plan

## Test Strategy

The implementation should be built spec-first and test-driven. Tests should prefer black-box CLI behavior for public contracts and smaller package-level tests for parsing, chunking, storage, and ranking.

Use temporary directories for project roots and user config homes. Tests must not depend on the developer's real `~/.speecdex` directory.

## CLI Tests

Required scenarios:

- `speecdex` with no flags runs indexing mode.
- `speecdex --query "root business object definitions"` runs semantic search.
- `speecdex --text "extends BusinessObject"` runs literal-only search.
- `speecdex --query "root" --text "extends BusinessObject"` runs combined OR search.
- `speecdex --service` starts service mode and rejects search/index flags.
- Invalid flag combinations exit with code `2`.
- Runtime failures exit with code `1`.

## Configuration Tests

Required scenarios:

- Discovers user-level `~/.speecdex/config.yaml`.
- Discovers project-level `.speecdex/config.yaml`.
- Supports `.yaml` and `.yml`.
- Uses `.yaml` over `.yml` in the same scope.
- Applies project config over user config.
- Parses `config.ignored_entries`.
- Parses OpenAI-compatible embedding config.
- Parses GGUF embedding config.
- Accepts `apiKey` and `name` aliases for compatibility.
- Redacts API keys in errors and logs.

## Indexing Tests

Required scenarios:

- Recursively finds `.md` and `.markdown` files.
- Treats Markdown extensions case-insensitively.
- Ignores `.git`, exact files, relative paths, and `*` glob patterns.
- Normalizes line endings to `\n`.
- Preserves 1-based inclusive line ranges.
- Produces deterministic chunk IDs.
- Produces no empty chunks.
- Writes the index only after all embeddings succeed.
- Leaves a previous valid index intact when a rebuild fails.

## Embedding Tests

Required scenarios:

- Sends OpenAI-compatible embedding requests.
- Preserves response ordering.
- Validates vector dimensions.
- Uses bearer authorization when `api_key` is set.
- Does not log API keys.
- Uses reachable local service before remote endpoint.
- Falls back to configured OpenAI-compatible endpoint when local service is unavailable.
- Fails clearly when a GGUF-only config is used without a running local service.

Use fake HTTP servers for remote and local embedding provider tests.

## Storage Tests

Required scenarios:

- Writes `.speecdex/index.bin`.
- Creates `.speecdex` when missing.
- Stores header metadata, source file records, chunk records, and vectors.
- Rejects unsupported format versions.
- Rejects incompatible embedding dimensions.
- Rejects incompatible model identity when known.
- Reads enough chunk metadata to produce search output without reading source Markdown files.

## Search Tests

Required scenarios:

- Semantic search ranks by descending similarity.
- Literal-only search does not call embeddings.
- Multiple `--text` values use OR semantics.
- Combined semantic and literal search uses OR semantics.
- Combined search deduplicates chunks by chunk ID.
- Results include `semantic`, `text`, or both in `match_sources`.
- Empty results print valid fenced YAML and exit `0`.
- Missing index exits non-zero and does not rebuild.

## Service Tests

Required scenarios:

- Service binds to `127.0.0.1:8248` by default.
- Configured port overrides the default.
- `/v1/embeddings` accepts OpenAI-compatible requests.
- `/v1/embeddings` returns embeddings in OpenAI-compatible response shape.
- Service reports startup URL on stdout.
- Service exits non-zero when the port is unavailable.
- Service reports model download or load failures clearly.

## Acceptance Criteria

The MVP implementation is complete when:

- Every public behavior in these specs has automated coverage.
- The CLI can index a temporary Markdown project with a fake embedding provider.
- The CLI can search that index semantically and literally.
- Search output is valid fenced YAML.
- The local service API is testable with fake or fixture embedding behavior.
- No test depends on external network access or real user configuration.

