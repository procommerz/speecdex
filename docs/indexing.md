# Indexing Specification

## Trigger

Running `speecdex` with no search or service flags rebuilds the local index for the current working directory tree.

Indexing is a full rebuild for MVP. Incremental indexing may be added later but is not required.

## Markdown Discovery

Speecdex recursively discovers Markdown files under the project root.

Included extensions:

- `.md`
- `.markdown`

Matching is case-insensitive.

Required behavior:

- Traverse subdirectories recursively.
- Ignore configured entries before reading file contents.
- Ignore the Speecdex data directory.
- Use paths relative to the project root in stored metadata and output.
- Process files in stable lexical order to keep index output reproducible.

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
- Write the index only after all files and chunks have been processed successfully.

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

Warnings, such as ignored unreadable directories, print to stderr.
