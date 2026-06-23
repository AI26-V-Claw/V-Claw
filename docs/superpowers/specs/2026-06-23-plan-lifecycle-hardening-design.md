# Plan Lifecycle Hardening Design

Date: 2026-06-23
Status: Approved
Scope: Task 1 N2 agent loop and planning hardening

## Goal

Harden the existing run-scoped planning behavior without adding a new planning subsystem. The changes should prevent stale plans, make plan ordering deterministic, retain useful plan state for continuations, and improve production debugging.

## Approach

Use minimal guardrails in the existing agent runtime and plan tool:

- Keep `PlanStore` in memory and scoped by `sessionID` plus `runID`.
- Keep `AgentResponse.Plan` unchanged.
- Avoid new storage, migrations, or lifecycle-manager abstractions.
- Prefer safe absence of a plan over resurrecting a stale plan.

## Lifecycle Contract

A plan belongs to one runtime run only.

1. The runtime creates a run and installs `WithPlanScope(ctx, sessionID, runID)`.
2. The `plan` tool reads and writes only the plan for that `sessionID/runID` pair.
3. `activePlanPrompt(sessionID, runID)` injects only the active plan for the current run.
4. `responsePlan(sessionID, runID)` returns only the current run plan.
5. Transcript hydration may restore a plan only when the tool payload `runId` matches the current run.
6. If no matching run-scoped plan exists, the runtime proceeds without an active plan.

## Clear And Retain Policy

Plan cleanup depends on run outcome:

- Clear on final outcomes that should not continue the same logical task: `completed`, `failed`, `cancelled`, `iteration_budget`, and panic/aborted failure.
- Retain on `blocked` when the run still has pending approval/action/clarification state that may resume.
- Log whether a plan was cleared or retained, including the reason.

## Parallel Tool Policy

The `plan` tool is housekeeping state and must not run in parallel with external tools.

- If `prepareParallelBatch` sees `PlanToolName`, it declines parallel execution.
- The existing serial loop handles the plan tool in deterministic order.
- The policy engine does not need a new capability or risk category for this change.

## Observability

Use the existing runtime logger. Add low-cardinality events for plan lifecycle operations:

- `plan hydrated`
- `plan prompt injected`
- `plan cleared`
- `plan retained`

Each event should include `session_id`, `run_id`, `step_count` when available, and a concise `reason`.

## Transcript Compaction Rule

Hydration only trusts a plan tool response whose `runId` matches the current run. If compaction removes that message, the runtime must not fall back to the latest session-level plan. Missing run-scoped plan state is safer than injecting stale plan state.

## Non-Goals

- Do not persist plan state to Postgres or session memory.
- Do not introduce a new `PlanLifecycle` manager.
- Do not change `AgentResponse.Plan` or UI display contracts.
- Do not refactor approval or clarification flows beyond plan clear/retain decisions.
