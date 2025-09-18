# Task 10 Implementation Plan — Sequential Insights (Generalized Thought Tracker)

## Objective
Design and implement a domain‑neutral sequential thinking capability for MCPXcel that helps an MCP client (LLM) iterate using a generalized thought tracker (no domain heuristics) plus separate bounded primitives. The server records thoughts and loop counters and always emits a tiny planning meta card — without embedding any LLM logic or making tool recommendations.

## Success Criteria
- Expose `sequential_insights` thought tracker returning:
  - `thought_number`, `total_thoughts`, `next_thought_needed`, `session_id`
  - `branches[]`, `thought_history_length`, optional `insight_cards[]`, `meta`.
- Implement bounded primitives (config‑gated): change/variance, driver ranking, composition/mix shift, concentration (Top‑N share, HHI), robust outliers (modified z‑score), funnel analysis.
- Add multiple‑table detection per sheet, role inference and data quality checks.
- Maintain caps (≤10k cells, ≤128KB payload) with streaming iterators and clear truncation/assumption metadata.

## Non‑Goals
- No server‑embedded LLM (narrative/strategy remains client‑side).
- No broad forecasting/seasonality modeling beyond simple comparisons.
- In‑memory session state only (bounded recent history), no persistence.

## API Additions (Tool Sketches)
- `sequential_insights` (thought tracker)
  - Input: `thought`, `next_thought_needed`, `thought_number`, `total_thoughts`, optional revision/branch fields, `session_id?`, `reset_session?`, flag `show_available_tools`.
  - Output: `thought_number`, `total_thoughts`, `next_thought_needed`, `session_id`, `branches[]`, `thought_history_length`, always-on `insight_cards[]` (tiny planning cue), `meta`.
- `detect_tables`
  - Discover rectangular table ranges in a sheet via streaming scan; return Top‑K ranges with header preview + confidence.
- `profile_schema`
  - Role inference (measure/dimension/time/id/target) with ≤100‑row sampling; quality checks: missingness, duplicate IDs, negative in nonnegative fields, >100% in percent‑like, mixed types.
- `composition_shift`
  - Share‑of‑total comparisons and ±5pp highlight threshold; net effect on KPI.
- `concentration_metrics`
  - Top‑N share and HHI (bands: <0.15 unconcentrated, 0.15–0.25 moderate, >0.25 high).
- `funnel_analysis`
  - Stage detection from column names/hints; step and cumulative conversion; bottleneck identification; optional segment overlays.

All new tools follow typed schemas via `mcp.NewTool` and `mcp.NewTypedToolHandler` with JSON schema tags and default limits surfaced in `list_tools`.

## Planning Flow (Client-Orchestrated)
1) LLM calls `sequential_insights` with a thought and counters; receives updated counters, session_id, and a planning card with an interleaving cue.
2) LLM chooses domain tools via `list_tools` descriptions (e.g., `detect_tables`, `profile_schema`, `composition_shift`, `concentration_metrics`, `funnel_analysis`).
3) After each domain tool call, LLM summarizes observations as its next thought and re-calls `sequential_insights` with the same `session_id`.
4) Repeat until `next_thought_needed=false`.

## Algorithms & Constraints (Streaming‑Friendly)
- Table Detection
  - Scan grid streaming; seed header candidates by row with high text ratio and unique headings; grow rectangles down/right until type consistency degrades or blank barriers; rank candidates (header confidence, size, type stability) and return Top‑K.
- Role Inference (≤100 row sample / col)
  - Dimension: mostly non‑numeric, low cardinality relative to rows.
  - Measure: numeric with meaningful aggregation; detect percent‑like.
  - Time: date/time parse success + monotonic or regular spacing; detect grain (daily/weekly/monthly) and require ≥2 periods.
  - ID: high uniqueness ratio; optional duplicate check for anomalies.
  - Target: string/column hints (plan/budget/target) or paired naming against measure.
- Change/Variance
  - Aggregate KPI by current vs baseline; Δabs, Δ% and contribution by segment; Top‑N + “Other”.
- Composition/Mix Shift
  - Share of segment across periods; highlight ±5pp moves; quantify net effect on KPI if interpretable.
- Concentration
  - Top‑N share; HHI bands; call out dependency risks.
- Outliers
  - Modified z‑score with median/MAD; |z|≥3.5; report ≤5; require ≥5 points; conservative phrasing.
- Funnel
  - Ordered stage columns (name patterns/hints); compute step and cumulative conversion; identify biggest stage loss; optional segment overlay.
- Data Quality
  - Missingness %, type mix, negative in nonnegative measures, >100% for percent‑like, duplicate IDs; always include as assumptions.

## Config & Safety
- Planning card: always on; no server LLM; deterministic wording.
- Thresholds: `max_groups`, `top_n`, `mix_pp_threshold`, `outlier_max`, `min_points_for_outliers`.
- Always respect global caps and emit `meta` with truncation and effective thresholds.

## Package & File Layout
- `internal/insights/`
  - `sequential_insights.go` — handler + schema types (generalized thought tracker) and session integration.
  - `session.go` — in‑memory bounded session store for thought history and branches.
  - `detect_tables.go` — block detection heuristics.
  - `profile_schema.go` — role inference + quality checks.
  - `primitives_change.go` — change/variance + drivers.
  - `primitives_composition.go` — composition/mix shift.
  - `primitives_concentration.go` — Top‑N + HHI.
  - `primitives_outliers.go` — modified z‑score.
  - `primitives_funnel.go` — funnel stage/cumulative conv + bottleneck.
  - `cards.go` — Insight Card formatting (finding, impact, evidence, assumptions, next action).
- `internal/registry/insights.go` — tool registration wiring for new tools.

## Testing Strategy
- Table‑Driven Unit Tests
  - Planner: thought loop state (counters, session continuity, branches) and presence of tiny planning card.
  - Table Detection: multiple block scenarios; header ambiguity questions.
  - Role Inference: synthetic columns (measure/dimension/time/id/target) with sampling; ambiguity prompts.
  - Primitives: correctness on small XLSX fixtures (Top‑N + Other, HHI bands, z‑score limits, ±5pp composition).
- Race/Concurrency
  - Verify streaming + locks and busy signals with `-race` on internal packages.

## Rollout Plan
- Phase 1: Planner + table detection + role inference + data quality (planning‑only default).
- Phase 2: Add change/variance + driver ranking + composition + concentration.
- Phase 3: Add robust outliers and funnel primitives.
- Phase 4: Polish Insight Cards and config docs; expand examples.

## References (please use mcp tools ref and octocode to study them)
- MCP Sequential Thinking Patterns
  - spences10/mcp‑sequentialthinking‑tools (tool recommendations, confidence, rationale, inputs):
    - https://github.com/spences10/mcp-sequentialthinking-tools
  - arben‑adm/mcp‑sequential‑thinking (models, storage, analysis scaffolding):
    - https://github.com/arben-adm/mcp-sequential-thinking
- MCP Server (typed tools, hooks)
  - mark3labs/mcp‑go: https://github.com/mark3labs/mcp-go
- Excel Streaming (iterators/StreamWriter)
  - Excelize docs: https://xuri.me/excelize/en/stream.html and https://github.com/xuri/excelize-doc/blob/master/en/sheet.md
- Concepts for Insight Primitives
  - HHI: https://en.wikipedia.org/wiki/Herfindahl%E2%80%93Hirschman_index
  - Modified z‑score: https://www.statology.org/modified-z-score/

## Acceptance Checklist
- [ ] sequential_insights generalized thought tracker schema and handler
- [ ] session store implemented (bounded history), no persistence
- [ ] detect_tables and profile_schema wired in registry with typed schemas
- [ ] primitives implemented with caps and metadata; planning‑only default for meta cards
- [ ] unit tests for thought tracking + branches, detection, inference, primitives; `make test` and `make test-race` pass
- [ ] documentation updated (design, steering, requirements) and examples added
