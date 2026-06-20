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

- All commands first log in with `POST /api/auth/login` and keep the returned `auth_token` cookie.
- `status` - read ATX power/HDD LED state from `GET /api/atx`
- `on` - request `POST /api/atx/power?action=on&wait=1`
- `off` - request soft ACPI shutdown with `action=off`
- `force-off` - long-press power with `action=off_hard`
- `reset` - hardware reset with `action=reset_hard`
- `click`, `click-long`, `reset-click` - raw button clicks via `POST /api/atx/click`

You can also pass config as flags:

```sh
./svr-mgmt -url https://ai-kvm -user admin -password 'your-kvm-password' on
```

## Notes

- `off` is a normal short power-button press; the OS must handle ACPI shutdown.
- `force-off` is equivalent to holding the physical power button and can lose data.
- `on` is preferred over `click` because the PiKVM-compatible API should do nothing if the server is already powered on.
