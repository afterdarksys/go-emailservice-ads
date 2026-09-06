# QA and UAT execution guide

Updated 2026-09-06. Run against an isolated, single-owner instance with synthetic
accounts. Record the commit, configuration, client version and results for each
acceptance run. Do not use a production data directory for automated tests.

## Automated release gate

From the repository root with Go and Python 3 installed:

```sh
bash scripts/verify-release.sh
```

This verifies modules, runs vet and all Go tests, race-tests critical packages
including IMAP/JMAP, builds commands, and runs the live TLS smoke test. The smoke
test generates temporary certificates, chooses loopback ports and creates its own
data. It stops the service, creates/verifies/restores an offline backup, restarts
the restored service and verifies persisted state. Temporary files are removed.

| Workflow | Required observable result |
| --- | --- |
| Configuration validation | Unknown keys, extra YAML documents and invalid values fail before listeners or storage open. |
| SMTP submission | Authenticated STARTTLS submission arrives in the owner's IMAP mailbox. Disabled recipients fail admission. |
| REST authorization | Missing credentials return 401; insufficient permissions return 403. |
| Account management | Create and enable/disable work; oversized passwords return 400 without changing the old credential. |
| Policy management | Create, test and delete return the documented results. |
| IMAP folders | CREATE, subscriptions, metadata APPEND, COPY, hierarchical RENAME and DELETE persist. |
| IMAP protocol responses | SELECT supplies counts/UIDVALIDITY; STORE supplies changed FLAGS; EXPUNGE supplies the removed sequence number. |
| Message reading | BODY.PEEK and EXAMINE preserve unread state; BODY in a writable selection sets Seen. |
| Search | Header, decoded body, wildcard UID and sent-date criteria select matching messages. |
| Recovery | Credentials, folders, copied message isolation, flags and internal dates survive restored-service startup. |

Go regressions additionally cover quota-atomic COPY, account isolation, deleted
mailbox recreation UIDVALIDITY, Sieve failure handling and persistent delivery
flags, and JMAP ownership, folder visibility, state changes and downloads.

## Manual client acceptance

Use the same configured TLS trust and two synthetic accounts in each required
client (for example Thunderbird or Apple Mail). Run the table above, then keep two
sessions open on one mailbox: append mail in one session, confirm count refresh in
the other, change flags, and expunge. Confirm another account cannot read or copy
those messages. Record any timeout or unexpected protocol response as a defect.
Run SMTP send/reply with your target external provider and confirm headers and
folder placement. Automated Python protocol checks do not establish acceptance in
every desktop/mobile client.

## Sieve configuration and failure diagnosis

Per-user scripts are loaded from `<data_dir>/sieve/<escaped-username>.sieve` on
local delivery. The filename uses Go `url.PathEscape`; ordinary full email
addresses retain `@`. Use service-account ownership, directory mode 0700 and file
mode 0600. Replace scripts atomically. A missing script means normal delivery;
unreadable, malformed or unsupported scripts defer delivery and retain the queue
transaction for retry. Fix the script before retrying the message.

```sieve
require ["fileinto", "imap4flags", "body"];
if body :contains "project" {
    addflag "\\Flagged";
    fileinto "Projects";
    stop;
}
```

Supported declared extensions are fileinto, reject, envelope, body, variables and
imap4flags. Unsupported capabilities/tags/actions are rejected. Vacation, redirect
and multiple delivery actions are not implemented. A rejection after SMTP
acceptance generates a DSN subject to normal DSN preferences and loop protection;
`discard` intentionally consumes the message. Body tests inspect decoded MIME
text, not RFC 5322 headers. Flags and the selected folder persist together.

## Remaining acceptance boundaries

The optional JMAP listener currently offers a read-only subset; see
[JMAP API](JMAP_API.md). Writable JMAP, incremental synchronization and broader
Sieve extensions remain open work and must not receive a passing acceptance mark.

Production OAuth, public DNS/signing, external scanner behavior, Object Lock and
provider fencing require the actual deployment dependencies. Execute
[deployment qualification](DEPLOYMENT_QUALIFICATION.md) for those environments;
local fixtures are not evidence that a customer's deployment passed. Keep these
results separate from the automated release result, and preserve failures in TODO.
