# Changelog

Release history follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and [Semantic Versioning](https://semver.org/). The release workflow uses each
version's section as its GitHub release notes.

## Unreleased

### Changed

- Name the application Goto, with the `goto` Python package and command,
  `GOTO_` environment variables and matching dashboard branding.
- Manage Python and uv with mise and run development tasks through Bash scripts.
- Use Python 3.12 with FastAPI, Pydantic, SQLModel, Alembic and Jinja.
- Use PostgreSQL with explicit Alembic migrations.
- Keep the dashboard page structure and make typography, control sizing, table
  alignment and keyboard focus indicators consistent across pages.
- Clarify API permissions, null updates and input limits in documentation and
  form help.
- Run the application through uv and Docker. Build Python wheels and source
  distributions in the release workflow.

### Added

- Python tests for validation, database migrations, REST/GraphQL, signed OIDC login,
  redirect tracking, outbound address checks and durable webhook retries.
- Guides for [deployment](docs/deployment.md), [configuration](docs/configuration.md)
  and the [REST and GraphQL APIs](docs/api.md).

## 0.2.1 - 2026-09-15

### Changed

- REST parses query parameters before calling shared operations; GraphQL passes
  typed filters and IDs. REST defaults and aliases are unchanged.
- The dashboard and APIs share validation and storage code for keys and webhooks.
- Tag, visit and short URL lists reuse the shared pagination query. Remove
  unused helpers and obsolete repository queries.
- Render chart SVG through a shared HTML template.

### Fixed

- Link edits save fields and tags in one transaction, rolling back both on failure.
- Dashboard key creation rejects expired keys and unknown roles.
- Dashboard handlers return server errors when templates fail to render.
- GraphQL hides internal database errors from clients.
- Webhook toggles update only the requested row in one query.

### Security

- Restrict author-key link reuse and global dashboard resources to authorized users.
- Reload user roles for each request and invalidate local sessions after password
  resets. OIDC sessions expire no later than the verified token.
- Protect dashboard forms with cross-origin checks, reject unsafe login return
  URLs and rate-limit password login attempts. HTTPS configuration enables Secure
  cookies.
- Block private and special-use destinations for title requests and webhooks by
  default. Accept forwarded headers only from configured trusted proxies.
- Reject overlong bcrypt passwords and create private data paths.

### Testing

- Add a PostgreSQL/Keycloak Compose stack and isolate integration tests in
  separate PostgreSQL schemas.
- Record security findings, remaining session limits and library decisions in
  `docs/security-review.md`.

### Upgrade notes

- No database migration is required. Existing session cookies require a new login.
- Behind a reverse proxy, configure `GOTO_TRUSTED_PROXIES` with its CIDRs so client
  IP addresses and forwarded schemes are accepted. Set `GOTO_USE_HTTPS=true` when
  serving the dashboard over HTTPS.
- Title resolution and webhook delivery block private destinations by default.
  Installations that intentionally use internal destinations can opt in with
  `GOTO_ALLOW_PRIVATE_OUTBOUND=true`.

## 0.2.0 - 2026-09-14

### Added

- GraphQL queries and mutations at `/graphql` for links, redirect rules, tags,
  domains, visits, statistics, API keys and webhooks. Nested link visits and
  redirect rules share the existing permissions and operations with REST.
- OpenAPI 3.1 documents at `/rest/openapi.json` and `/rest/openapi.yaml`, generated
  from every registered REST operation. Interactive REST docs at `/rest/docs`;
  GraphQL examples, a request editor and schema download at `/graphql/docs`.
- API keys page header links to both APIs and includes a curl example.
- GraphQL request size, parser-token and query-complexity limits, bounded field
  concurrency, authenticated introspection and POST rate limiting.

### Changed

- Rebuilt the dashboard with navy navigation, compact tables, consistent forms
  and responsive layouts. Routes, filters and pagination are unchanged. Removed
  decorative copy and design-preview controls.
- Bundle API documentation assets with the application. The editors keep keys in memory
  and send requests directly to the current Goto instance.

### Fixed

- Restrict scoped API keys to their permitted statistics and visits. Renaming and
  deleting shared tags requires an admin key. REST and GraphQL enforce the same
  permissions.
- Empty `group=` selects ungrouped links.
  Path-style slugs remain addressable through encoded REST path parameters.

### Upgrade notes

- `/graphql` and its children are reserved. Recreate any existing links under
  that prefix with another slug before upgrading.
- No database migration is required. Webhooks remain disabled by default;
  `GOTO_WEBHOOKS_ENABLED=true` also enables their GraphQL operations.

## 0.1.2 - 2026-09-14

### Changed

- Disable webhooks by default. Set `GOTO_WEBHOOKS_ENABLED=true` and restart to
  enable navigation, REST endpoints and workers. Stored configurations and pending
  deliveries are retained. Pending deliveries resume when enabled; events that
  occurred while disabled are not queued or replayed.
- Show 25 items per dashboard page. Add database pagination and search or filters
  to domains, API keys, users, webhooks and conditional redirect rules.

### Fixed

- Pagination preserves active filters, including tag searches and visit
  filters. Invalid and out-of-range pages resolve to valid page boundaries.
- Add links to clear filters and messages for empty lists.
- Align domain names, counts, inputs and buttons within rows. On narrow screens,
  tables scroll horizontally and row actions stay together.
- Wrapped mobile navigation reserves space above page headings and filters.
- Dashboard font loading no longer shifts navigation text between pages.

## 0.1.1 - 2026-09-09

### Fixed

- Return 404 for malformed `{id}` values in dashboard delete and toggle routes.
- Log JSON serialization failures and return a 500 problem document.
- Render templates to a buffer so failures do not send partial pages.

### Changed

- Add a quick start and manual walkthrough.

### Internal

- Route handler errors through one adapter. Represent RFC 7807 problems as errors.
- Render dashboard data rows directly where no conversion is needed.

## 0.1.0 - 2026-09-09

### Added

- Custom slugs, tags, lifetime controls, conditional redirects and multiple domains.
- Visit analytics, a REST API with API keys and RFC 7807 errors,
  an htmx dashboard, QR codes, signed webhooks and robots.txt.
- PostgreSQL persistence.
- OIDC login with group-scoped links and a configurable admin group.
