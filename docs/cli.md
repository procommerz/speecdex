# CLI Specification

## Binary

The command name is `speecdex`.

The CLI uses the current working directory as the project root for indexing and searching unless a future spec introduces an explicit root flag.

## Commands and Flags

### `speecdex`

Rebuild the local index for the current folder tree, reusing unchanged file chunks from a compatible previous index when possible.

Required behavior:

- Run indexing mode when no `--query`, `--text`, `--service`, `--show-branch`, `--init`, or `--install-skill` flag is provided.
- Print line-oriented indexing progress to stderr while files are processed.
- Read configuration before discovery.
- Store the current git branch in the index header when git is available and the project root is inside a git worktree.
- Discover Markdown files, compare file content checksums to a compatible previous index, and only rechunk/reembed current files whose checksum changed or whose chunks are missing.
- Preserve chunks for deleted source files as deleted index records; deleted chunks must not appear in search results.
- Print checksum rebuild diagnostics to stderr, including reused, embedded, and retained-deleted chunk counts.
- Print a concise indexing summary to stdout.
- Print diagnostics and recoverable warnings to stderr.
- Exit with code `0` only after the index artifact is written successfully.

### `speecdex --force`

Rebuild the local index for the current folder tree without reusing checksum-matched chunks for currently discovered files.

Required behavior:

- Ignore previous checksums for currently discovered files.
- Rechunk and reembed all currently discovered Markdown files.
- Preserve deleted chunks for files that are still missing.
- Exit with code `2` if combined with `--query`, `--text`, or `--service`.

Summary output must include:

- Project root path.
- Number of Markdown files indexed.
- Number of chunks indexed.
- Embedding provider identity.
- Index artifact path.
- Indexed git branch when available.

Progress output must use this form:

```text
Indexing file <current>/<total>: <path> (<chunks> chunks, mean <chars> chars) | elapsed <duration> | <rate> chunks/s | ETA <duration>
```

### `speecdex --init`

Initialize project-scope Speecdex configuration in the current folder.

Required behavior:

- Create `<project-root>/.speecdex/` when missing.
- Create `.speecdex/config.yaml` from the packaged default app config when neither `config.yaml` nor `config.yml` exists.
- Create `.speecdex/llms.yaml` from the packaged default model config when neither `llms.yaml` nor `llms.yml` exists.
- Treat an existing `.yaml` or `.yml` file for the same config type as existing config and do not create a duplicate.
- Never overwrite existing config files.
- Do not load runtime configuration, discover Markdown files, embed text, search, or rebuild the index.
- Print a concise initialization summary to stdout.
- Print filesystem errors to stderr and exit non-zero.

### `speecdex --install-skill`

Install the packaged `docs-search` skill into local project-scoped AI coding agent folders.

Required behavior:

- Detect Codex when `<project-root>/.codex/` exists or `<project-root>/AGENTS.md` exists.
- Detect Claude Code when `<project-root>/.claude/` exists or `<project-root>/CLAUDE.md` exists.
- When a marker file exists but its agent folder is missing, create the missing agent folder under the project root.
- Install the packaged skill file to `.codex/skills/docs-search/SKILL.md` and/or `.claude/skills/docs-search/SKILL.md` for detected agents.
- Never read or write global home-directory agent settings.
- Never overwrite an existing `SKILL.md`; report it as existing instead.
- Do not load runtime configuration, discover Markdown files, embed text, search, or rebuild the index.
- If no supported local agent installation is detected, print a concise no-op summary and exit `0`.
- Print filesystem errors to stderr and exit non-zero.

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

### `speecdex --show-branch`

Print the git branch stored in the existing local index and exit.

Required behavior:

- Load the local index before reading branch metadata.
- Print the stored branch name to stdout followed by a newline when available.
- Print nothing and exit `0` when the index has no stored branch.
- Exit non-zero when the index is missing, unreadable, or unsupported.
- Do not discover Markdown files, embed text, or rebuild the index.

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
- `--force` is valid only for indexing.
- `--show-branch` is mutually exclusive with `--query`, `--text`, `--force`, and `--service`.
- `--init` is mutually exclusive with all other flags.
- `--install-skill` is mutually exclusive with all other flags.
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
- Initialization summary.
- Skill installation summary.
- Search fenced YAML.
- Stored branch output from `--show-branch`.
- Service startup URL.

Search YAML includes the branch stored in the index as a top-level
`indexed_branch` field after `results`. When no branch is stored, the field is
an empty string.

Stderr is reserved for:

- Usage errors.
- Warnings.
- Provider diagnostics.
- Index compatibility errors.
- Indexing progress.

Search output must be machine-readable fenced YAML and must not include extra prose outside the fence.
