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
- `off` - request soft ACPI shutdown with `action=off`
- `force-off` - long-press power with `action=off_hard`
- `reset` - hardware reset with `action=reset_hard`
- `click`, `click-long`, `reset-click` - raw button clicks via `POST /api/atx/click`

The power commands (`on`, `off`, `force-off`, `reset`, and the `click`
variants) are fully synchronous: each passes `wait=true` to the GLKVM so the
API withholds its HTTP response until the ATX action completes. The CLI only
exits after the GLKVM responds (`ok=true` means the operation finished) or the
request times out (default 10s, configurable with `-timeout`).

> **GLKVM HTTP 500 quirk.** Some GLKVM firmware builds perform the requested
> ATX action but then reply with `HTTP 500: Server got itself in trouble` (an
> unhandled Go server error) instead of a clean success response. To handle
> this, after a waited power/click POST returns 500 the CLI re-checks `GET
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

### Keep the host awake

`--keep-awake` / `-ka` enables the [GNOME Caffeine extension](https://github.com/eonpatapon/gnome-shell-extension-caffeine#command-line-support) on the host running this CLI (via `gsettings ... set org.gnome.shell.extensions.caffeine cli-toggle true`) before the KVM command runs, so the host does not suspend while you work on the server. It also picks up `GLKVM_KEEP_AWAKE=true`.

```sh
./svr-mgmt -ka on
```

The Caffeine gsettings schema is relocatable, so the schemadir is auto-detected from common install paths (user-local `~/.local/share/...`, the NixOS system path `/run/current-system/sw/...`, and `/usr[/local]/share/...`). Override with `-caffeine-schema-dir` or `GLKVM_CAFFEINE_SCHEMA_DIR` if your install lives elsewhere.

## Notes

- `off` is a normal short power-button press; the OS must handle ACPI shutdown.
- `force-off` is equivalent to holding the physical power button and can lose data.
- `on` is preferred over `click` because the PiKVM-compatible API should do nothing if the server is already powered on.
- Use `-debug` (or `GLKVM_DEBUG=true`) to log every GLKVM API request and response to stderr, including redacted request bodies and response bodies. This is useful for troubleshooting commands that fail.
