# Speecdex MVP Overview

## Purpose

Speecdex is a local-first command-line tool for developers who need semantic search over Markdown documentation in a project folder. It indexes Markdown files in place, stores a compact local vector index, and returns source-grounded results that include file paths, line ranges, scores, and chunk text.

The MVP optimizes for predictable local developer use rather than hosted search, multi-user collaboration, or large-scale document collections.

## Primary User Flows

### Reindex the current folder

Running `speecdex` with no search or service flags rebuilds the index for the current working directory tree.

Expected behavior:

- Discover Markdown files recursively from the current working directory.
- Apply ignore rules from Speecdex configuration.
- Chunk Markdown into overlapping text chunks.
- Generate one embedding per chunk.
- Write the local index artifact into the project-local Speecdex data area.
- Print a short human-readable summary to stdout.
- Exit non-zero when indexing cannot complete.

### Search the existing index

Running `speecdex --query "..."` searches the existing index semantically.

Expected behavior:

- Load the local index for the current working directory.
- Embed the query text using the configured embedding provider.
- Rank indexed chunks by vector similarity.
- Optionally rerank results when reranking is configured.
- Print results as fenced YAML.
- Exit non-zero when no usable index or embedding provider exists.

### Combine semantic and literal search

Running `speecdex --query "..." --text "..."` combines semantic results with literal text matches.

Expected behavior:

- Treat semantic matches and literal text matches as OR conditions.
- Include chunks that satisfy either the semantic query or at least one literal text query.
- Deduplicate chunks that match multiple sources.
- Preserve match source metadata in the output.

### Serve a local embedding model

Running `speecdex --service` starts a long-running local embedding service.

Expected behavior:

- Bind to localhost on the configured port, default `8248`.
- Serve an OpenAI-compatible `/v1/embeddings` endpoint.
- Use the configured GGUF embedding model.
- Download the configured model on startup when it is not already available.
- Keep running until interrupted.

## MVP Boundaries

Included in MVP:

- macOS development build.
- Recursive Markdown discovery.
- Project and user configuration.
- OpenAI-compatible remote embeddings.
- Optional local GGUF embedding service.
- Local binary index artifact.
- Semantic search.
- Literal text search.
- Fenced YAML output.
- Optional reranking hook when configured.

Deferred from MVP:

- Watch mode.
- Linux release builds.
- Hosted or shared indexes.
- Incremental indexing guarantees.
- Non-Markdown source formats.
- Authentication for the local service.
- Complex query language.
- Editor or IDE integration.

## Product Principles

- Local by default: source files, configuration, and index artifacts live with the developer's working tree or user profile.
- Source-grounded: every result must point back to a file path and line range.
- Configurable but small: defaults should work for common Markdown projects, and configuration should remain readable YAML.
- Provider-flexible: embeddings can come from a remote OpenAI-compatible endpoint or a local GGUF model served through the same API shape.
- Testable contracts: CLI behavior, config precedence, index metadata, and output format must be stable enough to drive tests before implementation.

