# O365 CLI

A cross-platform CLI tool for Microsoft 365 mail and calendar access via OAuth2 – no admin approval, no API keys required.

## How It Works

The tool uses the **OAuth2 Device Authorization Flow** with a Multi-Tenant Public Client App. Any O365 user can authenticate without requiring administrator approval. All operations use the **Microsoft Graph API**.

## Prerequisites

### Register Your Own Azure App (Recommended)

1. **Open Azure Portal**: https://portal.azure.com
2. **Create App Registration**:
   - Navigate to "Microsoft Entra ID" → "App registrations" → "New registration"
   - Name: e.g., "O365 CLI"
   - Supported account types: **"Accounts in any organizational directory (Any Microsoft Entra ID tenant - Multitenant)"**
   - Redirect URI: Type "Public client/native", URI: `http://localhost`
   - Click "Register"

3. **Copy Client ID**: On the overview page, find the "Application (client) ID"

4. **Enable Public Client**:
   - "Authentication" → "Advanced settings"
   - Set "Allow public client flows" to **Yes**
   - Save

5. **Add API Permissions**:
   - "API permissions" → "Add a permission" → "Microsoft Graph"
   - Select "Delegated permissions"
   - Add:
     - `Mail.ReadWrite`
     - `Mail.Send`
     - `Calendars.ReadWrite`
     - `offline_access` (for Refresh Tokens)

6. **Done!** No admin consent required for these permissions.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install patrick-hofmann/tap/o365-cli
```

### Go Install

```bash
go install github.com/patrick-hofmann/o365-cli/cmd/o365-cli@latest
```

### Download Binary

```bash
# macOS (Apple Silicon)
curl -L https://github.com/patrick-hofmann/o365-cli/releases/latest/download/o365-cli-darwin-arm64 -o /usr/local/bin/o365-cli && chmod +x /usr/local/bin/o365-cli

# macOS (Intel)
curl -L https://github.com/patrick-hofmann/o365-cli/releases/latest/download/o365-cli-darwin-amd64 -o /usr/local/bin/o365-cli && chmod +x /usr/local/bin/o365-cli

# Linux (x64)
curl -L https://github.com/patrick-hofmann/o365-cli/releases/latest/download/o365-cli-linux-amd64 -o /usr/local/bin/o365-cli && chmod +x /usr/local/bin/o365-cli

# Windows — download o365-cli-windows-amd64.exe from GitHub Releases
```

### Build from Source

```bash
git clone https://github.com/patrick-hofmann/o365-cli.git
cd o365-cli
make build && sudo cp o365-cli /usr/local/bin/
```

## Configuration

The tool works out-of-the-box with the built-in Client ID. Optionally, create `~/.o365-cli/config.yaml`:

```yaml
client_id: "your-azure-app-client-id"
current_account: "user@example.com"
```

Or set environment variables:

```bash
export O365_CLIENT_ID="your-client-id"
export O365_ACCOUNT="user@example.com"
```

## Usage

### Authentication

```bash
o365-cli auth login            # Login (multiple accounts supported)
o365-cli auth list             # List all logged-in accounts
o365-cli auth status           # Check token status
o365-cli auth switch user2@example.com  # Switch active account
o365-cli auth logout user@example.com   # Logout specific account
o365-cli auth logout --all     # Logout all accounts
```

### Multi-Account Support

```bash
o365-cli --account user2@example.com mail list
# Or via environment variable
O365_ACCOUNT="user2@example.com" o365-cli mail list
```

Priority: `--account` flag → `O365_ACCOUNT` env → `current_account` in config

### Mail

```bash
# List emails
o365-cli mail list
o365-cli mail list --folder "Sent Items" --limit 20
o365-cli mail list --unread --json

# Read email
o365-cli mail read <message-id>

# Send email
o365-cli mail send --to user@example.com --subject "Test" --body "Hello!"
o365-cli mail send --to user@example.com --body-file report.txt --html

# Reply / Forward
o365-cli mail reply <message-id> --body "Thanks!"
o365-cli mail forward <message-id> --to other@example.com

# Manage
o365-cli mail mark-read <message-id>
o365-cli mail move <message-id> --to "Archive"
o365-cli mail trash <message-id>

# Search
o365-cli mail search --from boss@example.com --since 7d
o365-cli mail query "subject:invoice AND from:finance"

# Archive from specific senders
o365-cli mail archive-from sender@example.com --dry-run
```

### Calendar

```bash
# List events
o365-cli calendar list                    # Next 7 days
o365-cli calendar list --days 14          # Next 14 days
o365-cli calendar today                   # Today's events
o365-cli calendar today --json            # JSON output

# View event details
o365-cli calendar get <event-id>

# Create event
o365-cli calendar create \
  --subject "Team Meeting" \
  --start "2026-04-01T10:00:00+02:00" \
  --end "2026-04-01T11:00:00+02:00" \
  --location "Room A" \
  --attendees user@example.com,other@example.com

# Update event
o365-cli calendar update <event-id> --subject "New Title"
o365-cli calendar update <event-id> --start "2026-04-01T14:00:00+02:00"

# Delete event
o365-cli calendar delete <event-id>

# Respond to invitations
o365-cli calendar accept <event-id>
o365-cli calendar decline <event-id> --comment "Can't make it"
o365-cli calendar tentative <event-id>
```

### Folders

```bash
o365-cli folders list
o365-cli folders create "Archive/2024"
o365-cli folders delete "Old Folder"
```

### Inbox Rules

```bash
o365-cli rules list
o365-cli rules get <rule-id>
o365-cli rules create --name "Auto-archive" --from-contains newsletter --move-to "Archive"
o365-cli rules delete <rule-id>
```

### Drafts

```bash
o365-cli drafts list
o365-cli drafts save --to user@example.com --subject "Draft" --body "WIP"
o365-cli drafts send <draft-id>
o365-cli drafts delete <draft-id>
```

## Permission Profiles

Restrict CLI operations with YAML profiles in `~/.o365-cli/profiles/`:

```yaml
# ~/.o365-cli/profiles/read-only.yaml
description: "Read-only access"
enforce: false
allow:
  - mail.read
  - calendar.read
  - folders.read
  - rules.read
  - config.read
  - auth
```

### Available Permissions

| Permission | Description |
|-----------|-------------|
| `mail.read` | List and read emails |
| `mail.send` | Send emails |
| `mail.modify` | Mark read/unread |
| `mail.move` | Move emails between folders |
| `mail.delete` | Trash emails |
| `calendar.read` | List and view events |
| `calendar.write` | Create and update events |
| `calendar.delete` | Delete events |
| `calendar.respond` | Accept/decline/tentative |
| `folders.read` | List folders |
| `folders.manage` | Create/delete folders |
| `rules.read` | List inbox rules |
| `rules.manage` | Create/update/delete rules |
| `drafts.list` | List drafts |
| `drafts.create` | Save drafts |
| `drafts.send` | Send drafts |
| `drafts.delete` | Delete drafts |
| `config.read` | View config |
| `config.write` | Change config |
| `auth` | Login/logout/status |

Usage:
```bash
o365-cli --profile read-only mail list    # OK
o365-cli --profile read-only mail send    # Denied
```

## Migration from v1.x

If upgrading from `o365-mail-cli` v1.x:

- The binary is now called `o365-cli` (was `o365-mail-cli`)
- Config directory auto-migrates from `~/.o365-mail-cli/` to `~/.o365-cli/` on first run
- Existing tokens and config are preserved
- You may need to re-login (`o365-cli auth logout && o365-cli auth login`) for calendar access (new scope)

## Token Management

Tokens are stored in `~/.o365-cli/token.json` with restricted permissions (0600).

- **Access Token**: Valid for ~1 hour
- **Refresh Token**: Valid for ~90 days (automatically renewed)
- The tool automatically refreshes when the access token expires

## Project Structure

```
o365-cli/
├── cmd/o365-cli/main.go         # Entry point
├── internal/
│   ├── graph/client.go          # Generic Microsoft Graph API client
│   ├── auth/                    # OAuth2 Device Flow & token cache
│   ├── mail/                    # Mail operations (via Graph API)
│   ├── calendar/                # Calendar operations (via Graph API)
│   ├── config/                  # Configuration & migration
│   ├── profile/                 # Permission profiles (RBAC)
│   └── cmd/                     # CLI command definitions
├── examples/profiles/           # Example permission profiles
├── go.mod
├── Makefile
└── README.md
```

## Development

```bash
go test ./...                    # Run tests
go vet ./...                     # Static analysis
O365_DEBUG=1 o365-cli mail list  # Debug mode
```

## License

MIT License - see LICENSE file.
