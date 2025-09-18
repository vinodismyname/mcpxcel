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
- Insight generation without raw data exposure