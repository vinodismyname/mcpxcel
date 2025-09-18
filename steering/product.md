# Product Overview

## MCP Excel Analysis Server

A Model Context Protocol (MCP) server built in Go that enables AI assistants to analyze and manipulate Excel spreadsheets without overwhelming their context window. The server provides smart, targeted operations that return only relevant data slices, summaries, and insights rather than entire sheets.

## Key Value Propositions

- **Context-Aware Analysis**: Prevents LLM context window exhaustion by providing selective data retrieval and server-side computation
- **Smart Tool Usage**: Enables AI assistants to explore and analyze spreadsheets through targeted operations rather than naive data dumps  
- **Concurrent Processing**: Supports parallel requests and multi-workbook operations for scalable usage
- **Bounded Operations**: All operations have configurable limits to ensure predictable performance and resource usage

## Target Users

- **Primary**: AI assistants (LLMs) acting on behalf of end users for spreadsheet analysis
- **Secondary**: Analysts and developers who configure assistants and provide workbooks for analysis

## Core Capabilities

- Multi-workbook handling with selective data retrieval
- Server-side aggregation and summary statistics
- Targeted search and filtering operations
- Safe write operations with bounded payloads
- Sequential Insights planning with domain-neutral primitives (no server LLM)
- Multiple-table detection within a sheet and schema profiling

## Repository & Releases

- Repository: https://github.com/vinodismyname/mcpxcel (MIT License)
- Default branch: `main`; protected with required PR + passing CI.
- CI: GitHub Actions at `.github/workflows/ci.yml` running `make lint`, `make test`, and `make test-race` on pushes and PRs.
- PR workflow: create a feature branch, open a PR to `main`, await green CI, squash-merge, and delete the branch.
- Versioning: Semantic Versioning (vX.Y.Z). Tags are pushed and a GitHub Release is generated with notes.
- Current version: v0.2.8
- Policy: bump the patch version for each completed task. When all tasks currently listed in `tasks.md` are complete, bump the minor version. Use extra patch bumps for hotfixes.
- Go module: `github.com/vinodismyname/mcpxcel` (ensure imports use this path).
- Documentation updates and config changes must be included in PRs alongside code changes.
