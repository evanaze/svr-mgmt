# svr-mgmt

Small Go CLI for controlling a server's ATX power through a GL.iNet GLKVM Comet.

The GLKVM firmware is derived from PiKVM and exposes PiKVM-compatible ATX endpoints. GL.iNet's GoodCloud/Cloud API is not publicly documented for this control path, so this tool talks directly to your KVM over your tailnet. It defaults to `https://ai-kvm`.

## Build

```sh
go build -o svr-mgmt .
```

## Configure

```sh
export GLKVM_URL=https://ai-kvm
export GLKVM_USER=admin
export GLKVM_PASSWORD='your-kvm-password'
```

The KVM usually uses a self-signed TLS certificate, so certificate verification is skipped by default. To require valid TLS:

```sh
export GLKVM_INSECURE_SKIP_VERIFY=false
```

## Use

```sh
./svr-mgmt status
./svr-mgmt on
./svr-mgmt off
./svr-mgmt force-off
./svr-mgmt reset
```

Commands:

- All commands first log in with `POST /api/auth/login` and reuse the returned `auth_token` cookie for later API requests.
- `status` - read ATX power/HDD LED state from `GET /api/atx`
- `on` - read `GET /api/atx` first, skip if already on, refuse if ATX is busy, then request `POST /api/atx/power?action=on`
- `off` - check `GET /api/atx` first; if already off it does nothing, otherwise it long-presses the power button (`click?button=power_long`) to shut the server down. Used instead of the GLKVM's unreliable soft ACPI `action=off`.
- `force-off` - long-press power with `action=off_hard`
- `reset` - hardware reset with `action=reset_hard`
- `click`, `click-long`, `reset-click` - raw button clicks via `POST /api/atx/click`

The power commands (`on`, `off`, `force-off`, `reset`, and the `click`
variants) are synchronous: each sends its action to the GLKVM and waits for
the API to respond. The CLI only exits after the GLKVM responds (`ok=true`
means the operation finished) or the request times out (default 30s,
configurable with `-timeout`).

> **GLKVM HTTP 500 quirk.** Some GLKVM firmware builds perform the requested
> ATX action but then reply with `HTTP 500: Server got itself in trouble` (an
> unhandled Go server error) instead of a clean success response. To handle
> this, after a power/click POST returns 500 the CLI re-checks `GET
> /api/atx` (using a fresh timeout) to confirm the action actually took effect
> (e.g. power reaches the expected state, or ATX is no longer busy). If the
> action is confirmed, the command exits successfully with a
> "(confirmed despite HTTP 500)" note; if the expected state is never reached,
> the original HTTP 500 error is returned.

You can also pass config as flags:

```sh
./svr-mgmt -url https://ai-kvm -user admin -password 'your-kvm-password' on
```

### Debug the GLKVM API

Use `-debug` (or `GLKVM_DEBUG=true`) to log every request sent to and response
received from the GLKVM API to stderr. Request bodies are shown with the
password redacted.

```sh
./svr-mgmt -debug on
```

### Keep the managed server awake

`--keep-awake` / `-ka` uses **systemd-inhibit on the managed server** (not the host running this CLI) so the server does not suspend while you work on it. It runs `systemd-inhibit --what=sleep --who=svr-mgmt sleep infinity` on the server over SSH, detached in the background so the inhibit lock persists after the SSH session ends. The devices connect over your tailnet, so pass whatever target you would to `ssh` (e.g. `user@server` or an SSH config alias) and authenticate as you normally would (agent/key).

Specify the target with `-ssh-target` or `GLKVM_SSH_TARGET`:

```sh
./svr-mgmt -ssh-target user@server -ka on
```

```sh
export GLKVM_SSH_TARGET=user@server
./svr-mgmt -ka on
```

```sh
# Or use an SSH config alias
./svr-mgmt -ssh-target my-server -ka on
```

`-keep-awake` requires an `-ssh-target` and errors out if none is set. It also picks up `GLKVM_KEEP_AWAKE=true`.

`-keep-awake` keeps the server awake **indefinitely**. `systemd-inhibit` holds its lock only for the lifetime of its child, so the CLI runs a long-lived `sleep infinity` under it, detached with `nohup` and stdio redirected so nothing holds the SSH channel open. The inhibit lock persists until the process is killed or the machine reboots.
## Notes

- `off` long-presses the power button (same as holding the physical button) instead of the GLKVM's unreliable soft ACPI `action=off`. It checks the ATX state first and does nothing if the server is already off.
- `force-off` is equivalent to holding the physical power button and can lose data.
- `on` is preferred over `click` because the PiKVM-compatible API should do nothing if the server is already powered on.
- Use `-debug` (or `GLKVM_DEBUG=true`) to log every GLKVM API request and response to stderr, including redacted request bodies and response bodies. This is useful for troubleshooting commands that fail.
