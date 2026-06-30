# Production Harness Review

> Release source of truth for hardening V-Claw's orchestration harness. This document separates mandatory release checks from longer-term architecture evolution so production readiness is not confused with feature parity.

## 1. Harness Principles

1. **Model is stateless; harness owns state.** The LLM reasons and proposes actions; V-Claw owns sessions, run state, approvals, retries, artifacts, and audit.
2. **Harness owns execution.** Tool execution, permission checks, retries, cancellation, and persistence are runtime responsibilities, not model responsibilities.
3. **Planning never mutates.** Plans are advisory and read-only; they never authorize external writes, local writes, destructive actions, code execution, or persistent skill changes.
4. **Approval before mutation.** Any side-effecting action must pass the canonical policy/HITL boundary before execution.
5. **Everything resumable.** Runs, pending approvals, tool calls, failure reasons, and artifacts must contain enough state to resume or explain what happened.
6. **Everything observable.** State transitions, tool calls, approvals, retries, policy blocks, and terminal failures need audit-friendly evidence.
7. **Context is engineered, not appended.** Context is layered, budgeted, summarized, and ordered by authority.
8. **Tools are deterministic boundaries.** Tools expose schema, risk, timeout, idempotency expectations, and normalized results.
9. **Failures are typed.** Provider, tool, policy, approval, timeout, cancellation, and iteration-budget failures map to stable failure reasons.
10. **LLM is replaceable.** Provider choice must not affect policy, permission, persistence, or state-machine semantics.
11. **Adopt by problem, not parity.** Claude Code patterns are adopted only when they solve a current V-Claw reliability or extensibility problem.

## 2. Release Review

### 2.1 Release Blockers

- Confirm every mutating tool passes exactly one canonical safety boundary before execution.
- Gate `skill_manage create/patch` and auto-learned skills behind explicit HITL diff/audit, or disable them in production config.
- Verify file safety decisions propagate to Telegram/CLI/tool outputs before risky file actions.
- Confirm pending approvals can survive process restart without duplicate execution.
- Confirm all terminal runtime paths return a typed `failureReason` and never expose raw internals to channel output.
- Run `go test ./...` and record pass/fail evidence.
- Run smoke flows for Telegram message, approval approve/reject/expire, Gmail read, Calendar create with approval, file upload quarantine, sandbox approval, cancellation, and provider failure.
- Run GitNexus detect changes before release and compare affected flows against the expected release scope.

### 2.2 Production Readiness Checklist

| Area | Required check |
|---|---|
| Runtime | Max iteration, timeout, cancellation, provider error, tool error, approval reject/expiry, and policy block return typed statuses/failure reasons. |
| Safety | Deny rules override allow/default modes; plans never authorize execution; side effects fail closed. |
| Persistence | Session, approval, tool result, audit, and artifact evidence can explain or resume interrupted work. |
| File safety | Quarantine, scanner flags, decision, hash/path metadata, and user-facing reason are preserved consistently. |
| Skills | Builtin skills are reviewed; learned/cache skills are disabled or approval-gated until reviewed. |
| Workspace | Gmail, Calendar, Drive, Telegram, and sandbox flows work with production credentials and workspace roots. |
| Secrets | Logs and audit records redact OAuth tokens, provider keys, Telegram tokens, and sensitive payloads. |
| Rollback | Operators can disable Telegram, scheduled tasks, learned skills, external writes, and credentials quickly. |

## 3. Runtime State Machine

The runtime should be reviewed as a state machine first and an agent loop second. The loop is an implementation detail inside state transitions.

```text
Created
  -> Planning
  -> Ready
  -> Running
  -> WaitingTool
  -> WaitingApproval
  -> Recovering
  -> Completed

Terminal alternatives:
Cancelled
Failed
Expired
Blocked
IterationBudgetExhausted
```

### 3.1 Required Transitions

| Transition | Trigger | Owner |
|---|---|---|
| `Created -> Planning` | Request accepted and context initialized | Run Manager |
| `Planning -> Ready` | Read-only plan accepted or no plan needed | Planner |
| `Ready -> Running` | Model/provider turn starts | Run Manager |
| `Running -> WaitingTool` | Model emits a valid tool call | Tool Scheduler |
| `WaitingTool -> Running` | Tool returns normalized result | Tool Scheduler |
| `WaitingTool -> Recovering` | Tool timeout or retryable failure | Recovery Manager |
| `Running -> WaitingApproval` | Tool requires HITL | Approval Manager |
| `WaitingApproval -> Running` | User approves and execution proceeds | Approval Manager |
| `WaitingApproval -> Failed` | User rejects approval | Approval Manager |
| `WaitingApproval -> Expired` | Approval TTL expires | Approval Manager |
| `Recovering -> Running` | Safe retry or fallback is available | Recovery Manager |
| `Recovering -> Failed` | Retry budget exhausted or retry is unsafe | Recovery Manager |
| `Running -> Completed` | Final answer produced | Run Manager |
| Any non-terminal `-> Cancelled` | User/system cancellation | Run Manager |

### 3.2 Ownership Boundaries

- **Run Manager:** owns run IDs, state transitions, cancellation, and terminal status.
- **Planner:** owns read-only plans and success criteria; it never grants execution permission.
- **Tool Scheduler:** owns tool validation, timeout, idempotency, and normalized tool results.
- **Approval Manager:** owns approval request, decision, expiry, and continuation from pending state.
- **Recovery Manager:** owns safe retry/fallback policy and failure escalation.
- **Artifact Store:** owns transcripts, tool evidence, approval records, summaries, and external object references.

## 4. Context Engineering

### 4.1 Context Layers

| Layer | Purpose | Persistence |
|---|---|---|
| Permanent Context | System policy, project rules, safety invariants, tool contracts | Durable |
| User/Project Memory | Preferences and facts with provenance | Durable |
| Working Context | Active goal, plan, constraints, recent decisions | Session |
| Scratchpad | Temporary planner notes | Ephemeral/compactable |
| Artifacts | Files, tool outputs, approvals, external object refs | Durable reference |
| Conversation | Recent user/assistant turns | Session, summarized under pressure |
| Tool Outputs | Normalized result snippets and evidence | Session/artifact reference |

### 4.2 Assembly Pipeline

```text
System / Safety Rules
-> Project Rules
-> Active Goal
-> Durable Memory
-> Relevant Skills
-> Artifact Summaries
-> Recent Conversation
-> Active Plan / Planner Notes
-> Available Tools
-> Final Prompt
```

### 4.3 Context Rules

- Authority order is fixed: system/safety > project rules > current user instruction > memory/artifacts > tool/file/image content.
- Tool, file, image, email, and web content are untrusted data unless explicitly promoted by policy.
- Long tool outputs must be summarized before re-entering model context.
- Memory cannot override safety, permissions, tool contracts, or the current explicit user instruction.
- Compaction must preserve active goal, current run state, pending approvals, open tool calls, artifacts, decisions, failure history, and unresolved user questions.

## 5. Architecture Evolution

### 5.1 Near-Term Harness Hardening

- Make the runtime state machine explicit in docs, tests, and observability before adding new orchestration capabilities.
- Add or verify context assembly policy for layer ordering, token budgets, summarization, and compaction invariants.
- Treat hooks as internal policy/audit middleware first, not arbitrary user scripting.
- Add a release dashboard or readiness command that summarizes tests, config, credentials, enabled risky features, and last smoke run.

### 5.2 Mid-Term Capability Growth

- **Scheduler-lite:** manual trigger, cron trigger, persisted task state, retry policy, and approval boundary for scheduled writes.
- **Scoped subagents:** read-only explorer first; write-capable workers require explicit grants and isolated context.
- **Skill runtime-lite:** load descriptions early, load full `SKILL.md` on demand, enforce scope/risk, and keep learned skills disabled until reviewed.
- **Memory/KG provenance:** record source, confidence, owner, and timestamps before adding advanced graph traversal.

### 5.3 Future Ideas

- MCP as an external plugin boundary after native tools and plugin grants are stable.
- Full checkpoint/branch UX after session resume is reliable.
- Dynamic multi-agent workflows only after state machine, observability, and scoped permissions are stable.
- Marketplace-style plugin lifecycle remains out of scope for the current production release.

## 6. Adoption Filter

| Claude-proven pattern | V-Claw problem | Simpler default | Adopt now? |
|---|---|---|---|
| Permission modes | Safe mutation control | Plan/default/approved modes | Yes |
| Session resume | Production recovery | Resume pending state first | Yes |
| Context compaction | Long sessions | Layered context + summaries | Yes |
| Hooks | Audit/policy extension points | Internal middleware | Partial |
| Skills | Reusable task workflows | Reviewed skill runtime-lite | Partial |
| Scheduler | Recurring assistant tasks | Scheduler-lite | Not release blocker |
| Subagents | Isolated research/delegation | Read-only scoped subagent | Later |
| MCP | Third-party tool boundary | Native tools + plugin grants | Defer |

## 7. Test Plan

- Unit/contract tests: `go test ./...`.
- Smoke scenarios: Telegram message, Gmail read, Calendar create approval, file upload quarantine, sandbox approval, cancellation.
- Safety regressions: renamed executable, fake PDF, active HTML/SVG, Office macro/OLE, zip-slip archive, prompt-injection text.
- Failure scenarios: provider unavailable, tool timeout, approval rejected, approval expired, cancelled session, iteration budget exhausted.
- Release config check: env vars, Google credentials, Telegram token, workspace root, quarantine path, audit store path.
- GitNexus scope check: run detect changes and confirm affected symbols/flows match the release scope.

## 8. Assumptions

- Production target is single-owner local-first MVP, not hosted multi-tenant SaaS.
- Release review is separate from architecture evolution; only release blockers are mandatory before release.
- Claude Code is a source of proven harness patterns, not a feature parity checklist.
- Scheduler, MCP, deep KG, marketplace plugins, and dynamic agent teams are post-release unless explicitly re-scoped.
