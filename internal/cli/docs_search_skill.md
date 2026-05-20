---
name: docs-search
description: Semantic and literal text search in pre-indexed specification and documentation markdown files of the project.
---

# Docs Search

Use this skill when you need to find project documentation or specifications before answering, coding, reviewing, or planning. The `speecdex` CLI is expected to be available in the user's `PATH`.

## Workflow

Check that the indexed branch matches the current one using the command that will output the indexed branch:

```sh
speecdex --show-branch
```

Run all `speecdex` commands from the project root.

If the index is missing, stale, or search fails because the index cannot be read, refresh it first:

```sh
speecdex
```

For large documentation full reindex might take up to 10 minutes. If there are many markdown files in the repo, confirm reindex with the user first.

## Search

Choose the search mode by intent:

- Use semantic search for concepts, behavior, product intent, and spec discovery:

  ```sh
  speecdex --query "configuration precedence and validation rules"
  ```

- Use literal search for exact identifiers, flags, filenames, schema keys, error text, and quoted phrases:

  ```sh
  speecdex --text "interface ExactInterfaceName {"
  ```

- Combine semantic and literal search when both conceptual and exact matching matter. Repeated `--text` flags are OR matches:

  ```sh
  speecdex --query "semantical meaning query" --text "literal_match_option1" --text "literal_match_option2"
  ```

Prefer several focused searches over one overloaded query. Start broad with `--query`, then narrow with `--text` values from promising results.

## Handling Results

Search output on stdout is fenced YAML and is the authoritative result. Do not treat stderr diagnostics, warnings, usage text, provider errors, or index compatibility errors as search evidence.

Use these fields to decide what to read next:

- `file`: source documentation path.
- `start_line` and `end_line`: line range for the matched chunk.
- `text`: source-grounded chunk content.
- `score`: ranking signal, especially for semantic matches.
- `match_sources`: whether the chunk matched `semantic`, `text`, or both.
- `indexed_branch`: git branch stored when the index was written.

Always read the referenced source file range before making code, spec, or review decisions. Search results identify likely evidence; the source document is the contract.

Empty results are still successful search output when the command exits `0`:

```yaml
results: []
indexed_branch: ""
```

## Source-of-Truth Rules

Prefer current spec docs under `docs/` over README prose when they disagree. Use README for orientation and examples, then confirm behavior in the relevant `docs/` file before acting.
