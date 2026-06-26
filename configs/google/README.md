# Google Workspace OAuth

V-Claw uses Google OAuth user authentication. The CLI runs locally, opens a browser consent flow, stores a local token, then calls Gmail, Calendar, Google Chat, Google People, Drive, Docs, and Sheets APIs on behalf of the signed-in Workspace user.

This document only covers Google Cloud setup, Google OAuth, and Google API smoke tests. Channel bot setup lives in:

```text
internal/channels/README.md
```

## Required Google Cloud Setup

1. Create or select a Google Cloud project.
2. Enable the required APIs:

```text
Gmail API
Google Calendar API
Google Meet API
Google Chat API
Google People API
Google Drive API
Google Docs API
Google Sheets API
```

3. Configure the OAuth consent screen for your Workspace.
4. Create an OAuth client with application type `Desktop app`.
5. Download the OAuth client JSON.
6. Save it locally as:

```text
configs/google/credentials.json
```

Do not commit `credentials.json`, `oauth-client.internal.json`, `token.json`, or other generated token files.

## Google Chat API Configuration

If you use Google Chat API features, open the Google Chat API configuration tab in Google Cloud Console and fill in:

```text
App name
Avatar URL
Description
```

For the current user-OAuth flow, interactive features can stay off.

If a Chat command returns `Google Chat app not found`, save the Google Chat API configuration, wait a few minutes, then try again.

## Workspace Directory Visibility

If `people.searchDirectory` returns no users even though the domain has Workspace accounts, check Google Admin:

```text
Directory -> Directory settings -> Contact sharing
```

Enable contact sharing and expose at least the primary domain profile name/email inside the organization. This is only for resolving Workspace names and emails; it does not share contacts outside the organization.

## Shared Drive Secret Flow

Recommended team flow:

1. Admin creates a restricted shared drive:

```text
V-Claw Secrets
```

2. Admin uploads:

```text
credentials.json
```

3. Share the drive only with trusted Workspace members or a Workspace group.
4. Each developer downloads `credentials.json` locally into:

```text
configs/google/credentials.json
```

## Environment

V-Claw automatically loads local environment variables from `.env` when the CLI starts. Existing shell environment variables take precedence over `.env`.

Recommended `.env` values for Google:

```env
VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json
VCLAW_GOOGLE_WORKSPACE_DOMAINS=vclaw.site
VCLAW_GOOGLE_TOOLS_MODE=auto
```

Google tools mode:

```text
auto     register Google tools only when credentials/token files exist
required fail startup if Google OAuth is not ready
off      expose built-in and sandbox approval tools only
```

## Run OAuth

Run:

```powershell
go run ./cmd/vclaw google auth
```

The auth command creates:

```text
configs/google/token.json
```

This token is personal, local-only, and ignored by git.

During auth, V-Claw starts a temporary local callback server on `127.0.0.1`, so the browser should end on a small success page. If the callback cannot start, the CLI falls back to asking you to paste the redirected URL or authorization code.

Additional command-specific help is available under:

```powershell
go run ./cmd/vclaw google people help
go run ./cmd/vclaw google chat help
go run ./cmd/vclaw google gmail help
```

Mutating Chat commands are available for manual CLI testing. Agent-triggered Chat write/destructive tools such as `chat.sendMessage`, `chat.updateMessage`, `chat.deleteMessage`, `chat.createSpace`, `chat.addMember`, and `chat.removeMember` must pass the approval boundary before execution.
Mutating Gmail commands are also for manual CLI testing. Agent-triggered Gmail draft, send, attachment download, modify, batch modify, delete draft, trash, and untrash tools must pass the approval boundary before execution.

## Re-Auth When Scopes Change

If OAuth scopes change, delete the old token and auth again:

```powershell
Remove-Item configs/google/token.json
go run ./cmd/vclaw google auth
```

## Smoke Tests

Run a basic Google smoke test:

```powershell
go run ./cmd/vclaw google smoke
```

Optional Google API checks:

```powershell
go run ./cmd/vclaw google people search-directory -query "Bao"
go run ./cmd/vclaw google people search-directory -query "tung@yourdomain.com" -max-results 5
go run ./cmd/vclaw google chat list-spaces
go run ./cmd/vclaw google chat list-members -space spaces/AAAA...
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA...
go run ./cmd/vclaw google gmail list -max-results 10
go run ./cmd/vclaw google gmail list-threads -max-results 10
go run ./cmd/vclaw google drive list -max-results 10
go run ./cmd/vclaw google docs get -id DOCUMENT_ID
go run ./cmd/vclaw google sheets get -id SPREADSHEET_ID
```

Use the `Candidate Chat users` values from the output to compare with `google chat list-members`. You can also pass a candidate `users/...` value directly to `google chat find-spaces-by-members` before listing messages.

## Gmail manual test commands

List labels and show the signed-in account profile:

```powershell
go run ./cmd/vclaw google gmail labels
go run ./cmd/vclaw google gmail profile
```

List messages and threads:

```powershell
go run ./cmd/vclaw google gmail list -query "is:unread" -max-results 10
go run ./cmd/vclaw google gmail list-threads -query "from:alice@example.com" -max-results 10
```

Read one message or thread:

```powershell
go run ./cmd/vclaw google gmail get -id MESSAGE_ID -full
go run ./cmd/vclaw google gmail get-thread -id THREAD_ID -full
```

List, read, create, update, send, and delete drafts:

```powershell
go run ./cmd/vclaw google gmail list-drafts
go run ./cmd/vclaw google gmail get-draft -id DRAFT_ID -full
go run ./cmd/vclaw google gmail create-draft -to alice@example.com -subject "Hello" -text "Draft body" -attachments "C:\tmp\report.pdf"
go run ./cmd/vclaw google gmail update-draft -id DRAFT_ID -to alice@example.com -subject "Hello" -text "Updated body" -attachments "C:\tmp\report.pdf,C:\tmp\notes.txt"
go run ./cmd/vclaw google gmail send-draft -id DRAFT_ID
go run ./cmd/vclaw google gmail delete-draft -id DRAFT_ID
```

Create reply or forward drafts:

```powershell
go run ./cmd/vclaw google gmail reply-draft -id MESSAGE_ID -to alice@example.com -text "Reply body" -attachments "C:\tmp\reply-context.pdf"
go run ./cmd/vclaw google gmail forward-draft -id MESSAGE_ID -to alice@example.com -text "Forward note" -attachments "C:\tmp\extra.pdf"
```

Download attachments, modify labels, and move messages to or from trash:

```powershell
go run ./cmd/vclaw google gmail download-attachments -id MESSAGE_ID -output-dir C:\tmp\vclaw-gmail
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action markRead
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action archive
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action addLabels -labels LABEL_ID
go run ./cmd/vclaw google gmail batch-modify -ids MESSAGE_ID_1,MESSAGE_ID_2 -action markRead
go run ./cmd/vclaw google gmail trash-message -id MESSAGE_ID
go run ./cmd/vclaw google gmail untrash-message -id MESSAGE_ID
```

Draft attachments are local file paths. V-Claw supports up to 10 files per draft operation and up to 20 MiB total raw attachment data in the current MVP.

## Google Chat manual test commands

List spaces that the signed-in user can access:

```powershell
go run ./cmd/vclaw google chat list-spaces
go run ./cmd/vclaw google chat list-members -space spaces/AAAA...
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA...
```

Command-specific help:

```powershell
go run ./cmd/vclaw google people help
go run ./cmd/vclaw google chat help
go run ./cmd/vclaw google gmail help
```

## Drive / Docs / Sheets manual test commands

Read-first checks:

```powershell
go run ./cmd/vclaw google drive list -query "trashed = false" -max-results 10
go run ./cmd/vclaw google drive get -id FILE_ID
go run ./cmd/vclaw google drive export -id GOOGLE_DOC_FILE_ID -mime-type text/plain
go run ./cmd/vclaw google drive permissions -id FILE_ID
go run ./cmd/vclaw google docs get -id DOCUMENT_ID
go run ./cmd/vclaw google sheets get -id SPREADSHEET_ID
go run ./cmd/vclaw google sheets read -id SPREADSHEET_ID -range "Sheet1!A1:D10"
```

Manual write checks, for developer testing only:

```powershell
go run ./cmd/vclaw google drive create-folder -name "V-Claw Smoke"
go run ./cmd/vclaw google drive create-file -name "vclaw-smoke.txt" -content "hello"
go run ./cmd/vclaw google drive move-files -ids "FILE_ID_1,FILE_ID_2" -target-parent FOLDER_ID
go run ./cmd/vclaw google docs create -title "V-Claw Smoke Doc"
go run ./cmd/vclaw google docs append -id DOCUMENT_ID -text "Smoke text"
go run ./cmd/vclaw google docs replace -id DOCUMENT_ID -old "Smoke" -new "Verified"
go run ./cmd/vclaw google sheets create -title "V-Claw Smoke Sheet" -sheets "Data"
go run ./cmd/vclaw google sheets update -id SPREADSHEET_ID -range "Data!A1:B1" -values '[[\"Name\",\"Value\"]]'
go run ./cmd/vclaw google sheets append -id SPREADSHEET_ID -range "Data!A:B" -values '[[\"Smoke\",\"OK\"]]'
go run ./cmd/vclaw google sheets clear -id SPREADSHEET_ID -range "Data!A1:B2"
```

Agent-triggered Drive/Docs/Sheets mutating tools must pass HITL approval before execution. CLI write commands are direct developer smoke checks and do not represent the agent safety boundary.

## OAuth Scopes

Current G1 scopes:

```text
https://www.googleapis.com/auth/gmail.readonly
https://www.googleapis.com/auth/gmail.compose
https://www.googleapis.com/auth/gmail.send
https://www.googleapis.com/auth/gmail.modify
https://www.googleapis.com/auth/calendar.readonly
https://www.googleapis.com/auth/calendar.events
https://www.googleapis.com/auth/meetings.space.created
https://www.googleapis.com/auth/chat.spaces.readonly
https://www.googleapis.com/auth/chat.messages.create
https://www.googleapis.com/auth/chat.messages.readonly
https://www.googleapis.com/auth/chat.messages
https://www.googleapis.com/auth/chat.memberships
https://www.googleapis.com/auth/chat.spaces
https://www.googleapis.com/auth/directory.readonly
https://www.googleapis.com/auth/drive.readonly
https://www.googleapis.com/auth/drive
https://www.googleapis.com/auth/documents.readonly
https://www.googleapis.com/auth/documents
https://www.googleapis.com/auth/spreadsheets.readonly
https://www.googleapis.com/auth/spreadsheets
```

Scope usage:

- `gmail.readonly`: message/thread/draft reads, labels, profile, and attachment metadata.
- `gmail.compose`, `gmail.send`, `gmail.modify`: draft creation/update/send/delete, local file attachments in drafts, attachment download, message label changes, batch modify, trash, and untrash.
- `calendar.readonly`: listing Calendar events.
- `calendar.events`: creating, updating, and deleting Calendar events after HITL approval.
- `meetings.space.created`: creating standalone Google Meet meeting spaces after HITL approval.
- Chat scopes: listing spaces/messages, sending text replies/attachments, updating/deleting messages, creating spaces, and adding/removing members.
- `directory.readonly`: searching Workspace directory profiles so the agent can resolve names or emails before matching Google Chat members.
- `drive.readonly`: listing/searching Drive files, reading Drive file metadata, listing permissions, exporting Google Workspace files, and downloading capped file content.
- `drive`: creating Drive folders/files, uploading local files, updating metadata, sharing files, revoking permissions, moving files/folders, trashing, and untrashing after HITL approval.
- `documents.readonly`: reading Google Docs document structure/text with preview or full-content modes.
- `documents`: creating documents, appending text, replacing text, inserting text, and deleting content ranges after HITL approval.
- `spreadsheets.readonly`: reading spreadsheet metadata, one value range, or multiple value ranges.
- `spreadsheets`: creating spreadsheets, updating/batch-updating/appending/clearing values, adding/renaming/deleting/duplicating sheets after HITL approval.

## Safety Notes

Manual mutating Google CLI commands are only for developer testing. Agent-triggered mutating actions must pass the approval boundary before execution.

Examples of mutating actions:

```text
Gmail draft/send/modify/download attachment
Google Chat send/update/delete/create space/add member/remove member
Calendar create/update/delete
Google Meet link creation
Drive create folder/update metadata/share/move/trash/untrash
Docs create/append text
Sheets create/update/append values
```

Card messages are not supported by the current Google Chat user OAuth flow. Use normal text messages for Google Chat manual tests. If the project needs rich cards later, add a Google Chat app authentication flow and update contracts, docs, and tests before exposing it to agents.
