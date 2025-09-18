# Task 10 Implementation Plan — Sequential Insights (Domain‑Neutral)

## Objective
Design and implement a domain‑neutral Sequential Insights capability for MCPXcel that helps an MCP client (LLM) reason like an analyst using deterministic, bounded primitives. The server provides planning guidance, clarifying questions, recommended tool calls with confidence/rationale, and concise Insight Cards — without embedding any LLM logic on the server.

## Success Criteria
- Expose `sequential_insights` planning tool (path‑only API, cursor precedence) returning:
  - `current_step`, `recommended_tools`, `questions`, `insight_cards`, `meta`.
- Implement bounded primitives (config‑gated): change/variance, driver ranking, composition/mix shift, concentration (Top‑N share, HHI), robust outliers (modified z‑score), funnel analysis.
- Add multiple‑table detection per sheet, role inference and data quality checks.
- Maintain caps (≤10k cells, ≤128KB payload) with streaming iterators and clear truncation/assumption metadata.

## Non‑Goals
- No server‑embedded LLM (narrative/strategy remains client‑side).
- No broad forecasting/seasonality modeling beyond simple comparisons.
- No stateful session memory beyond normal request context.

## API Additions (Tool Sketches)
- `sequential_insights` (planning)
  - Input: `path|cursor`, optional `sheet|range`, `objective` (string), `available_tools` (string[]), `hints` (role/time/target/stage), `constraints` (caps), `step_number`, `total_steps`, `next_step_needed`, optional revision/branch fields.
  - Output: `current_step`, `recommended_tools[{ tool_name, confidence, rationale, priority, suggested_inputs, alternatives }]`, `questions[]`, `insight_cards[]`, `meta` (caps, truncation, cursor semantics).
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

## Planning Flow
1) Table Detection → choose range (ask if multiple candidates).
2) Schema Profiling → role/time inference + quality gate; ask clarifiers if ambiguous.
3) Pattern Selection → map `objective` to general patterns (change, variance to baseline/target, composition, drivers, concentration, funnel).
4) Optional Bounded Compute → run streaming primitives under caps; produce Insight Cards with assumptions and evidence snippets.
5) Recommend Next Steps → precise tool calls with parameters, confidence, rationale; repeat until `next_step_needed=false`.

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
- Feature flag: enable bounded compute; default planning‑only.
- Thresholds: `max_groups`, `top_n`, `mix_pp_threshold`, `outlier_max`, `min_points_for_outliers`.
- Always respect global caps and emit `meta` with truncation and effective thresholds.

## Package & File Layout
- `internal/insights/`
  - `sequential_insights.go` — handler + schema types, planner orchestration.
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
  - Planner: objective → recommended tools (+ params, confidence order).
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

## References
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
- [ ] sequential_insights tool schema and handler scaffolding
- [ ] detect_tables and profile_schema wired in registry with typed schemas
- [ ] primitives implemented with caps and metadata; planning‑only default behind config flag
- [ ] unit tests for planner, detection, inference, primitives; `make test` and `make test-race` pass
- [ ] documentation updated (design, steering, requirements) and examples added

