# Index Storage Specification

## Artifact Location

The MVP stores the index in the project-local Speecdex data area.

Default artifact path:

```text
.speecdex/index.bin
```

The `.speecdex` directory may also contain user-authored configuration files. Implementations must avoid overwriting configuration files when writing index artifacts.

## Format

The index is a Go-native binary file for MVP.

The exact binary encoding is an implementation detail, but the artifact must include enough metadata to:

- Reject incompatible versions.
- Validate embedding model compatibility.
- Reconstruct search output without reading source files.
- Rebuild deterministically from source files and config.

## Required Stored Fields

Index header:

- Format name: `speecdex-index`.
- Format version.
- Created timestamp.
- Project root identity.
- Embedding provider style.
- Embedding model identity.
- Embedding dimensions.
- Distance metric.
- Chunking settings.

Source file records:

- Relative path.
- Content hash.
- File size.
- Modified timestamp when available.

Chunk records:

- Deterministic chunk ID.
- Source file relative path.
- Start line, inclusive and 1-based.
- End line, inclusive and 1-based.
- Chunk text.
- Embedding vector.

## Write Behavior

Index writes must be atomic from the user's perspective.

Required behavior:

- Write to a temporary artifact in the same directory.
- Replace the previous index only after the new index is complete.
- Leave the previous usable index intact if indexing fails.
- Create the Speecdex data directory when missing.

## Read Behavior

Search commands load the index before executing search.

Required behavior:

- Fail clearly when the index is missing.
- Fail clearly when the index version is unsupported.
- Fail clearly when embedding dimensions or model identity are incompatible.
- Never silently rebuild during a search command.

## Rebuild Invalidation

Running `speecdex` performs a full rebuild.

The new index should differ when any of these inputs differ:

- Markdown file contents.
- Markdown file set.
- Ignore rules.
- Chunking settings.
- Embedding model identity.
- Embedding dimensions.

## Size Target

The expected MVP index size is under approximately `100 MB` for normal developer documentation projects.

If an index exceeds this size, MVP behavior may still succeed, but future specs may add warnings or size controls.

