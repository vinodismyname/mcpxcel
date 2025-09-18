# Feature Specification: MCP Excel Analysis Server

**Feature Branch**: `001-mcp-excel-analysis`
**Created**: 2025-09-17
**Status**: Draft
**Input**: User description: "MCP Excel Analysis Server: Architecture and Implementation Plan"

## Execution Flow (main)
```
1. Parse user description from Input
   → If empty: ERROR "No feature description provided"
2. Extract key concepts from description
   → Identify: actors, actions, data, constraints
3. For each unclear aspect:
   → Mark with [NEEDS CLARIFICATION: specific question]
4. Fill User Scenarios & Testing section
   → If no clear user flow: ERROR "Cannot determine user scenarios"
5. Generate Functional Requirements
   → Each requirement must be testable
   → Mark ambiguous requirements
6. Identify Key Entities (if data involved)
7. Run Review Checklist
   → If any [NEEDS CLARIFICATION]: WARN "Spec has uncertainties"
   → If implementation details found: ERROR "Remove tech details"
8. Return: SUCCESS (spec ready for planning)
```

---

## ⚡ Quick Guidelines
- ✅ Focus on WHAT users need and WHY
- ❌ Avoid HOW to implement (no tech stack, APIs, code structure)
- 👥 Written for business stakeholders, not developers

### Section Requirements
- **Mandatory sections**: Must be completed for every feature
- **Optional sections**: Include only when relevant to the feature
- When a section doesn't apply, remove it entirely (don't leave as "N/A")

### For AI Generation
When creating this spec from a user prompt:
1. **Mark all ambiguities**: Use [NEEDS CLARIFICATION: specific question] for any assumption you'd need to make
2. **Don't guess**: If the prompt doesn't specify something (e.g., "login system" without auth method), mark it
3. **Think like a tester**: Every vague requirement should fail the "testable and unambiguous" checklist item
4. **Common underspecified areas**:
   - User types and permissions
   - Data retention/deletion policies
   - Performance targets and scale
   - Error handling behaviors
   - Integration requirements
   - Security/compliance needs

---

# Excel Analysis & Manipulation Service (via AI Assistant) – Product Specification

## 1) Overview

**Goal.** Enable an AI assistant to analyze and manipulate Excel workbooks **without overwhelming its context window** by exposing a set of safe, targeted operations that return only the necessary slices, summaries, and results.

**Who uses it.**

* Primary: AI assistants (LLMs) acting on behalf of end users.
* Secondary: Analysts and developers who configure the assistant and provide workbooks.

**Value.**

* Faster decisions (summary- and insight‑first interactions)
* Safer automation (bounded payloads, clear limits)
* Scalable usage (parallel requests, controlled concurrency)

**Out of scope (v1).**

* Real‑time co‑editing and live cursors
* Executing macros/VBA or external data connections
* Remote file stores (e.g., S3, Google Drive)
* Authentication/authorization (local‑only, trusted environment)

---

## 2) User Scenarios & Acceptance Tests (Mandatory)

### Primary User Story

As an AI assistant, I need to analyze and manipulate Excel spreadsheets **without flooding my context**, so I can provide accurate insights and perform data operations efficiently within processing limits.

### Acceptance Scenarios

1. **Large file, trend question**
   **Given** an Excel file up to 20 MB with ≥10,000 rows,
   **When** the assistant requests *sales trend analysis*,
   **Then** the system returns only summary statistics and key insights (no raw tables), with total response payload ≤128 KB and completion ≤5 s for 100k cells.

2. **Targeted range from multi‑sheet workbook**
   **Given** a workbook with multiple sheets,
   **When** the assistant requests a specific range,
   **Then** the system returns only that range and associated headers, capped at 10,000 cells or 128 KB (whichever comes first).

3. **Bulk transformation**
   **Given** a sheet requiring a transformation across 1,000+ rows,
   **When** the assistant requests a formula/operation to apply across a defined range,
   **Then** the system applies it to up to 10,000 target cells, persists changes, and returns a confirmation plus a 20‑row sample preview.

4. **Search at scale**
   **Given** a sheet with many rows,
   **When** the assistant searches for “ACME Corp”,
   **Then** the system returns up to 50 matching locations/rows plus a total‑matches count; no full‑sheet dumps.

### Edge Cases (Expected Behavior)

* **Requested range exceeds limits** → return truncated result with `truncated=true`, include next‑page cursor.
* **Workbook in the millions of cells** → return metadata (sheet names, row/column counts, headers) and instruct targeted queries.
* **Concurrent analysis requests** → handle in parallel up to configured concurrency; serialize writes per workbook to maintain integrity.
* **Corrupt/malformed file** → return explicit error (`CORRUPT_WORKBOOK`) without crash; no partial writes.

---

## 3) Functional Requirements (Mandatory)

> **All MUST/SHOULD statements below are testable.** Numbers are defaults and MUST be configurable unless otherwise noted.

**FR‑001 – Multi‑workbook handling**
MUST open and operate on **multiple workbooks simultaneously**.

**FR‑002 – Structure discovery**
MUST return workbook structure **without loading full content**: sheet list, row/column counts, header row (if present).

**FR‑003 – Selective retrieval**
MUST retrieve by **range, rows, columns, or filter criteria**, returning at most **10,000 cells** or **128 KB** payload per call (whichever first). Include paging cursors when truncating.

**FR‑004 – Summary statistics**
MUST compute on demand (count, sum, average, min, max, distinct count) over specified columns/ranges **without returning raw rows**.

**FR‑005 – Search**
MUST search within a sheet for a string/pattern and return up to **50 results** (location + row snapshot) and a **total match count**; include paging cursors.

**FR‑006 – Bounded writes**
MUST write to specific cells/ranges with per‑operation cap of **10,000 cells**; return number of cells written.

**FR‑007 – Range transformations**
MUST apply a formula or transformation across a specified range (up to **10,000 target cells**). Return confirmation and a **20‑row preview** (or ≤10 KB, whichever smaller).

**FR‑008 – Server‑side filtering**
MUST filter by conditions (e.g., equals, contains, >, <, ≥, ≤, !=; AND/OR across named columns) and return up to **200 rows** + total count; include paging cursors.

**FR‑009 – Insight generation**
MUST produce an *insight summary* (bullet points or short narrative) based on computed stats/trends **without including raw tables**; response ≤**2,000 characters**.

**FR‑010 – File size limits (clarified)**
MUST refuse files **>20 MB** with `FILE_TOO_LARGE` and suggest narrowing or splitting. Limit is configurable but MUST default to 20 MB.

**FR‑011 – File formats (clarified)**
MUST accept **.xlsx** and **.xlsm**.
SHOULD preserve macros in .xlsm as opaque content (no execution).
MUST reject legacy **.xls** with `UNSUPPORTED_FORMAT` (v1).

**FR‑012 – Session model (clarified)**
MUST NOT require persistent server‑side sessions between calls. Stateless request handling is acceptable; callers provide necessary identifiers each call.

**FR‑013 – Authentication (clarified)**
MUST operate in a **local‑only trusted** mode (no auth) for v1.
SHOULD support auth in future versions (non‑blocking for v1).

**FR‑014 – Concurrency (clarified)**
MUST support **parallel requests**.
MUST allow **concurrent reads across different workbooks**.
MUST **serialize writes per workbook** (no overlapping writes to same file).
SHOULD support at least **10 concurrent requests** overall by default.

**FR‑015 – Access level (clarified)**
MUST support **read and write** access to files within a **configured allow‑list of local directories**. Access outside allow‑list MUST be denied.

**FR‑016 – Metadata‑only mode**
MUST provide a mode returning only **metadata and previews** (e.g., headers + first N rows) to guide targeted queries.

**FR‑017 – Deterministic paging**
MUST provide stable pagination (cursor/offset) for `read`, `filter`, and `search` so callers can iterate without duplicates or gaps.

**FR‑018 – Idempotent replays**
MUST ensure read/filter/search operations are idempotent under retries; write operations MUST include safe re‑try semantics or explicit non‑idempotent labeling.

**FR‑019 – Validation & guardrails**
MUST validate requested ranges, filters, and write sizes; MUST reject ambiguous inputs with actionable error messages and examples.

---

## 4) Non‑Functional Requirements

**NFR‑001 – Payload bounds**

* Default max response payload: **128 KB** per call (hard cap).
* Text insight length: **≤2,000 chars**.
* Preview samples: **≤200 rows** or **50 KB**, whichever smaller.

**NFR‑002 – Reliability**

* No crashes on corrupt or oversized files; return structured errors.
* Writes are atomic at the file level (no partial, corrupt saves).
* 99% of valid requests complete within the performance SLOs above.

**NFR‑003 – Concurrency & isolation**

* At least **10 concurrent requests** supported.
* Per‑workbook **write lock** enforced; concurrent reads allowed.
* No cross‑request data leakage.

**NFR‑004 – Usability (for AI assistants)**

* Clear, concise error messages with *“what to try next”* hints (e.g., narrower ranges, add filters).
* Each operation returns **self‑describing metadata** (counts, cursors, truncation flags) so assistants can plan next steps.

---

## 5) Capabilities Catalog (AI‑Facing Operations)

> Names are descriptive; exact wire names may differ. Inputs/outputs are conceptual for stakeholder review.

### A. Discovery & Access

* **List Workbook Sheets** → sheets\[]
* **Get Sheet Info** → rows, columns, header row (if present), sample preview (≤10 rows)
* **Open Workbook / Close Workbook** *(optional for v1 if stateless)*

### B. Targeted Retrieval

* **Read Range** → cells as a 2D array (capped to 10k cells / 128 KB) + truncation and paging cursors
* **Search** → up to 50 matches (sheet, cell address, row snapshot), total count, next cursor
* **Filter Rows** → up to 200 rows matching conditions, total count, next cursor

### C. Analysis & Insights

* **Summary Stats** → per column: count, sum, avg, min, max, distinct count; optional group‑by
* **Trends & Comparisons** → differences over time or between groups (bounded output)
* **Generate Insights** → short narrative/bullets summarizing notable patterns (no raw tables)

### D. Write & Transform

* **Write Range** → write up to 10k cells; return count written, sample preview
* **Apply Transformation** → apply a formula/rule across a target range (≤10k cells); return confirmation + sample preview

**Common Contracts (all operations):**

* **Limits & Flags:** Every response includes `total`, `returned`, `truncated` (bool), and `nextCursor` when relevant.
* **Validation:** Clear errors for invalid sheet/range/column names.
* **Idempotency:** Reads are idempotent; writes return a stable operation ID and a summary for audit.

---

## 6) Error Handling & User Guidance

* **FILE_TOO\LARGE** (file >20 MB): “File exceeds 20 MB. Try filtering columns, saving as a smaller workbook, or splitting into sheets.”
* **UNSUPPORTED_FORMAT** (.xls, unknown): “Unsupported file format. Save as .xlsx/.xlsm and retry.”
* **CORRUPT_WORKBOOK**: “Workbook appears malformed and cannot be read.”
* **INVALID_RANGE / INVALID_FILTER**: “Requested range/filter is invalid. Example of a valid request: …”
* **WRITE_LIMIT_EXCEEDED**: “Write exceeds per‑operation cap (10k cells). Split into smaller batches.”
* **BUSY_RESOURCE**: “Workbook is being modified. Try again shortly.”
* **TIMEOUT**: “Operation exceeded time limit. Narrow the scope or use filters.”

All errors MUST include: error code, human‑readable message, and an actionable “next step.”

---

## 7) Constraints, Assumptions & Dependencies

**Constraints**

* Local file system only (v1).
* Max workbook size: 20 MB (configurable; default enforced).
* Supported formats: .xlsx, .xlsm (macros preserved but never executed).
* Returned data is **always bounded** by size and row caps.

**Assumptions**

* Assistants can make **multi‑step** calls (e.g., discover → select → analyze).
* Users have read/write OS permissions to target files.
* Run‑time environment has sufficient CPU/RAM for the SLOs above.

**Dependencies**

* Operating system file services.
* An LLM‑capable client that can plan with truncation flags, cursors, and counts (typical for tool‑using assistants).


## 8) Glossary

* **Assistant / LLM**: The AI system orchestrating operations.
* **Workbook**: An Excel file containing one or more **Sheets**.
* **Range**: A rectangular selection of cells (e.g., A1\:D20).
* **Filter**: A server‑side condition selecting subsets of rows.
* **Preview**: A small sample (e.g., first 10–20 rows) returned to guide next steps.
* **Cursor**: A token to request the next page from a paginated result.


---

## Review & Acceptance Checklist
*GATE: Automated checks run during main() execution*

### Content Quality
- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

### Requirement Completeness
- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

---

## Execution Status
*Updated by main() during processing*

- [x] User description parsed
- [x] Key concepts extracted
- [x] Ambiguities marked
- [x] User scenarios defined
- [x] Requirements generated
- [x] Entities identified
- [x] Review checklist passed

---