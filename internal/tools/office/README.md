# office

Google Workspace-facing agent tools.

These tools should call `internal/connectors/google` rather than Google APIs directly.

## Risk Matrix

Current Google Workspace tools are grouped in the registry like this:

| Risk level | Approval | Tools |
| --- | --- | --- |
| `safe_read` | Auto-allow by default | `gmail.listEmails`, `gmail.listLabels`, `gmail.getProfile`, `gmail.listThreads`, `gmail.getThread`, `gmail.listDrafts`, `gmail.getDraft`, `calendar.listEvents`, `drive.listFiles`, `drive.getFile`, `drive.exportFile`, `drive.downloadFile`, `drive.listPermissions`, `sheets.getSpreadsheet`, `chat.listSpaces`, `chat.listMembers`, `chat.findSpacesByMembers`, `people.searchDirectory` |
| `sensitive_read` | Requires approval | `gmail.getEmail`, `docs.getDocument`, `sheets.readValues`, `sheets.batchGetValues`, `chat.listMessages` |
| `external_write` | Requires approval | `gmail.createDraft`, `gmail.updateDraft`, `gmail.sendDraft`, `gmail.replyDraft`, `gmail.forwardDraft`, `gmail.modifyMessage`, `gmail.batchModifyMessages`, `gmail.untrashMessage`, `calendar.createEvent`, `calendar.updateEvent`, `drive.createFolder`, `drive.createFile`, `drive.uploadFile`, `drive.updateFileMetadata`, `drive.shareFile`, `drive.revokePermission`, `drive.moveFile`, `drive.moveFiles`, `drive.untrashFile`, `docs.createDocument`, `docs.appendText`, `docs.replaceText`, `docs.insertText`, `sheets.createSpreadsheet`, `sheets.updateValues`, `sheets.batchUpdateValues`, `sheets.appendValues`, `sheets.clearValues`, `sheets.addSheet`, `sheets.renameSheet`, `sheets.duplicateSheet`, `chat.sendMessage`, `chat.updateMessage`, `chat.createSpace`, `chat.addMember` |
| `local_write` | Requires approval | `gmail.downloadAttachments` |
| `destructive` | Always blocked by default policy group | `gmail.deleteDraft`, `gmail.trashMessage`, `calendar.deleteEvent`, `drive.trashFile`, `docs.deleteContent`, `sheets.deleteSheet`, `chat.deleteMessage`, `chat.removeMember` |

Notes:

- User policy currently exposes `auto_allow`, `require_approval`, and `always_block` groups across the same risk levels.
- `safe_read` and `safe_compute` are the only levels eligible for auto-allow in the current settings UI.
- `safe_compute` exists in the policy model and builtin tools, but there is no Google Workspace tool in that bucket today.
- `sensitive_read` is for full-content reads that may expose private or user-generated data, even when they do not mutate anything.
