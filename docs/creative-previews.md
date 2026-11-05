# Creative landing previews

Rocket can proxy an agent's running landing dev server to the browser viewing
Rocket, including a browser on another machine. This is private development
access, not hosting: the agent owns starting/stopping the server in its existing
creative session. Only servers configured for a URL base path are supported.

## Installation

For published single-file HTML in a browser on the Rocket machine, see
[the same-machine configuration](creative-artifacts-api.md#same-machine-viewing-without-dns-or-certificates).
That narrowly validated local mode requires no DNS or certificates. The remote
preview setup below remains unchanged.

Configure a **separate reachable hostname** for remote previews. A different path or
port on Rocket's own hostname is insufficient. For example:

```yaml
public_url: https://rocket.example.com
preview_host: 127.0.0.1
preview_port: 4480
preview_public_url: https://landing.example.net
preview_ports: [5100, 5101, 5102]
```

Arrange private DNS/TLS/reverse forwarding so `https://landing.example.net`
reaches this machine's `http://127.0.0.1:4480`. It must be reachable from the
browser's network, not just the agent machine. Use HTTPS when viewing Rocket over
HTTPS. The preview listener itself speaks HTTP; terminate TLS at the private
reverse proxy. Forward all paths and WebSocket upgrades without removing the
base path. Configure no Rocket API routes on this origin. Prefer a separate site
(as in the example) to avoid sharing parent-domain cookies with Rocket.

The preview origin is a separate installation requirement: an existing
`public_url`/Tailscale Serve route to Rocket does **not** forward the preview
listener automatically. Rocket does not configure tunnels, publish a site,
change DNS or restart the daemon. Apply configuration at an operator-controlled
restart. If preview setup is absent or its listener fails, the dashboard gets an
actionable unavailable status; existing Rocket access continues normally.

`preview_host` defaults to loopback; bind another address only when the private
network/reverse proxy requires it. `preview_ports` is the explicit allowlist of
ports reserved for landing dev servers. Never include ports used by unrelated
services. Privileged ports and Rocket's API/TLS/preview listener ports are rejected.
Preview registration accepts a port, never an arbitrary host or target URL.

## Agent workflow

```sh
rocket creative-preview setup --json
rocket creative-preview register --run 12 --stage 34 --item 56 --port 5100 --json
```

Registration returns access information even before a server is running:

```json
{
  "id": "CAPABILITY",
  "run_id": 12,
  "stage_id": 34,
  "item_id": 56,
  "port": 5100,
  "base_path": "/p/CAPABILITY/",
  "status": "unavailable",
  "url": "https://landing.example.net/p/CAPABILITY/",
  "message": "Preview server is unavailable. Start the registered dev server on its allowed loopback port with base path /p/CAPABILITY/"
}
```

Start the dev server with the returned base path, for example a Vite project's
existing dev script:

```sh
npm run dev -- --host 127.0.0.1 --port 5100 --strictPort --base /p/CAPABILITY/
rocket creative-preview list --run 12 --stage 34 --item 56 --json
rocket creative-preview revoke CAPABILITY
```

Use a free allowed port. The proxy preserves the entire base path and query,
including for asset requests and WebSocket/HMR connections. Configure any HMR
client override to use the browser's preview origin and base path. Do not emit
absolute agent-machine localhost URLs. Context lookup is exact: a run-only list
does not mix in stage/item registrations. An item requires its stage. Unknown,
wrong-ancestry and cross-project agent contexts are rejected.

Registrations and their secret capability URLs live only for this daemon's
lifetime. Restarting Rocket revokes them: register again and relaunch with the
new base path. Registering the same port/context again returns its existing capability. A port
already registered to another context is rejected; revoke before changing
projects/contexts. Revocation blocks new requests; it does not
stop the dev-server process or forcibly close existing WebSocket connections.
Stop the server in its owning session when finished. Keep capability URLs private.

## API and dashboard contract

All management routes use Rocket's existing device authentication on TCP; agent
identity is accepted only over the trusted Unix socket.

| Method | Path | Result |
| --- | --- | --- |
| GET | `/v1/creative/previews/setup` | `{configured, message?, allowed_ports: number[]}` |
| POST | `/v1/creative/previews` | 201 access object; body `{run_id, stage_id?, item_id?, port}` |
| GET | `/v1/creative/previews?run_id=12&stage_id=34&item_id=56` | Array of access objects for that exact context |
| DELETE | `/v1/creative/previews/{id}` | 204; revoke access |

An access object has `id`, `run_id`, optional `stage_id`/`item_id`, `port`,
`base_path`, `status` (`ready` or `unavailable`), optional `url` and `message`.
`url` is omitted when installation is unavailable; `base_path` is always returned
on registration. `ready` means the registered upstream currently answers 2xx at
its base path. It does not approve a creative result or verify external DNS/TLS
configuration. Redirects and failed/stopped upstreams are unavailable. Refresh
context lookup to get current status; proxy connection failures produce HTTP 502.

Use the same `url` for embedded viewing and opening a separate tab:

```html
<iframe src="ACCESS_URL" sandbox="allow-scripts allow-forms allow-same-origin"
        referrerpolicy="no-referrer" title="Landing preview"></iframe>
<a href="ACCESS_URL" target="_blank" rel="noopener noreferrer">Open preview</a>
```

Show unavailable messages when status is unavailable. Scripts, forms, JS modules
and localStorage work on the separate preview origin. Rocket also applies the
sandbox through response CSP, including in a separate tab. Top-level navigation,
popups and downloads are intentionally not enabled. Authentication cookies and
other upstream `Set-Cookie` headers are stripped; cookie-based application flows
are outside this landing-preview contract. Preview sites must not rely on
root-relative URLs outside their configured base path, absolute redirects,
service-worker scope expansion or direct localhost URLs. There is no universal
HTML/JavaScript rewriting layer.

The preview listener exposes only capability-scoped dev-server content. It does
not serve Rocket's dashboard or API. Rocket Cookie/Authorization/agent/forwarded
headers never reach the dev server; upstream cookies and CORS headers are
removed. Cross-origin browser protections and Rocket's existing origin checks
protect the privileged API. Preview content shares its preview origin with other
previews, so keep sensitive credentials out of landing projects.
