# Google Workspace OAuth

G1 uses OAuth user authentication. V-Claw runs locally, opens a browser consent flow, then calls Gmail, Calendar, and Google Chat on behalf of the signed-in Workspace user.

## Required Google Cloud setup

1. Enable Gmail API, Google Calendar API, and Google Chat API.
2. Configure the Google Chat API with an app name, avatar URL, and description. Turn interactive features off for G1.
3. Configure the OAuth consent screen for your Workspace.
4. Create an OAuth client with application type `Desktop app`.
5. Download the client JSON.

For this private/internal repo, the admin-owned OAuth Desktop app config is committed at `configs/google/oauth-client.internal.json` so Workspace members can run from source without downloading credentials.

Do not commit `credentials.json` or generated token files. `credentials.json` is only a local admin/developer override.

## Pull-and-run flow

Workspace members can pull the private repo and sign in immediately:

```powershell
go run ./cmd/vclaw google auth
go run ./cmd/vclaw google smoke
go run ./cmd/vclaw google smoke -chat-space spaces/AAAA...
```

The auth command creates `configs/google/token.json` locally for each user. This token is personal and ignored by git.

During auth, V-Claw starts a temporary local callback server on `127.0.0.1`, so the browser should end on a small success page. If that local callback cannot start, the CLI falls back to asking you to paste the redirected URL or authorization code.

## G1 scopes

```text
https://www.googleapis.com/auth/gmail.readonly
https://www.googleapis.com/auth/calendar.readonly
https://www.googleapis.com/auth/chat.spaces.readonly
https://www.googleapis.com/auth/chat.messages.create
```

`chat.messages.create` is only needed when you want the smoke test to send a text message to a Chat space.

If you change OAuth scopes later, delete `configs/google/token.json` and run auth again.

If Chat smoke test returns `Google Chat app not found`, open the Google Chat API Configuration tab in Google Cloud Console, fill in Application info, turn Interactive features off, click Save, wait a few minutes, then run smoke again.
