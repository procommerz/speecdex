# Search Specification

## Search Modes

Speecdex supports three MVP search modes:

- Semantic search with `--query`.
- Literal text search with `--text`.
- Combined OR search with both `--query` and one or more `--text` flags.

All search modes read from the local index artifact. Search commands do not discover Markdown files and do not rebuild the index.

## Semantic Search

Semantic search embeds the query string and compares it with indexed chunk vectors.

Required behavior:

- Reject empty queries.
- Validate index compatibility before ranking.
- Use cosine similarity for MVP unless the index declares a different supported metric.
- Sort results by descending score.
- Return a bounded number of results using an implementation default of `10`.

The result limit may become configurable later. For MVP, tests should assert that results are bounded and ranked, not that a flag exists.

## Literal Text Search

Literal text search scans stored chunk text in the index.

Required behavior:

- Accept one or more `--text` flags.
- Match case-sensitively for MVP.
- Treat multiple `--text` values as OR conditions.
- Avoid embedding calls when no semantic query is present.
- Score literal-only matches below semantic matches unless no semantic query is present.

## Combined Search

When `--query` and `--text` are both provided:

- Include chunks that match the semantic query.
- Include chunks that match at least one literal text value.
- Deduplicate by chunk ID.
- Preserve all match sources for each result.
- Sort primarily by best available score, with deterministic tie-breaking by path and start line.

## Optional Reranking

Reranking is optional.

When `llms.reranking` is configured:

- Apply reranking after initial semantic and text candidate collection.
- Preserve the same output schema.
- Include rerank score when available.

When reranking is not configured:

- Search must still work.
- Output must omit rerank-specific fields or set them to `null` consistently.

## Output Format

Search output is fenced YAML on stdout and contains no prose outside the fence.

Required shape:

````text
```yaml
results:
  - file: docs/example.md
    start_line: 10
    end_line: 24
    score: 0.8123
    match_sources:
      - semantic
      - text
    matched_text:
      - extends BusinessObject
    text: |
      Chunk text appears here.
```
````

Required fields per result:

- `file`
- `start_line`
- `end_line`
- `score`
- `match_sources`
- `text`

Optional fields:

- `matched_text`
- `rerank_score`

## Empty Results

Empty searches still print valid fenced YAML:

````text
```yaml
results: []
```
````

The command exits with code `0` when the search ran successfully, even if no results matched.

