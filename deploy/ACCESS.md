# ACCESS.md — putting clauditor behind Cloudflare Access

This assumes you already have a working Cloudflare Tunnel and Cloudflare
Access with a Google Workspace identity provider configured for your
account (SPEC §2) — this doc adds clauditor as one more hostname behind
that existing setup. It does **not** create a new tunnel or a new IdP.

## 1. Add the hostname to the existing tunnel

Two equivalent ways to do this — use whichever one you already manage the
tunnel with:

- **Dashboard-managed tunnel:** Zero Trust dashboard → **Networks → Tunnels**
  → select your existing tunnel → **Public Hostname** tab → **Add a public
  hostname**. Subdomain/domain: `clauditor.<yourdomain>`. Service: `HTTP`,
  `127.0.0.1:8790`. Save — this takes effect within seconds, no restart
  needed.
- **Config-file-managed tunnel:** merge `deploy/cloudflared/ingress-snippet.yml`
  into your tunnel's `config.yml` (above the catch-all rule) and reload
  cloudflared. See that file's header comment for the exact merge and
  reload steps.

Either way, the port (`8790` by default) must match `[serve].listen` in
clauditor's `config.toml`.

## 2. Create a Self-hosted Access application for it

Zero Trust dashboard → **Access → Applications → Add an application →
Self-hosted**.

- **Application domain:** `clauditor.<yourdomain>` (must match step 1
  exactly).
- **Session duration:** your call — a short-to-medium duration (e.g. 24h)
  is reasonable for a personal fleet-management tool; clauditor's own JWT
  validation (SPEC §9) checks `exp`/`iat` independently of this setting.
- **Identity providers:** reuse the **existing Google Workspace IdP**
  already configured for the account — do not create a new IdP for this.
- **Policy:** Action = `Allow`. Include rule — pick one:
  - `Include → Emails` → `you@yourdomain.com` (and any other specific
    people who should reach it), or
  - `Include → Google Workspace group` → the group your Workspace admin
    already uses for this kind of internal tooling access.

## 3. Find the AUD tag

After saving the application: **Access → Applications** → click the
`clauditor` app you just created → **Overview** tab. The **Application
Audience (AUD) Tag** is shown there — copy it verbatim.

## 4. Put both values into clauditor's config

Edit `~/.config/clauditor/config.toml` (or wherever `--config` points),
`[access]` section — same shape as `config.example.toml`:

```toml
[access]
team_domain = "yourteam.cloudflareaccess.com"   # from Zero Trust -> Settings -> Custom Pages,
                                                  # or just read it off the dashboard URL
                                                  # (https://<team>.cloudflareaccess.com/...)
policy_aud  = "<the AUD tag copied in step 3>"
```

Restart `clauditor serve` (or `systemctl --user restart clauditor` if
installed per `deploy/systemd/`) after editing.

## 5. Verify it worked

```
# Unauthenticated: expect a redirect into the Access login flow, not the API.
curl -si https://clauditor.<yourdomain>/api/v1/state | head -1
# -> HTTP/2 302 (Location: https://<team>.cloudflareaccess.com/...)

# /healthz is unauthenticated BY DESIGN (SPEC §7.2) — it always works,
# Access or no Access, so it's also a good "is the tunnel/hostname even
# routing correctly" smoke test:
curl -s https://clauditor.<yourdomain>/healthz
# -> {"ok":true,"version":"...","collectors":{...}}
```

Then, in a real browser: visit `https://clauditor.<yourdomain>/api/v1/state`,
log in through the Access prompt (Google Workspace SSO), and confirm it now
returns the JSON snapshot instead of redirecting. That confirms both halves
are wired correctly: Cloudflare Access is gating the hostname, and
clauditor's own middleware (`internal/api/middleware.go`) is independently
validating the `Cf-Access-Jwt-Assertion` it receives against your
`team_domain`/`policy_aud`.
