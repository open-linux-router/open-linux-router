# Administrator access

The first-run screen offers an administrator password (at least 12 characters)
and an explicit choice to continue without one. Existing password-free boxes
keep that choice until the administrator sets a password under **Advanced** →
**System** → **Administrator access**. A password protects the TCP API immediately; restarting
the daemon is unnecessary.

Passwords are stored as salted PBKDF2-SHA256 hashes with 600,000 iterations.
Browser sessions use random, HttpOnly, SameSite=Strict cookies lasting 12 hours.
HTTPS cookies also have Secure. Sign out revokes the current session; password
changes revoke all sessions. Restarting the control plane clears browser
sessions while independent network backends keep running. Login and password
hashing have bounded concurrency and source-based rate limits.

## HTTPS and network access

Plain HTTP remains available for compatibility and local recovery. It does not
encrypt passwords, cookies or API tokens in transit. Configure HTTPS before
using password authentication across an untrusted network, or reach loopback
through an SSH tunnel. The daemon accepts:

```
--listen 192.168.1.1:8443 --tls-cert /path/router.crt --tls-key /path/router.key
--admin-networks 192.168.1.0/24,fd00::/64
```

Put these in `OLRD_ARGS` in `/etc/open-linux-router/olrd.env` and restart olrd.
Both certificate and key are required; invalid credentials fail startup.
`admin-networks` is optional and filters the connection's client address, not
X-Forwarded-For. The Unix socket is unaffected by these restrictions.

Custom DNS names and HTTPS reverse proxies require no Host allowlist. The old
`--allowed-hosts` flag is accepted and ignored for upgrade compatibility.
Cross-site fetches and non-JSON API bodies are rejected, including on
password-free boxes and claim. A reverse proxy may terminate HTTPS and forward
HTTP to the daemon over a trusted local connection. Forwarding headers do not
widen permitted client addresses.

## API tokens and local recovery

`--auth` retains the separate bearer-token path for automation. A valid bearer
token can authorize the API alongside password sessions. On a password-free
box, `--auth` requires the token. Browser token entry is kept only for the
current tab/session; old localStorage tokens are removed and must be entered
again. The token is not a password hash and password rotation does not rotate
it.

The Unix control socket is authenticated by its filesystem permissions. It
does not require a password or session, so SSH/console access can recover a
forgotten password:

```
sudo olr claim --password < /path/to/protected-password-file
sudo olr system set password < /path/to/protected-password-file
sudo olr system set password --no-password
```

The input contains one password, optionally ending in a newline. Do not put
passwords in command arguments. Interactive echoed stdin is refused. Removing
the password deliberately permits password-free access again unless `--auth`
still requires a token.

The REST routes are `GET /api/system/access`, `POST /access/claim`,
`POST /access/login`, `POST /access/logout`, and `PUT /access/password`, with
the last four paths relative to `/api/system`. Password changes require the
current TCP credential or the local control socket. Access status contains
booleans only and is public so a browser can show the correct sign-in screen.

A hostile device already on the LAN can still claim a fresh, unclaimed box
first. Source restrictions and cross-site fetch checks reduce exposure but
do not establish physical ownership; claim from the local control socket when
the initial network is untrusted.
