# OIDC provider setup

Styr authenticates with OpenID Connect only — no local passwords, no login wall beyond your own
identity provider. It uses the authorization code flow with PKCE, so a public client (no client
secret) works fine; a confidential client with a secret works too. The **first user to complete
login becomes admin**; everyone after that is a member.

Every provider needs the same three things from Styr, regardless of which one you use:

- **Redirect URI**: `<base_url>/api/v1/auth/callback` — substitute your own `base_url` from
  `config.yaml` (e.g. `https://styr.example.com/api/v1/auth/callback`).
- **Scopes**: `openid profile email`.
- **Grant type**: Authorization Code with PKCE (`S256`).

After registering the client, add it to `config.yaml`'s `oidc` list (see
[docs/CONFIGURATION.md](CONFIGURATION.md)):

```yaml
oidc:
  - name: Authentik
    issuer: https://auth.example.com/application/o/styr/
    client_id: styr
    client_secret: change-me   # omit for a public client; or set STYR_OIDC_CLIENT_SECRET
    scopes: [openid, profile, email]
```

`issuer` must be the exact URL OIDC discovery is served from — Styr fetches
`<issuer>/.well-known/openid-configuration` at startup (lazily, on first use of that provider).

Below are the fields to fill in for each provider. Menu labels can shift between versions —
these describe the fields to look for, not a guaranteed click path.

## Authentik

1. Applications → Providers → Create, type **OAuth2/OpenID Provider**.
2. Client type: **Public** (PKCE, no secret) or **Confidential** if you want a secret.
3. Redirect URIs: add `<base_url>/api/v1/auth/callback` (strict, matching exactly).
4. Scopes: ensure `openid`, `profile`(or `given_name`/`name` scope) and `email` are included in
   the provider's scope mapping — Authentik's default scopes usually already cover these.
5. Applications → Applications → Create, attach it to the provider above, set the launch URL if
   you want a tile in Authentik's own dashboard (optional).
6. `issuer` is the provider's OpenID URL, typically
   `https://auth.example.com/application/o/<slug>/` — Authentik shows this on the provider's
   detail page.
7. `client_id` is generated when the provider is created; copy it into `config.yaml`.

## Authelia

Authelia's OIDC clients are configured in `configuration.yml`, not a web UI — you add a client
entry rather than clicking through screens.

1. In Authelia's `identity_providers.oidc.clients` list, add an entry with:
   - `client_id`: a slug of your choice, e.g. `styr`.
   - `client_secret`: a hashed secret (Authelia requires hashing it with its own CLI, or you
     configure a public client instead — see the next point).
   - `public: true` for a PKCE-only public client (no secret needed on the Styr side), or
     `false` with a hashed secret for a confidential client.
   - `authorization_policy`: `two_factor` or `one_factor`, per your Authelia policy.
   - `redirect_uris`: `['<base_url>/api/v1/auth/callback']`.
   - `scopes`: `['openid', 'profile', 'email']`.
2. `issuer` in `config.yaml` is your Authelia base URL, e.g. `https://auth.example.com`.
3. `client_id` matches the slug you chose above; `client_secret` only if you configured a
   confidential client.

## Pocket ID

1. In the Pocket ID admin UI, go to OIDC Clients (or Applications) → Add client.
2. Give it a name (e.g. `Styr`) and set the callback/redirect URL to
   `<base_url>/api/v1/auth/callback`.
3. Pocket ID issues a client ID (and, depending on version, a client secret) when the client is
   created — copy both into `config.yaml` if a secret is shown; Pocket ID supports public
   clients with PKCE, so you can omit the secret if the client is created as public.
4. Confirm the client's scopes include `openid`, `profile` and `email` (Pocket ID enables these
   by default for most client types).
5. `issuer` is your Pocket ID instance URL, e.g. `https://id.example.com`.

## Keycloak

1. In your realm, go to Clients → Create client.
2. Client type: **OpenID Connect**. Client ID: e.g. `styr`.
3. Capability config: enable **Standard flow** (authorization code); disable Direct access
   grants unless you need it for something else. Set **Client authentication** off for a public
   client (PKCE), or on for a confidential client with a secret.
4. Login settings → Valid redirect URIs: `<base_url>/api/v1/auth/callback`.
5. If client authentication is on, the client's Credentials tab shows the client secret — copy
   it into `config.yaml` (or `STYR_OIDC_CLIENT_SECRET`).
6. Default client scopes in Keycloak already include `openid`, `profile` and `email`; confirm
   they're assigned to this client under the Client scopes tab.
7. `issuer` is `https://keycloak.example.com/realms/<realm-name>`.

## Multiple providers

`oidc` is a list — add more than one entry to show multiple login buttons (e.g. Authentik for
staff, Keycloak for a separate tenant). Each provider is independent; "first user becomes
admin" counts across all of them (the first successful login on any provider).

## Troubleshooting

- **State or nonce mismatch on callback**: usually a stale `styr_pkce` cookie (login flow
  started, then retried from a different tab or after the cookie expired) — start the login
  again from `/login`.
- **Redirect URI mismatch**: the provider is strict about an exact match, including trailing
  slashes and scheme (`https`, not `http`, once you're off localhost) — check `base_url` in
  `config.yaml` matches what you registered.
- **Discovery failure at startup/login**: Styr fetches
  `<issuer>/.well-known/openid-configuration` on first use of that provider; confirm `issuer` is
  reachable from the Styr host itself, not just from your browser.
