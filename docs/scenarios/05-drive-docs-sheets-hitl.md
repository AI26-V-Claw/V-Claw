# Scenario 05: Drive / Docs / Sheets Read-Before-Write + HITL

## Goal

The agent can inspect Drive, Docs, and Sheets data first, then propose a precise mutating action for human approval before changing external Google Workspace state.

## Flow

1. User asks to move/share/update a Drive file, append or replace text in a Doc, or update/clear values in a Sheet.
2. Agent uses read-only tools first when resource IDs, titles, ranges, or current content are ambiguous:
   - `drive.listFiles`, `drive.getFile`, `drive.listPermissions`
   - `docs.getDocument`
   - `sheets.getSpreadsheet`, `sheets.readValues`, `sheets.batchGetValues`
3. Agent prepares the mutating tool call with concrete IDs/ranges/content.
4. Runtime policy returns `requires_approval` for external writes/destructive actions.
5. Channel renders a HITL prompt with service-specific summary and relevant fields.
6. Tool executes only after approval. Reject/revise does not execute the pending tool.

## Examples

Drive:

```text
User: Chia sẻ file "Q3 Plan" cho alice@example.com quyền commenter.
Agent: drive.listFiles -> choose file ID -> propose drive.shareFile -> wait for approval.
```

Docs:

```text
User: Thay "Draft" bằng "Final" trong document này.
Agent: docs.getDocument -> propose docs.replaceText -> wait for approval.
```

Sheets:

```text
User: Xóa dữ liệu Sheet1!A2:D20.
Agent: sheets.readValues -> propose sheets.clearValues -> wait for approval.
```

## Safety Notes

- Read tools do not require approval but should bound returned content.
- `drive.trashFile` and `sheets.deleteSheet` are destructive.
- Broad sharing such as Drive `type=anyone` or `role=writer` must be visible in the approval preview.
