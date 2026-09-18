# Users and access

Administrators manage accounts under **Settings → Users** (`/settings/users`). They can create an
account, edit its profile, assign a role and applications, deactivate/reactivate
it, and set a replacement password. Search and role/status filters help locate
accounts. Deactivation preserves history; accounts are not permanently deleted.
The audit log is available under **Settings → Audit** (`/settings/audit`), with
filters by action, acting user and outcome. It includes pipeline/stage and
schedule operations, bulk per-target results, settings changes, and sign-in and
sign-out activity. See [User activity audit](audit.md) for coverage, identity
semantics, the paginated API and upgrade instructions.
Other Settings tabs group API keys, licence information, and password settings.

## Roles

| Capability | Admin | Operator | Viewer |
| --- | --- | --- | --- |
| View assigned pipelines, stages, logs, context, executions and schedules | Yes | Yes | Yes |
| Pause/resume pipelines, retry/skip stages, use bulk actions | Yes | Yes | No |
| Trigger, pause, resume, archive and adjust policies on assigned schedules | Yes | Yes | No |
| Manage users, their roles, account status and application memberships | Yes | No | No |
| Manage settings, API keys, observability, action policies and licensing | Yes | No | No |
| View the audit log and worker dashboard | Yes | No | No |
| Change own password using the current password | Yes | Yes | Yes |

These are three built-in roles. Role creation and custom permission sets are not
part of this version. Admin user management covers the installation, but data
access still uses assigned applications for every role, including Admin. An
account with no assigned applications can sign in but sees no pipelines or
schedules. Administrators can assign any existing application through the user
editor; the application picker never includes API key secrets.

All restrictions are enforced by the internal API. Hiding controls in the web
app is only a usability measure. SDK and worker API key authentication is
unchanged; these user roles apply to dashboard sessions.

## Passwords and session revocation

New passwords require at least 12 Unicode characters and fit bcrypt's 72-byte
UTF-8 input limit. Administrators provide an initial or replacement password to
the user through their chosen channel; Pipelogiq does not send invitations or
password emails. Users can change their own password under **Account** in the
profile menu, including Operator and Viewer accounts.

Every account edit and administrator password reset revokes the target user's
sessions. A deactivated account cannot sign in. HTTP requests check current
account status, role, memberships and token version in the database. Live
streams are disconnected on local account changes and revalidate sessions
before delivering updates, including changes made through another API replica.
Streams also close when their original session token expires, even when idle.
Changing your own password retains your current HTTP session and revokes other
sessions. Saving your own profile requires signing in again.
An in-flight password change cannot overwrite a later administrator reset,
account edit, logout, or another completed password change.

Administrators cannot deactivate themselves or change their own role. The
store also serializes account management writes to prevent concurrent changes
from removing the last active administrator. Changes and password resets are
recorded in the audit log without plaintext passwords or password hashes.

## Upgrade

Apply `database/changelog.xml` before starting the updated API (the normal
Compose migration flow or `make migrate-up`). The migration:

1. Adds `user.is_active`, defaulting existing accounts to active.
2. Maps Admin/Administrator/SuperAdmin to `admin`, User/Operator to `operator`,
   and other legacy roles to `viewer`.
3. Revokes existing sessions once so everyone signs in with the new roles.
4. Adds a unique index on trimmed, case-insensitive email addresses.
5. Disables accounts still using the exact historical demo password hashes and
   revokes their sessions, including renamed demo identities. Accounts whose
passwords were replaced are preserved.

If existing emails collide after normalization, migration stops with an
explanation. Resolve those duplicate identities and rerun the migration; it
does not merge accounts or remove data automatically. Existing application
memberships are retained. The configured bootstrap administrator remains
authoritative on startup, so account management should be coordinated with
`ADMIN_EMAIL` configuration.
Bootstrap also disables unchanged demo credentials after an email rename,
preserves their history, and copies their application access to the configured
administrator. It no longer removes genuine accounts merely for sharing a
historical demo email. Supplying a valid non-demo `ADMIN_PASSWORD_HASH`
reactivates that administrator and revokes prior sessions when credentials or
permissions change; unchanged restarts preserve sessions. Without a replacement
hash, a disabled account stays disabled. Login always rejects the shipped demo
hashes, even if someone reactivates such an account without resetting its password.

## Proxy addresses and login throttling

Sign-in attempts are limited per account and per client IP, independently of
the TCP source port. Limits are local to each API process.
The API accepts `X-Real-IP` only from a loopback transport peer, matching the
bundled nginx proxy in the same container. That proxy overwrites the header;
`True-Client-IP` and `X-Forwarded-For` never select the rate-limit identity.
Direct clients cannot forge their audit address or rotate their login budget
using forwarding headers. A remote reverse proxy is recorded and throttled as
the transport peer; deployments needing individual clients behind it require
an explicitly trusted proxy configuration before changing this boundary.

## Internal API

All management endpoints require an active administrator session cookie.
The web proxy adds the `/api` prefix to these paths.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/users` | Accounts, roles, active status and application IDs |
| GET | `/users/roles` | Built-in role descriptions and permission IDs |
| GET | `/users/applications` | All application IDs, names and descriptions |
| POST | `/users` | Create account; returns 201 with account details |
| PUT | `/users/{id}` | Replace profile, role, status and memberships |
| POST | `/users/{id}/password` | Reset another user's password; returns 204 |

Create/update bodies contain `firstName`, optional `lastName`, `email`, `role`,
`isActive`, and `applicationIds` (an array; empty explicitly means no data
access). Creation also requires `password`. Password reset takes only
`password`. A successful update returns the account. Responses never expose
password hashes or session token versions. Invalid input returns 400, missing
accounts 404, and duplicate emails or account lockout conflicts 409.

## Verification

Run `make test`, `cd apps/go && go vet ./...`, and from `apps/web` run
`npm test`, `npm run lint`, and `npm run build`. The tests cover API role
enforcement, application isolation, revoked sessions, password resets,
disabled accounts, safe account updates, and role-aware UI behavior.

To exercise PostgreSQL concurrency protections, point
`PIPELOGIQ_TEST_DATABASE_URL` at a disposable test database and run
`go test ./internal/store -run TestPostgresUserManagement -count=1` from
`apps/go`. These tests create and remove their own schemas.

Run `sh database/test-user-management-upgrade.sh` to verify upgrades, normalized
email collisions, disabled demo credentials, repeatable migrations, and the
PostgreSQL security regressions. It creates and removes its own Docker network
and database and never connects to an application database.
