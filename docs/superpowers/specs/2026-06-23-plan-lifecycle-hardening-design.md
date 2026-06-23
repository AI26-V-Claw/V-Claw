# Plan Lifecycle Hardening Design

Date: 2026-06-23
Status: Approved after review revisions
Scope: Task 1 N2 agent loop and planning hardening

## Goal

Harden the existing run-scoped planning behavior without adding a new planning subsystem. The changes should prevent stale plans, make plan ordering deterministic, retain best-effort same-process plan state for continuations, and improve production debugging.

## Approach

Use minimal guardrails in the existing agent runtime and plan tool:

- Keep `PlanStore` in memory and scoped by `sessionID` plus `runID`.
- Keep `AgentResponse.Plan` unchanged.
- Avoid new storage, migrations, or lifecycle-manager abstractions.
- Prefer safe absence of a plan over resurrecting a stale plan.
- Treat retained plans as best-effort same-process state only. After restart, deploy, crash, or routing to another runtime instance, the system safely resumes without a plan.

## Lifecycle Contract

A plan belongs to one runtime run only.

1. The runtime creates a run and installs `WithPlanScope(ctx, sessionID, runID)`.
2. The `plan` tool reads and writes only the plan for that `sessionID/runID` pair.
3. `activePlanPrompt(sessionID, runID)` injects only the active plan for the current run.
4. `responsePlan(sessionID, runID)` returns only the current run plan.
5. Transcript hydration may restore a plan only when the tool payload `runId` matches the current run.
6. If no matching run-scoped plan exists, the runtime proceeds without an active plan.

Blocked continuations reuse the original run ID while pending continuation state remains valid. Approval resume already resumes `pending.runID`; clarification answers may start a transient new run, then resume `pendingClarification.RunID`. Any new independent user request receives its own run ID and must not inherit the prior plan through session fallback.

`PlanStore` operations are synchronized. Reads return copied snapshots, and writes are atomic per `(sessionID, runID)` key. Multiple plan writes for one run use last-write-wins semantics. A per-run revision counter may be kept for logs/debugging but is not exposed in the UI contract.

## Clear And Retain Policy

Plan cleanup depends on run outcome:

- Clear on final outcomes that should not continue the same logical task: `completed`, `failed`, `cancelled`, and panic/aborted failure.
- Clear `iteration_budget` only when it produces a terminal response with no pending continuation state.
- Retain `blocked` and any other resumable state only when the run still has pending approval/action/clarification state that may resume.
- Do not decide cleanup from the status name alone. Use a helper that classifies the run as resumable or non-resumable from status plus pending action/clarification fields.
- Retained blocked plans must be bounded. Use a small TTL cleanup path in `PlanStore` or cleanup-on-read/write so abandoned blocked plans do not accumulate indefinitely.
- Log whether a plan was cleared or retained, including the reason.

## Parallel Tool Policy

The `plan` tool is housekeeping state and must not run in parallel with external tools.

- If `prepareParallelBatch` sees `PlanToolName`, it declines parallel execution.
- The existing serial loop handles all tool calls in provider-emitted index order.
- If a provider emits `[external_tool_A, plan, external_tool_B]`, the runtime executes exactly that order serially.
- Multiple plan updates in one serial pass use last-write-wins semantics, with revision/debug logging if available.
- The policy engine does not need a new capability or risk category for this change.

## Observability

Use the existing runtime logger. Add low-cardinality events for plan lifecycle operations:

- `plan hydrated`
- `plan prompt injected`
- `plan cleared`
- `plan retained`

Each event should include `session_id`, `run_id`, `step_count` when available, a concise `reason`, and `revision` when available.

## Transcript Compaction Rule

Hydration only trusts a server-generated plan-tool result envelope whose tool identity, schema, and runtime-assigned `runId` match the current run. It must not infer plan scope from assistant text or model-supplied arguments.

Malformed JSON, blank `runId`, invalid schema, over-large plan content, or excessive step counts are ignored safely. Duplicate historical plan results are resolved by taking the last valid result for the current run, or the highest revision if revisions are present. If compaction removes the matching plan message, the runtime must not fall back to the latest session-level plan. Missing run-scoped plan state is safer than injecting stale plan state.

## Acceptance Criteria

1. Two different runs in one `sessionID` cannot see each other's plans.
2. Approval continuation resumes the original run and can see its retained plan while same-process pending state is valid.
3. Clarification continuation resumes `pendingClarification.RunID`; unrelated new requests do not inherit prior plans.
4. Transcript hydration with old and current run plan messages hydrates only the current run.
5. Missing compacted plan messages do not trigger session-level fallback.
6. Non-resumable outcomes clear the plan once; resumable `blocked` retains it until resume or expiry.
7. Retained blocked plans expire or are pruned so the store remains bounded.
8. A tool batch containing `plan` executes serially in provider-emitted order.
9. Concurrent reads/writes remain race-safe because store operations are synchronized and return copies.
10. Plan lifecycle logs include `session_id`, `run_id`, `reason`, `step_count`, and `revision` when available.

## Non-Goals

- Do not persist plan state to Postgres or session memory.
- Do not introduce a new `PlanLifecycle` manager.
- Do not change `AgentResponse.Plan` or UI display contracts.
- Do not refactor approval or clarification flows beyond plan clear/retain decisions.
