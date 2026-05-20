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
- `speecdex --init` creates missing project config files without loading runtime config.
- `speecdex --init` preserves existing `.yaml` configs and treats existing `.yml` configs as present.
- `speecdex --install-skill` installs the packaged docs-search skill into detected local Codex and Claude Code project folders without overwriting existing skills.
- Invalid flag combinations exit with code `2`.
- Runtime failures exit with code `1`.

## Configuration Tests

Required scenarios:

- Discovers user-level `~/.speecdex/config.yaml`.
- Discovers project-level `.speecdex/config.yaml`.
- Supports `.yaml` and `.yml`.
- Uses `.yaml` over `.yml` in the same scope.
- Applies project config over user config.
- Parses `config.only_entries`.
- Parses `config.ignored_entries`.
- Parses OpenAI-compatible embedding config.
- Accepts `apiKey` and `name` aliases for compatibility.
- Redacts API keys in errors and logs.

## Indexing Tests

Required scenarios:

- Recursively finds `.md` and `.markdown` files.
- Treats Markdown extensions case-insensitively.
- Applies include-only `config.only_entries` before ignore rules.
- Ignores `.git`, exact files, relative paths, and `*` glob patterns.
- Always ignores `.speecdex`, even when matched by `only_entries`.
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
- Fails clearly when `llms.embedding` is missing.

Use fake HTTP servers for embedding provider tests.

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

## Acceptance Criteria

The MVP implementation is complete when:

- Every public behavior in these specs has automated coverage.
- The CLI can index a temporary Markdown project with a fake embedding provider.
- The CLI can search that index semantically and literally.
- Search output is valid fenced YAML.
- No test depends on external network access or real user configuration.
