# Scenario 04: Sandbox Command With HITL

## Purpose

Luồng chuẩn cho code execution hoặc local file action qua sandbox. Đây là pattern bắt buộc approval trước khi chạy Python/Shell.

Scenario này đại diện cho:

- Sprint 2 G1: chạy Python/Shell trong sandbox.
- Sprint 2 G2: HITL trước command/code execution.
- Contract E2E "Shell Command Requires Approval".

## Sequence

```mermaid
sequenceDiagram
    autonumber

    actor User as Người dùng
    participant Channel as Message Channel
    participant Adapter as Channel Adapter
    participant Agent as Agent Core
    participant Safety as Safety Layer
    participant Approval as Approvals
    participant Router as Tool Router
    participant SandboxTool as Sandbox Tool
    participant Sandbox as Sandbox Runtime
    participant Store as Session / Audit Store

    User->>Channel: "Xóa các file tạm trong thư mục Downloads giúp tôi"
    Channel->>Adapter: Deliver message
    Adapter->>Agent: UserMessage

    Agent->>Agent: Extract intent + proposed command/script
    Agent->>Safety: Check sandbox.runShell / sandbox.runPython
    Safety-->>Agent: RiskDecision(code_execution/destructive, requires_approval)

    Agent->>Approval: Create ApprovalRequest(command preview)
    Approval->>Store: Save pending approval
    Approval-->>Adapter: AgentResponse(status=approval_required)
    Adapter-->>Channel: Send approval preview
    Channel-->>User: Command/script preview + Approve / Reject

    alt User rejects or approval expires
        User->>Channel: Reject / no response
        Channel->>Adapter: ApprovalDecision(rejected/expired)
        Adapter->>Approval: Resolve approval
        Approval->>Store: Save decision
        Approval-->>Agent: Not approved
        Agent-->>Adapter: AgentResponse(status=blocked)
        Adapter-->>Channel: Send cancellation
        Channel-->>User: Không chạy lệnh
    else User approves
        User->>Channel: Approve
        Channel->>Adapter: ApprovalDecision(approved)
        Adapter->>Approval: Resolve approval
        Approval->>Store: Save decision
        Approval-->>Agent: Approved

        Agent->>Router: ToolCall sandbox.runShell / sandbox.runPython
        Router->>SandboxTool: Execute approved tool call
        SandboxTool->>Sandbox: Run with workspace/resource limits

        alt Command blocked by sandbox policy
            Sandbox-->>SandboxTool: COMMAND_NOT_ALLOWED / FILE_ACCESS_DENIED
            SandboxTool-->>Router: ToolResult(success=false)
            Router-->>Agent: ToolResult error
            Agent->>Store: Save failed execution audit
            Agent-->>Adapter: AgentResponse(status=failed)
            Adapter-->>Channel: Send blocked result
            Channel-->>User: Lệnh bị chặn bởi policy
        else Timeout
            Sandbox-->>SandboxTool: SANDBOX_TIMEOUT
            SandboxTool-->>Router: ToolResult(success=false)
            Router-->>Agent: ToolResult error
            Agent->>Store: Save timeout audit
            Agent-->>Adapter: AgentResponse(status=failed)
            Adapter-->>Channel: Send timeout result
            Channel-->>User: Lệnh quá thời gian
        else Success
            Sandbox-->>SandboxTool: stdout/stderr + result metadata
            SandboxTool-->>Router: ToolResult(success=true)
            Router-->>Agent: ToolResult
            Agent->>Store: Save execution audit
            Agent-->>Adapter: AgentResponse(status=completed)
            Adapter-->>Channel: Send summary
            Channel-->>User: Kết quả đã thực thi
        end
    end
```

## Implementation Checklist

- Không chạy Python/Shell trước `ApprovalDecision=approved`.
- Approval preview phải hiển thị command/script, target path và tác động dự kiến.
- Sandbox vẫn phải enforce policy sau approval; approval không bỏ qua sandbox policy.
- Timeout/output limit phải được chuyển thành `SANDBOX_TIMEOUT` hoặc error tương ứng.
- Mọi kết quả thực thi phải được audit ở mức metadata/redacted phù hợp.
- Không mô tả `MEDIUM/HIGH`; dùng risk level contract như `code_execution` hoặc `destructive`.
