# Requirements Document

## Introduction

The MCP Excel Analysis Server is a Model Context Protocol (MCP) compliant service that enables AI assistants to analyze and manipulate Excel spreadsheets without overwhelming their context windows. The system exposes a set of MCP tools that provide targeted operations returning only necessary data slices, summaries, and insights, allowing AI assistants to work efficiently with large spreadsheets while maintaining processing limits and providing accurate analysis. The server operates as a local service with configurable security boundaries and supports multiple concurrent AI assistant sessions.

## Requirements

### Requirement 1 (Path-Only Access)

**User Story:** As an AI assistant, I want to operate on Excel files by path so that I don’t need transient workbook IDs and my calls are resilient to server restarts.

#### Acceptance Criteria

1. WHEN a tool is called with a `path` THEN the system SHALL validate allow-list and open or reuse an internal handle transparently
2. WHEN multiple distinct paths are used simultaneously THEN the system SHALL maintain separate internal handles without interference
3. WHEN internal capacity for open files is reached THEN the system SHALL return an error with guidance on resource limits
4. WHEN a server restarts THEN subsequent calls with the same `path` SHALL succeed without requiring migration steps

### Requirement 2

**User Story:** As an AI assistant, I want to discover the structure of Excel workbooks without loading full content, so that I can understand the data organization and plan targeted queries efficiently.

#### Acceptance Criteria

1. WHEN an AI assistant requests workbook structure THEN the system SHALL return a list of all sheet names without loading cell data
2. WHEN sheet information is requested THEN the system SHALL return row count, column count, and header row information without loading full cell data, and SHALL allow the response behavior to be tuned through configuration
3. WHEN a preview is requested THEN the system SHALL return the first 10 rows or less by default, with the preview size configurable and bound by the global payload limits
4. WHEN workbook metadata-only mode is requested OR the workbook size exceeds configured data retrieval thresholds THEN the system SHALL return only metadata (sheet names, counts, headers) plus guidance for targeted queries without streaming cell data
5. WHEN structure discovery fails THEN the system SHALL return specific error codes indicating the type of failure

### Requirement 3

**User Story:** As an AI assistant, I want to retrieve specific ranges of data from spreadsheets, so that I can access only the relevant information without overwhelming my context window.

#### Acceptance Criteria

1. WHEN a specific cell range is requested THEN the system SHALL return only that range capped at the default 10,000 cells or 128 KB payload, with both limits configurable
2. WHEN the requested range exceeds configured limits THEN the system SHALL return truncated results with a truncation flag and pagination cursor
3. WHEN an invalid range is specified THEN the system SHALL return an error with examples of valid range formats
4. WHEN range data is returned THEN the system SHALL include metadata fields for total cells, returned cells, truncation status, and `nextCursor`

### Requirement 4

**User Story:** As an AI assistant, I want to compute summary statistics on spreadsheet data, so that I can provide insights without processing raw data in my context.

#### Acceptance Criteria

1. WHEN summary statistics are requested for a column THEN the system SHALL return count, sum, average, min, max, and distinct count
2. WHEN statistics computation is requested THEN the system SHALL complete for datasets up to 100,000 cells while honoring configured resource limits, with the supported dataset size configurable
3. WHEN group-by analysis is requested THEN the system SHALL return aggregated statistics per group without raw data
4. WHEN statistics cannot be computed THEN the system SHALL return an error explaining the limitation and suggesting alternatives

### Requirement 5

**User Story:** As an AI assistant, I want to search for specific values or patterns within spreadsheets, so that I can locate relevant data without scanning entire sheets.

#### Acceptance Criteria

1. WHEN a search query is submitted THEN the system SHALL return up to the default 50 matching results with cell locations and bounded row snapshots, with the match limit and snapshot size configurable
2. WHEN search results exceed the configured match limit THEN the system SHALL return the first page of results plus total match count and pagination cursor
3. WHEN search includes column filters THEN the system SHALL limit search to specified columns only
4. WHEN no matches are found THEN the system SHALL return an empty result set with total count of zero

### Requirement 6

**User Story:** As an AI assistant, I want to write data to specific cells and ranges in spreadsheets, so that I can update or add information based on analysis results.

#### Acceptance Criteria

1. WHEN data is written to a range THEN the system SHALL accept up to the default 10,000 cells per operation, with this limit configurable
2. WHEN write operation completes THEN the system SHALL return the number of cells successfully written
3. WHEN write operation exceeds limits THEN the system SHALL return an error suggesting batch operations
4. WHEN write operation fails THEN the system SHALL ensure no partial writes occur and return specific error information

### Requirement 7

**User Story:** As an AI assistant, I want to apply formulas and transformations across cell ranges, so that I can perform bulk calculations without manually processing each cell.

#### Acceptance Criteria

1. WHEN a formula is applied to a range THEN the system SHALL process up to the default 10,000 target cells, with this limit configurable
2. WHEN transformation completes THEN the system SHALL return confirmation and a 20-row (or configured) preview of results within the global payload limits
3. WHEN formula application fails THEN the system SHALL return specific error information and rollback any partial changes
4. WHEN preview is generated THEN the system SHALL limit preview to the default 20 rows or 10 KB (whichever is smaller), with both limits configurable

### Requirement 8

**User Story:** As an AI assistant, I want to filter spreadsheet data based on conditions, so that I can work with relevant subsets without processing entire datasets.

#### Acceptance Criteria

1. WHEN filter conditions are applied THEN the system SHALL support equals, contains, greater than, less than, greater than or equal, less than or equal, not equal, and boolean operators
2. WHEN filtered results are returned THEN the system SHALL limit output to the default 200 rows plus total count, with the row limit configurable
3. WHEN filter results exceed limits THEN the system SHALL provide pagination cursors for additional data
4. WHEN filter conditions are invalid THEN the system SHALL return error with examples of valid filter syntax

### Requirement 9

**User Story:** As an AI assistant, I want to generate insights and summaries from spreadsheet data, so that I can provide meaningful analysis without including raw data in my responses.

#### Acceptance Criteria

1. WHEN insight generation is requested THEN the system SHALL produce summary text limited to the default 2,000 characters, with this limit configurable
2. WHEN insights are generated THEN the system SHALL include key statistics, trends, and notable patterns
3. WHEN insight generation fails THEN the system SHALL return available statistics and suggest manual analysis approaches
4. WHEN insights are returned THEN the system SHALL exclude raw data tables from the response

### Requirement 10

**User Story:** As an AI assistant, I want the system to handle file size and format limitations gracefully, so that I can work with supported files and receive clear guidance for unsupported ones.

#### Acceptance Criteria

1. WHEN a file exceeds the default 20 MB limit THEN the system SHALL reject the file with FILE_TOO_LARGE error and suggest alternatives, with the limit configurable
2. WHEN an unsupported format is provided THEN the system SHALL return UNSUPPORTED_FORMAT error with conversion guidance
3. WHEN .xlsx and .xlsm files are provided THEN the system SHALL accept and process them successfully
4. WHEN .xlsm files contain macros THEN the system SHALL preserve macros as opaque content without execution

### Requirement 11

**User Story:** As an AI assistant, I want to work with the system in a stateless manner, so that I can make requests without maintaining persistent connections or sessions.

#### Acceptance Criteria

1. WHEN requests are made THEN the system SHALL NOT require persistent server-side sessions
2. WHEN workbook identifiers are provided THEN the system SHALL accept them as parameters for each operation
3. WHEN operations are retried THEN read operations SHALL be idempotent and return consistent results
4. WHEN write operations are retried THEN the system SHALL provide safe retry semantics or clear non-idempotent labeling

### Requirement 12

**User Story:** As an AI assistant, I want the system to handle concurrent requests efficiently, so that multiple operations can be performed simultaneously without conflicts.

#### Acceptance Criteria

1. WHEN multiple requests are received THEN the system SHALL support at least the default 10 concurrent requests, with concurrency level configurable
2. WHEN concurrent reads target different workbooks THEN the system SHALL process them in parallel
3. WHEN concurrent writes target the same workbook THEN the system SHALL serialize writes to maintain data integrity
4. WHEN concurrent operations exceed capacity THEN the system SHALL queue requests or return appropriate busy signals

### Requirement 13

**User Story:** As an AI assistant, I want to access files within configured directories only, so that the system operates securely within defined boundaries.

#### Acceptance Criteria

1. WHEN file access is requested THEN the system SHALL only allow access to files within configured allow-list directories
2. WHEN access is requested outside allowed directories THEN the system SHALL deny access with clear error messages
3. WHEN file permissions are insufficient THEN the system SHALL return appropriate permission error messages
4. WHEN directory configuration is invalid THEN the system SHALL fail safely and log configuration errors

### Requirement 14

**User Story:** As an AI assistant, I want to receive consistent pagination and error handling, so that I can reliably iterate through large datasets and handle failures gracefully.

#### Acceptance Criteria

1. WHEN pagination is provided THEN cursors SHALL remain stable across requests to prevent duplicates or gaps and responses SHALL include `total`, `returned`, `truncated`, and `nextCursor` metadata fields; cursors bind to file `path` and `mtime`
2. WHEN errors occur THEN the system SHALL return structured error objects containing an error code, human-readable message, and actionable guidance consistent with MCP protocol schemas
3. WHEN operations timeout THEN the system SHALL return TIMEOUT error with suggestions to narrow scope
4. WHEN validation fails THEN the system SHALL return specific validation errors with examples of correct formats
5. WHEN workbook corruption or resource contention is detected THEN the system SHALL return CORRUPT_WORKBOOK or BUSY_RESOURCE errors respectively without partially completing the operation

### Requirement 15

**User Story:** As a system integrator, I want operational limits to be configurable, so that deployments can tune performance envelopes while retaining safe defaults.

#### Acceptance Criteria

1. WHEN the server starts without explicit overrides THEN it SHALL apply the documented default limits for payload size, row counts, timeouts, and concurrency
2. WHEN configuration parameters for limits are supplied at setup THEN the server SHALL apply the overrides and expose the effective limits to clients via discovery or metadata responses
3. WHEN invalid configuration values are provided THEN the server SHALL reject startup (or reload) with a structured configuration error identifying the offending parameters

### Requirement 16

**User Story:** As an MCP client developer, I want the server to describe its tools and resources with protocol-compliant schemas, so that I can integrate reliably without reverse engineering behaviors.

#### Acceptance Criteria

1. WHEN an MCP client calls `list_tools` THEN the server SHALL enumerate each tool (open, read, search, filter, write, transform, insights, metadata-only) with JSON schema definitions covering input parameters, default limits, and response structures
2. WHEN an MCP client calls `list_resources` or retrieves a resource THEN the server SHALL expose workbook metadata, previews, and configuration references via registered resource URIs with declared MIME types and size bounds
3. WHEN tool invocations return errors THEN the server SHALL use MCP structured error payloads (code, message, actionable `nextSteps`) consistent with the error catalog defined in this document

### Requirement 17

**User Story:** As a maintainer, I want a reliable GitHub-based release workflow so that quality gates and versioned releases are consistent.

#### Acceptance Criteria

1. WHEN changes are proposed THEN they SHALL be submitted via a pull request targeting `main`.
2. WHEN CI runs on a pull request THEN it SHALL execute `make lint`, `make test`, and `make test-race` and block merges on failure.
3. WHEN a PR is merged into `main` THEN the history SHALL be squashed and the branch deleted.
4. WHEN a release is cut THEN a SemVer tag (`vX.Y.Z`) SHALL be pushed and a GitHub Release generated with notes.
5. WHEN the module path is referenced THEN it SHALL be `github.com/vinodismyname/mcpxcel`.
