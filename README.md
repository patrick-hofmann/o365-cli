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
```

Or set environment variables:

```bash
export O365_CLIENT_ID="your-client-id"
```

## Usage

### Authentication

```bash
o365-cli auth login                      # Login (multiple accounts supported)
o365-cli auth list                       # List all logged-in accounts
o365-cli auth status                     # Check token status
o365-cli auth logout user@example.com    # Logout specific account
o365-cli auth logout --all               # Logout all accounts
```

### Multi-Account Behavior

**Read commands** (list, search, today) automatically show data from **all logged-in accounts**. Use `--account` to filter to a specific account:

```bash
o365-cli mail list                                # All accounts
o365-cli mail list --account user@example.com     # Specific account
o365-cli calendar today                           # All accounts
```

**Write commands** (send, create, delete, etc.) require `--account` when multiple accounts are logged in:

```bash
o365-cli mail send --account user@example.com --to ... --subject ... --body ...
o365-cli calendar create --account user@example.com --subject "Meeting" --start ... --end ...
```

With only one account logged in, `--account` is not needed — the single account is used automatically.

### Mail

```bash
# List emails (all accounts)
o365-cli mail list
o365-cli mail list --folder "Sent Items" --limit 20
o365-cli mail list --unread --json

# Read email (requires --account if multiple)
o365-cli mail read <message-id> --account user@example.com

# Send email (requires --account if multiple)
o365-cli mail send --account user@example.com --to recipient@example.com --subject "Test" --body "Hello!"

# Send with attachments — auto-routes inline POST (≤ 3 MB) vs. upload session (> 3 MB)
o365-cli mail send --account user@example.com \
  --to recipient@example.com --subject "Report" --body "Anbei." \
  --attach report.xlsx --attach summary.pdf

# Reply / Forward
o365-cli mail reply <message-id> --account user@example.com --body "Thanks!"
o365-cli mail forward <message-id> --account user@example.com --to other@example.com

# Reply-draft with attachment (TOFU auto-quote + Excel attached)
o365-cli mail drafts create-reply <message-id> --account user@example.com \
  --body "Anbei das Update." --attach report.xlsx

# Manage
o365-cli mail mark-read <message-id> --account user@example.com
o365-cli mail move <message-id> --account user@example.com --to "Archive"
o365-cli mail trash <message-id> --account user@example.com

# Search (all accounts)
o365-cli mail search --from boss@example.com --since 7d
o365-cli mail query "subject:invoice AND from:finance"

# Archive from specific senders
o365-cli mail archive-from sender@example.com --account user@example.com --dry-run
```

### Calendar

```bash
# List events (all accounts)
o365-cli calendar list                    # Next 7 days, all accounts
o365-cli calendar list --days 14          # Next 14 days
o365-cli calendar today                   # Today's events, all accounts
o365-cli calendar today --json            # JSON output

# View event details (requires --account if multiple)
o365-cli calendar get <event-id> --account user@example.com

# Create event (requires --account if multiple)
o365-cli calendar create --account user@example.com \
  --subject "Team Meeting" \
  --start "2026-04-01T10:00:00+02:00" \
  --end "2026-04-01T11:00:00+02:00" \
  --location "Room A" \
  --attendees colleague@example.com

# Update / Delete
o365-cli calendar update <event-id> --account user@example.com --subject "New Title"
o365-cli calendar delete <event-id> --account user@example.com

# Respond to invitations
o365-cli calendar accept <event-id> --account user@example.com
o365-cli calendar decline <event-id> --account user@example.com --comment "Can't make it"
o365-cli calendar tentative <event-id> --account user@example.com
```

### Folders

```bash
o365-cli folders list
o365-cli folders list --json          # includes folder IDs
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
| `mailbox.categories` | Add names to the Outlook category list (`workflow ensure-categories`) |
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

## Migration from v1.x / v2.0

- The binary is now `o365-cli` (was `o365-mail-cli`)
- Config directory auto-migrates from `~/.o365-mail-cli/` to `~/.o365-cli/`
- `auth switch` has been removed — there is no "current account" concept
- Read commands show all accounts by default; use `--account` to filter
- Write commands require `--account` when multiple accounts are logged in
- Re-login needed for calendar: `o365-cli auth logout --all && o365-cli auth login`

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

## Releasing

Releases are fully automated via GitHub Actions. To create a new release:

```bash
git tag v2.2.0
git push origin v2.2.0
```

This automatically:
1. Builds binaries for all platforms (macOS, Linux, Windows)
2. Creates a GitHub Release with the binaries attached
3. Updates the Homebrew formula in [patrick-hofmann/homebrew-tap](https://github.com/patrick-hofmann/homebrew-tap)

Users can then upgrade via:
```bash
brew upgrade o365-cli
```

## License

MIT License - see LICENSE file.

## OpenApe Pods read-only protocol

`o365-cli pods` is an isolated JSON interface. It never loads the normal CLI
configuration, migrates the home directory, selects an ambient account, opens a
browser, or requests write scopes. Supply the exact `--account` and an absolute
`--cache-dir` owned by this connection. The host broker must protect that directory,
serialize connection use, enforce assigned folders, and restrict process/network
access. This interface alone is not an operating-system sandbox.

```sh
o365-cli pods capabilities
o365-cli pods login --account pod@example.invalid --cache-dir /absolute/private/cache
o365-cli pods read --account pod@example.invalid --cache-dir /absolute/private/cache \
  --operation messages --folder inbox
```

Login emits a device-code event followed by a connection event, never a token.
Only `Mail.Read` is requested. The host presents the verification URL to the owner;
it must not give the login operation to an executing pod. A wrong signed-in account
is removed from the isolated cache and reported as an error.

Read operations are `folders`, `messages`, `attachments` (metadata), and
`attachment` (one attachment with content). Message and attachment reads require a
folder; attachment reads also require immutable message/attachment IDs. Child
folder inventories use `folders --folder ID`. Each response contains at most one
bounded page, `nextCursor`, and `complete`. Persist every page before passing its
cursor with `--cursor`. Follow child folders explicitly; never interpret one page
or one folder as the complete mailbox. Messages are not date-filtered, allowing a
full inventory to discover old mail moved into a selected folder.

Pagination remains on the exact collection and preserves the fixed projection;
foreign origins, redirects, mutation methods and expanded projections are rejected.
Reads request Microsoft's immutable ID preference. These IDs are stable across
folder moves within a mailbox, not across archive/export/re-import boundaries:
[Microsoft documentation](https://learn.microsoft.com/en-us/graph/outlook-immutable-id).
Use account plus immutable item ID and `changeKey` for versions; Internet Message-ID
is correlation metadata rather than a unique item key. The host must validate
returned records, normalize source provenance, and treat message/attachment text as
untrusted data.

Responses are capped at 32 MiB. Retries are bounded and context-cancellable;
incomplete or rejected pages fail visibly. Attachment downloads in the normal CLI
reject unsafe basenames, symlinks at the destination, existing files and invalid
base64 instead of overwriting unrelated data. The download directory and its
parents must be caller-owned; Pods uses its kernel-enforced private workspace.

Token updates use private atomic files, OS advisory locks and conflict detection.
A stale cache object cannot overwrite a newer refresh with `Save` or `Export`.
Conflicts require retry rather than silently discarding another process's update.
No new library version was introduced: file locks use the already pinned `x/sys`.

Verification uses synthetic OAuth/Graph transports only: device flow, refresh after
restart, wrong accounts, cancellation, scope boundaries, pagination, moved old
messages, redirects, throttling, attachment traversal/symlinks and cache conflicts.
`go vet ./...`, `go build ./cmd/o365-cli`, and `go test -race ./...` are the local
Go gates. Windows compilation is checked; platform runtime acceptance and live
tenant/consent behavior are separate gates. No live mail was used for these tests.

Inside the Pods sandbox, the host may set `PODS_CA_FILE` to an explicitly readable
snapshot of public CA certificates. OAuth and Graph use that snapshot with TLS
1.2 or newer, normal hostname verification and redirects disabled; no platform
trust-service permission or insecure verification switch is required. The host
must construct the environment itself and pin the snapshot digest. This is the
only Pods-specific environment input beyond the host's restricted HTTPS proxy;
account, cache directory and read scope still require explicit arguments.

Pods also checks the returned token boundary after silent refresh: the account's
username/home identity must match the selected account and granted scopes must be
`Mail.Read` plus standard OIDC identity scopes. A changed account or additional
resource permission is rejected and the offending cached account is removed.


### Bounded historical Pods reads

`o365-cli pods read --account ACCOUNT --cache-dir DIRECTORY --operation messages
--folder FOLDER --since 2026-06-01T00:00:00Z` applies an inclusive Graph
`receivedDateTime` filter before fetching message bodies. Omitting `--since`
preserves full-history behavior. Only canonical UTC timestamps and the messages
operation accept this option. Pagination must retain the exact filter and read
contract; altered or missing filter parameters are rejected.

Attachment reads use explicit message identities. The calling broker must bind
those identities to messages observed inside its assigned historical scope;
`--since` is deliberately rejected on attachment commands. This follows the
[Graph message filter and ordering contract](https://learn.microsoft.com/en-us/graph/api/user-list-messages?view=graph-rest-1.0).

### Pods workflow mail transport

`workflow capabilities` describes the additive `pods-mail/v1` JSON contract.
`workflow delta --account ADDRESS [--folder inbox|sentitems]` reads a bounded
delta page of the Inbox (default) or Sent Items; continue with its exact `next`
cursor until a `delta` boundary is returned. Both cursors remain bound to the
selected account and folder. Messages carry `categories` and `isRead`. Consumers may rescan from the Inbox on each run and use existing message categories
to select unfinished work. Durable synchronization consumers may retain the delta boundary.
`workflow read --account ADDRESS --message IMMUTABLE_ID` reads one message.
All message requests ask Graph for immutable IDs and retain `changeKey`, folder,
conversation, From/Sender/Reply-To/To/Cc, list headers and protection metadata.

`workflow label-snapshot --account ADDRESS --message IMMUTABLE_ID` returns the
complete message, its account Inbox ID, an `assessment-v1` digest as
`contentVersion` and an `assessment-v2` digest as `assessmentVersion`. The v1
digest covers the returned assessment data, excluding changeKey, folder, unread
state and categories. The v2 digest also excludes the Outlook flag and importance,
so triage marks do not count as new content. Existing v1 values are unchanged.
Those fields are independently checked immediately before a mutation. Incomplete
bodies, missing unread/category data and truncated messages cannot produce a snapshot.

`workflow find --account ADDRESS [--with NAME ...] [--without NAME ...]
[--has-attachments] [--oldest] [--top 1..50]` finds Inbox messages in one Graph
query instead of paging `delta`. `--with` names are any-of, `--without` names are
none-of; both compare exact category names. Messages come newest first (or oldest
first with `--oldest`) as `pods-mail/v1` items with only the immutable `id`,
`receivedDateTime`, `categories` and `hasAttachments`. Read the full message with
`label-snapshot` before acting.

`workflow attachments --account ADDRESS --message IMMUTABLE_ID --save-to ABSOLUTE_DIR`
stores every non-inline PDF attachment (up to 20 MiB) of one message as
`ABSOLUTE_DIR/<sha256>.pdf` and returns `name`, `path`, `sha256` and `size` per file.
A repeat reuses identical files and refuses a different file under the same name.
Reading the PDF text, including OCR for scans, is left to a separate PDF tool.

`workflow archive-labelled --account ADDRESS --message IMMUTABLE_ID --label NAME
--content-version assessment-v1:...|assessment-v2:... [--lock DIRECTORY]` moves one
message to Archive under the shared mailbox lock only while it is in the Inbox, carries
the exact category `NAME` and still has the assessed content version. Missing label:
`notApplied`; changed content: `deferred`; already outside the Inbox: `skipped`, so an
interrupted run can repeat. The label makes a narrowly scoped standing archive grant
possible (for example only mails labelled `Rechnung: abgelegt`).

`workflow add-category --account ADDRESS --message IMMUTABLE_ID --category NAME
[--lock DIRECTORY]` adds one category without a journal or operation ID. It holds
the shared mailbox lock (see `update-labels`), reads the current categories,
preserves them, and sends one PATCH only when the requested category is absent.
An unchanged repeat returns `confirmed` without writing. The response uses
`pods-mail/v1`; a lost write response remains `unknown`. A subsequent invocation
reads the message again, so an already applied category is a no-op. A lock that
stays busy longer than `--lock-wait` returns `notApplied` without a write.

`workflow update-labels --account ADDRESS --message IMMUTABLE_ID [--lock DIRECTORY]
--remove NAME ... --add NAME ... [--content-version assessment-v1:...|assessment-v2:...]`
removes and adds named categories in one PATCH. `--remove` and `--add` are
repeatable; removing an absent name or adding a present one changes nothing, so the
target is always the current set minus `--remove` plus `--add`. Other categories
stay untouched. A name in both lists is rejected. To replace a value of a label
family, remove all other values of that family in the same call. The message must
still be in the Inbox; with `--content-version` the write is refused when the
assessed content changed. The resulting category set is the only idempotency
state: a repeat after success returns `skipped` without writing, and a repeat
after an interruption or `unknown` result simply writes the remaining change.
The `o365-labels/v2` receipt reports `confirmed`, `skipped`, `deferred`, `busy`,
`notApplied` or `unknown` with `before` and `categories`.

Label writers and archivers serialize on one OS-backed lock file per account,
`DIRECTORY/<sha256(account)>.json.lock`. `--lock` names that directory for
`add-category` and `update-labels`; without it, `lock_dir` from the config file
is used (`o365-cli config set lock_dir /absolute/shared/directory`), and writes
are refused when neither is set. Use the same directory as the `--journal` of
`decide-label` and `archive`. `--lock-wait` (default 10s) bounds how long any
workflow mutation waits for a busy lock. Graph accepts stale If-Match headers, so
the lock protects only against other CLI writers, not against concurrent edits in Outlook.

`workflow decide-label --account ADDRESS --journal SHARED_PRIVATE_DIRECTORY
--request '{"message":"IMMUTABLE_ID","contentVersion":"assessment-v1:...",
"operationId":"STABLE_OPERATION","dimension":["Choice A","Choice B"],
"category":"Choice A","gate":{"requireUnread":true}}'` adds one decision
from a caller-declared dimension. The account in the optional request field must
exactly match `--account`. The gate also accepts `requireAll`, `requireAny`
(an array of alternative-name groups), `forbid`, and `exclusive` (groups with at
most one present name). Inbox membership is always required. Supply policy and
allowed choices in the recipe; do not accept them from mail text or model output.

The command takes the same nonblocking OS-backed account lock as protected
archiving, then reads the complete current message and Inbox identity. It checks
gates and conflicts, skips an existing dimension decision, and defers changed
assessment content. Only then does it merge its one selected category with the
fresh complete list. The durable journal records intent before one PATCH, then
records the exact provider readback. `confirmed`, `skipped`, `conflict`,
`deferred`, `busy`, `notApplied` and `unknown` distinguish results; only `confirmed`
establishes a completed write. A lock still busy after `--lock-wait` returns `busy` with no intent or provider effects; retry the same
retained operation later. A crashed
process releases its lock while retaining its uncertain intent. Evaluate a model
before acquiring this short write lock.

Participants in the journal-based `decide-label`/archive workflow must use the
identical assigned journal directory. `add-category` and `update-labels` share its
lock but write no journal records. Different directories do not share a lock or uncertainty holds.
The journal is an existing private absolute directory without symlink traversal.
Schema 4 preserves historical archive entries, adds label receipts and refuses
unsafe replay across callers. Older binaries refuse the upgraded journal; keep a
backup and pin the new build for every participant. `workflow categorize` and
`workflow move` now refuse raw mutation calls. Use `decide-label` and the exact
reviewed `workflow archive` command instead.

`workflow label-inspect --account ADDRESS --journal DIRECTORY [--operation ID]`
shows immutable receipts, exact digests and separate reconciliation records.
`workflow label-reconcile --account ADDRESS --journal DIRECTORY --message ID
--operation ORIGINAL_OPERATION --receipt-sha256 DIGEST --outcome
confirmed|notApplied --evidence OBSERVATION` records an explicit operator
observation without a provider call. Missing labels alone do not prove a write
failed. An uncertain label or archive attempt blocks new effects on that message
until reconciled. The original operation never executes again, including after a
`notApplied` resolution; a new attempt needs a new assessment and operation ID.

`workflow thread-evidence --account ADDRESS --message IMMUTABLE_ID [--cursor
OPAQUE_CURSOR]` reads bounded complete-body pages from that message's same-account
conversation. It returns the exact Sent Items folder ID and each message's
`isDraft` and folder. Evidence is Sent only when its folder equals `sentFolder`
and `isDraft` is false; matching the sender alone is insufficient. Follow `next`
until `complete` is true, retaining every page. Continuations bind the account,
conversation, assessment fields and unchanged origin-message digest. A thread
page is a fresh read, not an atomic snapshot of an entire conversation.

`workflow ensure-categories --account ADDRESS --categories '["Name",...]'` adds
the names missing from the mailbox's Outlook category list (the new Outlook only
shows message categories whose name is listed), reads the list back and is
**confirmed** only when every name is listed. Existing entries and colors stay
untouched; new names get a stable preset color. It needs the Graph scope
`MailboxSettings.ReadWrite`, which only `auth login --categories` requests, and
the `mailbox.categories` profile permission.

**Production limitation:** Graph documents folder-scoped move, but its published
contract does not guarantee `If-Match` on that operation. The transport sends the
reviewed version but advertises `conditionalMoveVerified: false`. Archiving requires an exact human approval
through the matching registry/IdP integration and the protected journal path.
Every terminal archive refusal after taking the lock is journaled, so restoring
eligibility never revives that operation. A busy lock cannot write a consumed
receipt; the requesting recipe must treat that attempt as terminal and require
a new operation and separate exact approval for any later attempt.
Its optional `--gate` JSON is part of the exact reviewed request returned by
`review-archive --gate`; execution checks that same gate on the freshly read
message. Freeze relevant observed labels in `requireAll`, with veto and exclusive
groups covering forbidden changes. Confirmed archival additionally requires a
fresh provider folder readback. Controlled transport tests prove request construction and
one-attempt behavior; they do not prove server-side concurrency semantics.
Manual Outlook changes remain outside the local lock; neither the lock nor
`If-Match` establishes provider compare-and-swap. Finding a message in Archive
alone never establishes who moved it.

The explicit account flag has no ambient-account fallback. Normal CLI profile
permissions still apply (`mail.read` or `mail.move`); no login, installation,
schedule or live mailbox mutation is part of this change. `workflow-shapes.toml`
provides an independently reviewable OpenApe command descriptor. Use a separate
reviewed installation; do not overwrite an owner's assigned executable in place.

References: [Graph immutable identities](https://learn.microsoft.com/en-us/graph/outlook-immutable-id),
[Graph move](https://learn.microsoft.com/en-us/graph/api/message-move?view=graph-rest-1.0).
