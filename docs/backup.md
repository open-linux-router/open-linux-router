# Back up and restore configuration

OLR keeps all module intent in `/etc/open-linux-router/olr.json`. A backup is
that same JSON document, including unredacted WireGuard server keys, proxy
passwords, provider credentials and the administrator password hash. Keep it
private. Use HTTPS or the local control socket to transfer credentials.

In the web UI, open **Advanced → Backup**. Download `olr.json`, or select a
backup to validate it before choosing **Replace settings and restore**.

From the CLI:

```sh
sudo olr backup ./olr-backup.json
sudo olr restore ./olr-backup.json --dry-run
sudo olr restore ./olr-backup.json --confirm
olr recovery status
sudo olr recovery confirm <id>
```

`backup` creates a mode-0600 file and refuses to overwrite an existing file.
It does not print credentials to the terminal. A direct copy of the complete
`olr.json` works too; redacted per-module API responses are not backups.

Restore validates the schema version, JSON structure, module fields, references
and target interfaces before changing configuration. Duplicate keys, masked
credentials and unsupported sections are refused. Interface names must exist
on the destination; edit the backup's names before restoring on different
hardware. Dry run creates only a temporary private validation file; it changes
no installed intent, kernel state or services.

Restore replaces the whole configuration. Omitted sections return to defaults,
including administrator access. The password in the backup takes effect; sign
in with it if the previous session stops working, or confirm from SSH/console.
The browser checks that configuration has not changed since validation. A
conflict requires reselecting the file and reviewing it again.

The existing configuration and connectivity are journaled before replacement.
Check router access and actual network service, then confirm within two minutes.
Otherwise the previous settings return automatically. An apply error attempts
an immediate rollback; incomplete recovery retains the journal and retries.
Restarting the daemon during an unconfirmed restore also triggers rollback.
Do not copy a backup over the live file by hand as a substitute for restore:
that bypasses validation, runtime reconciliation and connectivity protection.

## What is separate

This is a configuration backup, not an image of the Linux machine. It excludes:

- Daemon startup options in `olrd.env`, the separate `api-token`, and externally
  supplied HTTPS certificate/key files. Reconfigure these on the destination.
- Caddy's ACME accounts and issued certificate storage. Its configuration and
  DNS credentials are backed up; it can obtain certificates again, subject to
  provider availability and issuance limits.
- Inspection CA state and previously generated, unstored client private keys.
- SQLite observation history, login sessions and host recovery bookkeeping.
  See [storage.md](storage.md) for consistent runtime database backups. Do not
  transplant host recovery records from another machine.
- Linux packages, distribution networking, SSH keys or external backend files
  referenced by raw configuration. Install dependencies on the destination.

The REST surface is `GET /api/configuration/config` and
`POST /api/configuration/restore?dry_run=true` or `?confirm=true`. Restore takes
the complete JSON directly. Export contains secrets and sends `Cache-Control:
no-store`. All routes use normal administrator authentication. Recovery status,
confirmation and reversal are under `/api/recovery`.
