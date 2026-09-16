# Configuration

[Back to the README](../README.md) · [Deployment](deployment.md)

Set `GORT_USE_HTTPS=true` behind HTTPS termination so session cookies are marked
Secure, and configure only the actual proxy CIDRs in `GORT_TRUSTED_PROXIES`.

Gort reads process environment variables. For a shell session, export settings
before starting the application:

```sh
export GORT_DB_CONNECTION='postgresql://localhost/gort'
export GORT_DEFAULT_DOMAIN='links.example.com'
uv run gort
```

The application does not automatically load `.env`. Docker Compose reads `.env`
for substitution, then passes the variables listed in `docker-compose.yml` to
the container. See [deployment](deployment.md) for database and proxy setup.

Available settings:

| Variable | Default | Purpose |
|---|---|---|
| `GORT_PORT` | `8080` | HTTP listen port |
| `GORT_DEFAULT_DOMAIN` | `localhost:<port>` | Authority used to build short URLs when no domain is given |
| `GORT_USE_HTTPS` | `false` | Render HTTPS short URLs and set Secure session cookies |
| `GORT_DATA_DIR` | `./data` | Persistent session-signing key |
| `GORT_DB_CONNECTION` | `postgresql://localhost/gort` | PostgreSQL URL or libpq connection string |
| `GORT_SHORT_CODE_LENGTH` | `5` | Length of generated codes; minimum 4 |
| `GORT_REDIRECT_STATUS` | `302` | Default redirect status: 301, 302, 307 or 308 |
| `GORT_WORKERS_ENABLED` | `true` | Run title and webhook background threads |
| `GORT_WEBHOOKS_ENABLED` | `false` | Enable webhook management and event delivery |
| `GORT_AUTO_RESOLVE_TITLES` | `true` | Fetch page `<title>` in the background |
| `GORT_DISABLE_TRACKING` | `false` | Disable visit recording |
| `GORT_DISABLE_IP_TRACKING` | `false` | Record visits without storing IPs; IP-based redirect rules still use the request IP |
| `GORT_ANONYMIZE_IPS` | `true` | Store IPv4 /24 and IPv6 /48 prefixes instead of full IPs |
| `GORT_TRACK_SKIP_PARAM` | *(unset)* | Query parameter that disables tracking for a request, such as `no-track` |
| `GORT_TRACK_ORPHAN_VISITS` | `true` | Track base-URL/404 traffic |
| `GORT_BASE_URL_REDIRECT` | *(unset)* | Redirect for `GET /`; otherwise show a landing page |
| `GORT_REGULAR_404_REDIRECT` | *(unset)* | Redirect for non-short-URL 404s |
| `GORT_INVALID_SHORT_URL_REDIRECT` | *(unset)* | Redirect for unknown/expired short codes |
| `GORT_INITIAL_ADMIN_USERNAME` | `admin` | Admin account created when no users exist |
| `GORT_INITIAL_ADMIN_PASSWORD` | *(generated)* | Password for that new admin; does not reset existing accounts |
| `GORT_TRUSTED_PROXIES` | *(unset)* | Comma-separated proxy CIDRs allowed to supply forwarded client IP and scheme. Headers from other peers are ignored. |
| `GORT_ALLOW_PRIVATE_OUTBOUND` | `false` | Allow title fetching and webhooks to private/loopback destinations. Enable only for trusted deployments that need internal targets. |
| `GORT_RATE_LIMIT_PER_MINUTE` | `120` | REST writes, GraphQL POSTs and login attempts per minute per IP; 0 disables the limit |
| `GORT_OIDC_ISSUER` | *(unset)* | Enable SSO with the provider's issuer URL, such as `https://kc.example.com/realms/main` |
| `GORT_OIDC_CLIENT_ID` | *(unset)* | OIDC client ID; required with issuer |
| `GORT_OIDC_CLIENT_SECRET` | *(unset)* | OIDC client secret for confidential clients |
| `GORT_OIDC_REDIRECT_URL` | *(derived)* | Callback override; default `{scheme}://{host}/admin/oidc/callback` |
| `GORT_OIDC_SCOPES` | `profile email` | Extra scopes besides `openid`, separated by spaces or commas |
| `GORT_OIDC_GROUPS_CLAIM` | `groups` | Token claim carrying the user's groups |
| `GORT_OIDC_ADMIN_GROUP` | `gort-admins` | Members of this group become dashboard admins |
| `GORT_OIDC_PROVIDER_NAME` | `SSO` | Label on the login button |
| `GORT_OIDC_ONLY` | `false` | Disable local password login when OIDC is configured |

Domain-specific not-found redirects override the global settings. Configure them
in the dashboard or through `PATCH /rest/v1/domains/redirects`.

Gort accepts `X-Forwarded-For` and `X-Forwarded-Proto` only from configured
trusted proxies.
