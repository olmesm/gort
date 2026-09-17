# Configuration

[Back to the README](../README.md) · [Deployment](deployment.md)

Set `GOTO_USE_HTTPS=true` behind HTTPS termination so session cookies are marked
Secure, and configure only the actual proxy CIDRs in `GOTO_TRUSTED_PROXIES`.

Goto reads process environment variables. For a shell session, export settings
before starting the application:

```sh
export GOTO_DB_CONNECTION='postgresql://localhost/goto'
export GOTO_DEFAULT_DOMAIN='links.example.com'
uv run goto
```

The application does not automatically load `.env`. Docker Compose reads `.env`
for substitution, then passes the variables listed in `docker-compose.yml` to
the container. See [deployment](deployment.md) for database and proxy setup.

Available settings:

| Variable | Default | Purpose |
|---|---|---|
| `GOTO_PORT` | `8080` | HTTP listen port |
| `GOTO_DEFAULT_DOMAIN` | `localhost:<port>` | Authority used to build short URLs when no domain is given |
| `GOTO_USE_HTTPS` | `false` | Render HTTPS short URLs and set Secure session cookies |
| `GOTO_DATA_DIR` | `./data` | Persistent session-signing key |
| `GOTO_DB_CONNECTION` | `postgresql://localhost/goto` | PostgreSQL URL or libpq connection string |
| `GOTO_SHORT_CODE_LENGTH` | `5` | Length of generated codes; minimum 4 |
| `GOTO_REDIRECT_STATUS` | `302` | Default redirect status: 301, 302, 307 or 308 |
| `GOTO_WORKERS_ENABLED` | `true` | Run title and webhook background threads |
| `GOTO_WEBHOOKS_ENABLED` | `false` | Enable webhook management and event delivery |
| `GOTO_AUTO_RESOLVE_TITLES` | `true` | Fetch page `<title>` in the background |
| `GOTO_DISABLE_TRACKING` | `false` | Disable visit recording |
| `GOTO_DISABLE_IP_TRACKING` | `false` | Record visits without storing IPs; IP-based redirect rules still use the request IP |
| `GOTO_ANONYMIZE_IPS` | `true` | Store IPv4 /24 and IPv6 /48 prefixes instead of full IPs |
| `GOTO_TRACK_SKIP_PARAM` | *(unset)* | Query parameter that disables tracking for a request, such as `no-track` |
| `GOTO_TRACK_ORPHAN_VISITS` | `true` | Track base-URL/404 traffic |
| `GOTO_BASE_URL_REDIRECT` | *(unset)* | Redirect for `GET /`; otherwise show a landing page |
| `GOTO_REGULAR_404_REDIRECT` | *(unset)* | Redirect for non-short-URL 404s |
| `GOTO_INVALID_SHORT_URL_REDIRECT` | *(unset)* | Redirect for unknown/expired short codes |
| `GOTO_INITIAL_ADMIN_USERNAME` | `admin` | Admin account created when no users exist |
| `GOTO_INITIAL_ADMIN_PASSWORD` | *(generated)* | Password for that new admin; does not reset existing accounts |
| `GOTO_TRUSTED_PROXIES` | *(unset)* | Comma-separated proxy CIDRs allowed to supply forwarded client IP and scheme. Headers from other peers are ignored. |
| `GOTO_ALLOW_PRIVATE_OUTBOUND` | `false` | Allow title fetching and webhooks to private/loopback destinations. Enable only for trusted deployments that need internal targets. |
| `GOTO_RATE_LIMIT_PER_MINUTE` | `120` | REST writes, GraphQL POSTs and login attempts per minute per IP; 0 disables the limit |
| `GOTO_OIDC_ISSUER` | *(unset)* | Enable SSO with the provider's issuer URL, such as `https://kc.example.com/realms/main` |
| `GOTO_OIDC_CLIENT_ID` | *(unset)* | OIDC client ID; required with issuer |
| `GOTO_OIDC_CLIENT_SECRET` | *(unset)* | OIDC client secret for confidential clients |
| `GOTO_OIDC_REDIRECT_URL` | *(derived)* | Callback override; default `{scheme}://{host}/admin/oidc/callback` |
| `GOTO_OIDC_SCOPES` | `profile email` | Extra scopes besides `openid`, separated by spaces or commas |
| `GOTO_OIDC_GROUPS_CLAIM` | `groups` | Token claim carrying the user's groups |
| `GOTO_OIDC_ADMIN_GROUP` | `goto-admins` | Members of this group become dashboard admins |
| `GOTO_OIDC_PROVIDER_NAME` | `SSO` | Label on the login button |
| `GOTO_OIDC_ONLY` | `false` | Disable local password login when OIDC is configured |

Domain-specific not-found redirects override the global settings. Configure them
in the dashboard or through `PATCH /rest/v1/domains/redirects`.

Goto accepts `X-Forwarded-For` and `X-Forwarded-Proto` only from configured
trusted proxies.

## Compose and test variables

The following variables are used by tooling, rather than application settings:

| Variable | Purpose |
|---|---|
| `GOTO_POSTGRES_PASSWORD` | Required database password for the root Compose stack; reuse the same value across starts. See [Docker setup](../README.md#docker). |
| `GOTO_TEST_POSTGRES_DSN` | Required connection string for the Python and browser test suites. See [development and checks](../README.md#development-and-checks). |
