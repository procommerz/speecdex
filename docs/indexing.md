# Indexing Specification

## Trigger

Running `speecdex` with no search or service flags rebuilds the local index for the current working directory tree.

By default, indexing performs an intelligent rebuild against a compatible existing index. Files whose normalized content checksum is unchanged reuse their stored chunks and vectors. Files whose checksum changed are rechunked and reembedded. Running `speecdex --force` disables checksum reuse for currently discovered files.

## Markdown Discovery

Speecdex recursively discovers Markdown files under the project root.

Included extensions:

- `.md`
- `.markdown`

Matching is case-insensitive.

Required behavior:

- Traverse subdirectories recursively.
- When `config.only_entries` is non-empty, include only Markdown files that match at least one configured entry.
- Ignore configured entries before reading file contents.
- Ignore the Speecdex data directory.
- Use paths relative to the project root in stored metadata and output.
- Process files in stable lexical order to keep index output reproducible.

## Include-Only Rules

Include-only rules come from `config.only_entries`.

Required behavior:

- Empty or omitted include-only rules include the full project tree.
- Exact directory names include that directory and its children.
- Exact file names include matching files.
- Relative path patterns match from the project root.
- Glob patterns support `*`.
- Ignore rules apply after include-only rules, so ignored files must not be read, chunked, embedded, or stored even when they also match `only_entries`.
- The Speecdex data directory is always ignored.

## Ignore Rules

Ignore rules come from `config.ignored_entries`.

Required behavior:

- Exact directory names ignore that directory and its children.
- Exact file names ignore matching files.
- Relative path patterns match from the project root.
- Glob patterns support `*`.
- Ignored files must not be read, chunked, embedded, or stored.

## File Reading

Markdown files are read as UTF-8.

Required behavior:

- Preserve original line numbering.
- Normalize line endings to `\n` for chunk text.
- Fail indexing with a clear error when a matched Markdown file cannot be read as UTF-8.

## Chunking

The MVP chunks Markdown by approximate character count with overlap.

Defaults:

- `chunk_size`: `1200`
- `chunk_overlap`: `200`

Required behavior:

- Prefer chunk boundaries at Markdown headings, blank lines, or paragraph boundaries when practical.
- Preserve non-empty chunk text.
- Record inclusive 1-based start and end line numbers for each chunk.
- Preserve enough overlap that adjacent chunks can share context.
- Never produce an empty chunk.
- Avoid splitting a single line unless the line is longer than `chunk_size`.

When a single physical line exceeds `chunk_size`, split subchunks keep the same inclusive 1-based line range. They are distinguished by chunk text and deterministic chunk ID; rune offsets are deferred beyond the MVP.

Chunk IDs must be deterministic for the same relative path, line range, chunk text, and embedding model identity.

## Embedding

Indexing generates one embedding per chunk using the active embedding provider.

Required behavior:

- Use the embedding model identity and dimensions from configuration.
- Validate that returned vector dimensions match the configured dimensions.
- Fail the indexing run if any chunk cannot be embedded.
- Do not call the embedding provider for chunks reused from an unchanged file checksum.
- Write the index only after all files and chunks have been processed successfully.

## Git Branch Metadata

Indexing stores the current git branch in the index header when it can be detected.

Required behavior:

- Detect the branch for the project root being indexed.
- Store an empty branch name when git is unavailable, the project root is not a git worktree, the checkout is detached, or no branch is returned.
- Do not print warnings or fail indexing when branch detection is unavailable.
- Do not force rechunking or reembedding solely because the current git branch changed.

## Deleted Source Files

When a source Markdown file that existed in a compatible previous index is no longer discovered, indexing preserves its existing chunks and source checksum but marks the source and chunks as deleted.

Required behavior:

- Deleted chunks must remain in the index artifact with their original checksum and vector.
- Deleted chunks must not be included in semantic or literal search.
- If the same relative path is later discovered with the same checksum, indexing must undelete and reuse those chunks without rechunking or reembedding.
- If the same relative path is later discovered with a different checksum, indexing must discard the old deleted chunks and rechunk/reembed the current file.
- `--force` must rechunk/reembed restored current files even when the checksum matches, while still preserving chunks for files that remain missing.

## Progress

Indexing prints line-oriented progress to stderr as files are processed.

Each progress line must include:

- Current file number out of total files.
- Current relative file path.
- Number of chunks for that file.
- Mean chunk length for that file, in characters.
- Elapsed time.
- Current cumulative chunks-per-second rate.
- ETA based on remaining chunks.

Progress lines must use this form:

```text
Indexing file <current>/<total>: <path> (<chunks> chunks, mean <chars> chars) | elapsed <duration> | <rate> chunks/s | ETA <duration>
```

## Index Summary

Successful indexing prints a summary to stdout.

Required fields:

- Project root.
- Markdown files discovered.
- Markdown files indexed.
- Chunks indexed.
- Ignored entries count.
- Embedding provider identity.
- Index artifact path.
- Indexed git branch when available.

Warnings, such as ignored unreadable directories, print to stderr.

Checksum rebuild diagnostics, including reused, embedded, and retained-deleted chunk counts, print to stderr.
