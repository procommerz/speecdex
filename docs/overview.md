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

## MVP Boundaries

Included in MVP:

- macOS development and release builds.
- Linux release builds.
- Recursive Markdown discovery.
- Project and user configuration.
- OpenAI-compatible embeddings (remote or local OpenAI-compatible runtime).
- Local binary index artifact.
- Semantic search.
- Literal text search.
- Fenced YAML output.
- Optional reranking hook when configured.

Deferred from MVP:

- Watch mode.
- Hosted or shared indexes.
- Incremental indexing guarantees.
- Non-Markdown source formats.
- Complex query language.
- Editor or IDE integration.

## Product Principles

- Local by default: source files, configuration, and index artifacts live with the developer's working tree or user profile.
- Source-grounded: every result must point back to a file path and line range.
- Configurable but small: defaults should work for common Markdown projects, and configuration should remain readable YAML.
- Provider-flexible: embeddings come from any OpenAI-compatible endpoint, whether remote or served locally by a runtime the user already runs.
- Testable contracts: CLI behavior, config precedence, index metadata, and output format must be stable enough to drive tests before implementation.
