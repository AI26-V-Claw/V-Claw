# Google Workspace OAuth

G1 uses OAuth user authentication. V-Claw runs locally, opens a browser consent flow, then calls Gmail, Calendar, and Google Chat on behalf of the signed-in Workspace user.

## Required Google Cloud setup

1. Enable Gmail API, Google Calendar API, and Google Chat API.
2. Configure the Google Chat API with an app name, avatar URL, and description. Turn interactive features off for G1.
3. Configure the OAuth consent screen for your Workspace.
4. Create an OAuth client with application type `Desktop app`.
5. Download the client JSON and store it in a restricted Google Drive shared drive.

The OAuth Desktop app config is not committed to git. Developers download it locally into `configs/google/credentials.json`.

Do not commit `credentials.json`, `oauth-client.internal.json`, or generated token files.

## Google Drive flow

Admin creates a restricted shared drive:

```text
V-Claw Secrets
```

Upload:

```text
credentials.json
```

Share the drive only with trusted Workspace members or a Workspace group.

Workspace members download `credentials.json` from Google Drive and save it to:

```text
configs/google/credentials.json
```

V-Claw automatically loads local environment variables from the repository `.env` file when the CLI starts. Existing shell environment variables take precedence over values in `.env`.

Then sign in and smoke test:

```powershell
go run ./cmd/vclaw google auth
go run ./cmd/vclaw google smoke
go run ./cmd/vclaw google smoke -chat-space spaces/AAAA...
go run ./cmd/vclaw google chat list-spaces
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA...
```

Additional Chat test commands are available under:

```powershell
go run ./cmd/vclaw google chat help
```

Additional Gmail test commands are available under:

```powershell
go run ./cmd/vclaw google gmail help
```

Mutating Chat commands are for manual CLI testing. Agent-triggered `chat.sendMessage` remains an `external_write` tool and must pass the approval boundary before execution.
Mutating Gmail commands are also for manual CLI testing. Agent-triggered Gmail draft, send, attachment download, and modify tools must pass the approval boundary before execution.

## Gmail manual test commands

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

Create, update, and send drafts:

```powershell
go run ./cmd/vclaw google gmail create-draft -to alice@example.com -subject "Hello" -text "Draft body"
go run ./cmd/vclaw google gmail update-draft -id DRAFT_ID -to alice@example.com -subject "Hello" -text "Updated body"
go run ./cmd/vclaw google gmail send-draft -id DRAFT_ID
```

Create reply or forward drafts:

```powershell
go run ./cmd/vclaw google gmail reply-draft -id MESSAGE_ID -to alice@example.com -text "Reply body"
go run ./cmd/vclaw google gmail forward-draft -id MESSAGE_ID -to alice@example.com -text "Forward note"
```

Download attachments and modify message labels:

```powershell
go run ./cmd/vclaw google gmail download-attachments -id MESSAGE_ID -output-dir C:\tmp\vclaw-gmail
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action markRead
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action archive
go run ./cmd/vclaw google gmail modify-message -id MESSAGE_ID -action addLabels -labels LABEL_ID
```

## Google Chat manual test commands

List spaces that the signed-in user can access:

```powershell
go run ./cmd/vclaw google chat list-spaces
go run ./cmd/vclaw google chat list-spaces -page-size 20
```

List recent messages in a space:

```powershell
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA...
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA... -max-results 20
go run ./cmd/vclaw google chat list-messages -space spaces/AAAA... -show-deleted
```

Send a text message:

```powershell
go run ./cmd/vclaw google chat send -space spaces/AAAA... -text "Hello from V-Claw"
```

Send a reply in a thread:

```powershell
go run ./cmd/vclaw google chat send -space spaces/AAAA... -text "Reply from V-Claw" -thread spaces/AAAA.../threads/BBBB...
go run ./cmd/vclaw google chat send -space spaces/AAAA... -text "Reply with thread key" -thread-key vclaw-test-thread -reply-option REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD
```

Send a file attachment:

```powershell
go run ./cmd/vclaw google chat send -space spaces/AAAA... -text "Attached file from V-Claw" -attachment "D:\path\to\file.pdf"
go run ./cmd/vclaw google chat send -space spaces/AAAA... -text "Attached image from V-Claw" -attachment "D:\path\to\image.png"
```

Update or delete a message created by the signed-in user:

```powershell
go run ./cmd/vclaw google chat update-message -name spaces/AAAA.../messages/BBBB... -text "Updated text from V-Claw"
go run ./cmd/vclaw google chat delete-message -name spaces/AAAA.../messages/BBBB...
```

Create a group chat or named space with Workspace members:

```powershell
go run ./cmd/vclaw google chat create-space -type GROUP_CHAT -members alice@yourdomain.com,bob@yourdomain.com
go run ./cmd/vclaw google chat create-space -type SPACE -name "V-Claw Test Space" -members alice@yourdomain.com,bob@yourdomain.com
```

Add or remove members:

```powershell
go run ./cmd/vclaw google chat add-member -space spaces/AAAA... -user alice@yourdomain.com
go run ./cmd/vclaw google chat remove-member -name spaces/AAAA.../members/BBBB...
```

Card messages are intentionally blocked in the CLI because Google Chat does not support cards with the current user OAuth flow:

```powershell
go run ./cmd/vclaw google chat send-card -space spaces/AAAA... -title "Title" -text "Body"
```

Chat member invitations are restricted to configured Workspace domains. Set the allowed domains before creating spaces with members or adding members:

```powershell
$env:VCLAW_GOOGLE_WORKSPACE_DOMAINS="yourdomain.com"
go run ./cmd/vclaw google chat create-space -type GROUP_CHAT -members alice@yourdomain.com,bob@yourdomain.com
go run ./cmd/vclaw google chat add-member -space spaces/AAAA... -user alice@yourdomain.com
```

External emails are rejected locally before V-Claw calls the Google Chat API. Use email addresses instead of opaque `users/{id}` values so the domain can be verified.

The auth command creates `configs/google/token.json` locally for each user. This token is personal and ignored by git.

During auth, V-Claw starts a temporary local callback server on `127.0.0.1`, so the browser should end on a small success page. If that local callback cannot start, the CLI falls back to asking you to paste the redirected URL or authorization code.

## G1 scopes

```text
https://www.googleapis.com/auth/gmail.readonly
https://www.googleapis.com/auth/gmail.compose
https://www.googleapis.com/auth/gmail.send
https://www.googleapis.com/auth/gmail.modify
https://www.googleapis.com/auth/calendar.readonly
https://www.googleapis.com/auth/chat.spaces.readonly
https://www.googleapis.com/auth/chat.messages.create
https://www.googleapis.com/auth/chat.messages.readonly
https://www.googleapis.com/auth/chat.messages
https://www.googleapis.com/auth/chat.memberships
https://www.googleapis.com/auth/chat.spaces
```

`gmail.readonly` is used for message/thread reads and attachment metadata. `gmail.compose`, `gmail.send`, and `gmail.modify` support draft creation/update/send, attachment download, and message label changes. `chat.messages.create` is used by the smoke test when you send a text message to a Chat space. The broader Chat scopes support listing messages, sending text replies/attachments, updating or deleting messages, creating spaces, and adding or removing members.

Card messages are not supported by the current user OAuth flow. Use `google chat send -space ... -text ...` for normal messages. If the project needs rich cards later, add a Google Chat app authentication flow and update the tool contract, docs, and tests before exposing it to agents.

If you change OAuth scopes later, delete `configs/google/token.json` and run auth again. This is required after pulling the Gmail draft/modify scope expansion.

If Chat smoke test returns `Google Chat app not found`, open the Google Chat API Configuration tab in Google Cloud Console, fill in Application info, turn Interactive features off, click Save, wait a few minutes, then run smoke again.
