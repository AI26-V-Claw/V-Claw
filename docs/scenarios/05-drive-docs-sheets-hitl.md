# Drive / Docs / Sheets Read-Before-Write With HITL

Luồng chuẩn cho Google Workspace mở rộng: Drive/Docs/Sheets phải đọc trước, đề xuất rõ thay đổi, rồi mới ghi sau khi người dùng duyệt.

## Scope

- Sprint 2 N3: mở rộng Google Workspace connectors.
- Sprint 2 N4: side-effect Workspace actions phải đi qua HITL.
- Manual E2E: chạy qua Telegram demo channel.

## Representative Prompt

```text
Tìm tài liệu Sprint 2 trên Drive, đọc nội dung chính, rồi đề xuất cập nhật checklist vào Google Doc hoặc Sheet phù hợp sau khi tôi duyệt.
```

## Expected Contract Flow

```mermaid
sequenceDiagram
    participant User as Người dùng
    participant TG as Telegram
    participant Agent as Agent Runtime
    participant Drive as drive.*
    participant Docs as docs.*
    participant Sheets as sheets.*
    participant Policy as Tool Policy / HITL
    participant Store as Audit / State

    User->>TG: Prompt Workspace update
    TG->>Agent: UserMessage
    Agent->>Drive: drive.listFiles / drive.getFile / drive.exportFile
    Drive-->>Agent: File metadata/content summary

    alt Target is Google Doc
        Agent->>Docs: docs.getDocument
        Docs-->>Agent: Document content/metadata
        Agent->>Policy: Propose docs.appendText / appendMarkdown / replaceText / insertText / deleteContent
    else Target is Google Sheet
        Agent->>Sheets: sheets.getSpreadsheet / sheets.readValues
        Sheets-->>Agent: Sheet metadata/values
        Agent->>Policy: Propose sheets.appendValues / updateValues / clearValues / tab action
    else Target is Drive file operation
        Agent->>Policy: Propose drive.createFile / uploadFile / shareFile / moveFile / trashFile
    end

    Policy-->>Agent: RiskDecision(requires_approval)
    Agent->>Store: Save approval/audit metadata
    Agent-->>TG: AgentResponse(approval_required)
    TG-->>User: Show approval proposal

    alt User approves
        User->>TG: approve
        TG->>Agent: ApprovalDecision(approved)
        Agent->>Docs: Execute approved write if Docs target
        Agent->>Sheets: Execute approved write if Sheets target
        Agent->>Drive: Execute approved write if Drive target
        Agent->>Store: Save execution result/audit
        Agent-->>TG: AgentResponse(completed)
        TG-->>User: Final result with artifact/source reference
    else User rejects or approval expires
        User->>TG: reject / timeout
        TG->>Agent: ApprovalDecision(rejected/expired)
        Agent->>Store: Save cancellation/audit
        Agent-->>TG: AgentResponse(blocked/failed/cancelled)
        TG-->>User: No write executed
    else User revises
        User->>TG: revise <requested change>
        TG->>Agent: ApprovalDecision(revised)
        Agent->>Store: Mark original approval revised
        Agent-->>TG: New or clarified proposal
    end
```

## Required Tool Behavior

| Tool Group | Safe Reads | Writes Requiring HITL |
|---|---|---|
| Drive | `drive.listFiles`, `drive.getFile`, `drive.exportFile`, `drive.downloadFile`, `drive.listPermissions` | `drive.createFolder`, `drive.createFile`, `drive.uploadFile`, `drive.updateFileMetadata`, `drive.shareFile`, `drive.revokePermission`, `drive.moveFile`, `drive.moveFiles`, `drive.trashFile`, `drive.untrashFile` |
| Docs | `docs.getDocument` | `docs.createDocument`, `docs.appendText`, `docs.appendMarkdown`, `docs.replaceText`, `docs.insertText`, `docs.deleteContent` |
| Sheets | `sheets.getSpreadsheet`, `sheets.readValues`, `sheets.batchGetValues` | `sheets.createSpreadsheet`, `sheets.updateValues`, `sheets.batchUpdateValues`, `sheets.appendValues`, `sheets.clearValues`, `sheets.addSheet`, `sheets.renameSheet`, `sheets.deleteSheet`, `sheets.duplicateSheet` |

## HITL Proposal Must Include

- Target artifact name and ID if available.
- Exact operation/tool name.
- Summary of content to create/update/append/delete.
- Risk level and reason.
- Expected result or artifact reference.

## Must Not Happen

```text
- Drive/Docs/Sheets write executes before approval.
- Agent claims a document/sheet/file was updated without a successful tool result.
- drive.uploadFile reads local host secrets such as configs/google/token.json or .env.
- drive.shareFile grants public writer/commenter access to anyone links.
- sheets.clearValues or sheets.deleteSheet runs without explicit approval.
- docs.deleteContent runs without explicit approval.
```

## E2E Test References

- `docs/testing-e2e/04_DEMO_STORIES.md`: DS-002 Workspace Assistant Flow.
- `docs/testing-e2e/05_DEMO_TOP_10_TEST_CASES.md`: DTC-07 Drive Docs Sheets Update.
- `docs/testing-e2e/06_FULL_E2E_TEST_CASES.md`: FE-021 through FE-025.
