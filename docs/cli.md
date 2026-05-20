# CLI Specification

## Binary

The command name is `speecdex`.

The CLI uses the current working directory as the project root for indexing and searching unless a future spec introduces an explicit root flag.

## Commands and Flags

### `speecdex`

Rebuild the local index for the current folder tree.

Required behavior:

- Run indexing mode when no `--query`, `--text`, or `--service` flag is provided.
- Print line-oriented indexing progress to stderr while files are processed.
- Read configuration before discovery.
- Discover, chunk, embed, and store indexed chunks.
- Print a concise indexing summary to stdout.
- Print diagnostics and recoverable warnings to stderr.
- Exit with code `0` only after the index artifact is written successfully.

Summary output must include:

- Project root path.
- Number of Markdown files indexed.
- Number of chunks indexed.
- Embedding provider identity.
- Index artifact path.

Progress output must use this form:

```text
Indexing file <current>/<total>: <path> (<chunks> chunks) | elapsed <duration> | <rate> chunks/s | ETA <duration>
```

### `speecdex --query "<query>"`

Run semantic search over the existing local index.

Required behavior:

- Require a non-empty query string.
- Load the local index before embedding the query.
- Generate a query embedding using the active embedding provider.
- Return ranked results as fenced YAML on stdout.
- Print diagnostics to stderr.
- Exit non-zero when the index is missing, unreadable, or incompatible with the active embedding model.

### `speecdex --query "<query>" --text "<literal>"`

Run semantic search combined with literal text search.

Required behavior:

- Accept one or more `--text` flags.
- Treat semantic and literal matching as OR conditions.
- Search literal text case-sensitively for MVP.
- Deduplicate results by chunk identity.
- Preserve whether each result matched `semantic`, `text`, or both.

### `speecdex --text "<literal>"`

Run literal text search over indexed chunks without semantic search.

Required behavior:

- Accept one or more `--text` flags.
- Search chunk text stored in the local index.
- Avoid embedding calls when no `--query` is present.
- Return results as fenced YAML.

### `speecdex --service`

Start the local embedding service.

Required behavior:

- Bind only to localhost.
- Use default port `8248` unless configuration overrides it.
- Expose an OpenAI-compatible `/v1/embeddings` endpoint.
- Load or download the configured GGUF embedding model.
- Print the listening URL to stdout after startup succeeds.
- Continue running until interrupted.
- Exit non-zero if the service cannot bind, load the model, or initialize the embedding runtime.

## Flag Rules

- `--service` is mutually exclusive with indexing and search flags.
- `--query` requires a non-empty value after trimming whitespace.
- `--text` may be repeated.
- Empty `--text` values are invalid.
- Unknown flags exit non-zero and print usage to stderr.

## Exit Codes

Use these exit code meanings:

- `0`: command completed successfully.
- `1`: user-facing runtime failure, such as missing config, missing index, or provider failure.
- `2`: invalid CLI usage, such as bad flags or invalid flag combinations.

## Output Streams

Stdout is reserved for command results:

- Indexing summary.
- Search fenced YAML.
- Service startup URL.

Stderr is reserved for:

- Usage errors.
- Warnings.
- Provider diagnostics.
- Index compatibility errors.
- Indexing progress.

Search output must be machine-readable fenced YAML and must not include extra prose outside the fence.
