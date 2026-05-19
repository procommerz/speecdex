# AGENTS.md - Speecdex - In-Place Local Markdown Files Indexer and Vector Search Tool

This project is for a CLI tool for developers that can watch configurable paths with Markdown documents,
chunk, embed and index them in vector similarity database.

The app is written in Golang.

## Spec Docs Overview

The `docs/` directory contains the implementation contracts for the MVP. Treat these docs as the source of truth for spec-and-test-driven development before writing or changing app code.

- `docs/overview.md` describes the product intent, MVP boundaries, primary user flows, and deferred features.
- `docs/cli.md` defines the public command-line interface, supported flags, exit codes, and stdout/stderr contracts.
- `docs/configuration.md` defines config discovery, supported file names, YAML schemas, defaults, precedence, and validation rules.
- `docs/indexing.md` defines Markdown discovery, ignore handling, file reading, chunking, embedding during indexing, and indexing summaries.
- `docs/embeddings.md` defines embedding provider behavior, OpenAI-compatible calls, the local GGUF-backed embedding service, model caching, and compatibility checks.
- `docs/index-storage.md` defines the local index artifact location, required stored metadata, atomic write behavior, read behavior, and rebuild invalidation rules.
- `docs/search.md` defines semantic search, literal text search, combined OR search, optional reranking, and fenced YAML result output.
- `docs/test-plan.md` maps the MVP specs to required CLI, config, indexing, embedding, storage, search, and service tests.

When specs and older prose disagree, prefer the files under `docs/`.


## How It Works

When speecdex CLI is run in a folder, it will recursively go through all the Markdown files in this folder and its children

The app will scan the current folder for a `.speecdex` folder, where users can optionally put the `llms.yaml` or `llms.yml` config file for LLM model configurations and a `config.yaml` or `config.yml` file that can enable some overrides to the default behaviour, introduce ignored entries, exceptions etc.

Running `speecdex` in a folder by default triggers reindex. The app will use find to get all markdown files in the current folder tree, will apply ignore patterns from the config and then will chunk (with some overlapping) it, generate chunk embeddings and index them.

We do not expect the index to go over 100Mb or so, so the plan is to keep in the same folder (that's usually project root level, where the .speecdex config folder might also be). The index can be stored as some common binary file format that Golang can work with natively. The index should hold chunk metadata (file name, chunk text, chunk lines range in the original file) + the contents of the chunk.

Running `speecdex` with "--query" or "--text" arguments runs a search in the index and returns result as fenced YAML (file name, chunk text, chunk lines range).


## Building the App

We build for platforms: 

- MacOS (during development, only for MacOS)
- Linux (later, after we have a stable release)

Building is done in Docker for security reasons, but we'll test the resulting build on the host system.


## Test Configs

The `config.yaml` and `llms.yaml` in the project source folder can be used for tests.