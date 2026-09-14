# Changelog

All notable changes to Gort are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
uses [Semantic Versioning](https://semver.org/).

The release workflow publishes the section matching the tag as the GitHub
release notes, so every release needs an entry below.

## [Unreleased]

## [0.2.1] - 2026-09-15

### Changed

- REST parses query parameters before shared operations; GraphQL passes typed
  filters and IDs directly. Existing REST defaults and aliases are retained.
- Dashboard and API key/webhook creation share validation and persistence.
- Tag, visit and short URL lists reuse the shared pagination query. Removed
  unused helpers and obsolete repository queries.

### Fixed

- Link edits save fields and tags in one transaction, rolling back both on failure.
- Dashboard key creation rejects expired keys and unknown roles.
- Dashboard handlers return server errors when templates fail to render.
- GraphQL hides internal database errors from clients.
- Webhook toggles update only the requested row in one query.

### Security

- Restrict author-key link reuse and global dashboard resources to authorized users.
- Refresh session authorization after account changes and invalidate local sessions
  after password resets. OIDC sessions are bounded by verified token expiry.
- Protect dashboard forms with Go's cross-origin checks, reject unsafe login return
  URLs, and rate-limit password login attempts. HTTPS configuration enables Secure cookies.
- Block private/special outbound destinations for title resolution and webhooks by
  default. Forwarded headers require explicitly trusted proxy CIDRs.
- Reject overlong bcrypt passwords, create private data paths, and bound GeoIP extraction.
- Update x/crypto to v0.56.0. Add routine database/race and vulnerability checks.

### Testing

- Add an isolated PostgreSQL/Keycloak Compose stack and run Go integration tests in
  separate PostgreSQL schemas. Chart SVG now uses the shared HTML template system.
- Record findings, residual session limitations and library tradeoffs in
  `docs/security-review.md`.

### Upgrade notes

- No database migration is required. Existing session cookies require a new login.
- Behind a reverse proxy, configure `GORT_TRUSTED_PROXIES` with its CIDRs so client
  IP addresses and forwarded schemes are accepted. Set `GORT_USE_HTTPS=true` when
  serving the dashboard over HTTPS.
- Title resolution and webhook delivery block private destinations by default.
  Installations that intentionally use internal destinations can opt in with
  `GORT_ALLOW_PRIVATE_OUTBOUND=true`.

## [0.2.0] - 2026-09-14

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

- Rebuilt the dashboard with navy navigation, compact tables, consistent forms,
  clearer hierarchy and responsive layouts. Existing routes, features, filters
  and pagination remain in place. Removed decorative copy and design controls.
- Chi handles routing, Huma binds and documents REST operations, and gqlgen
  provides GraphQL. REST URLs, response envelopes, error documents and partial
  update semantics are retained.
- API documentation assets ship in the binary. The built-in editors keep API
  keys in memory and send requests directly to the current Gort instance.
- Building from source now requires Go 1.26+. The Docker build uses Go 1.26.

### Fixed

- Scoped API keys can no longer access other domains' statistics or global tag
  statistics and visits. Renaming and deleting shared tags now requires an admin
  key. These permissions apply to both REST and GraphQL.
- Empty `group=` still selects ungrouped links after the router migration.
  Path-style slugs remain addressable through encoded REST path parameters.

### Upgrade notes

- `/graphql` and its children are reserved. Recreate any existing links under
  that prefix with another slug before upgrading.
- No database migration is required. Webhooks remain disabled by default;
  `GORT_WEBHOOKS_ENABLED=true` also enables their GraphQL operations.

## [0.1.2] - 2026-09-14

### Changed

- Webhooks are now disabled by default. Set `GORT_WEBHOOKS_ENABLED=true`
  and restart to enable webhook navigation, REST endpoints and delivery
  workers. Existing webhook configurations and pending deliveries are
  retained; pending deliveries resume when enabled. Events occurring while
  disabled are not queued or replayed.
- Dashboard lists consistently show 25 items per page. Domains, API keys,
  users, webhooks and conditional redirect rules now have database-backed
  pagination and relevant search or filter controls.

### Fixed

- Pagination preserves active filters, including tag searches and visit
  filters. Invalid and out-of-range pages resolve to valid page boundaries.
- Lists provide clear-filter links and explicit empty-result messages.
- Domain names, counts, inputs and action buttons align vertically within
  table rows. Narrow layouts scroll table contents without wrapping row
  actions onto inconsistent lines.
- Wrapped mobile navigation reserves space above page headings and filters.
- Dashboard font loading no longer shifts navigation text between pages.

## [0.1.1] - 2026-09-09

### Fixed

- Dashboard delete/toggle routes given a malformed `{id}` now answer 404
  instead of silently redirecting back to the list.
- A JSON response that fails to serialize is logged and answered with a
  500 problem document rather than a bare "serialization error".
- A template that fails part-way through no longer leaks a half-rendered
  page: pages render to a buffer first.

### Changed

- The HTTP server sets a 10s `ReadHeaderTimeout`.
- Request contexts propagate to the database driver, so a cancelled
  request cancels its queries.
- README: zero-config quick start and a manual walkthrough for the
  prebuilt binary.

### Internal

- Handlers return errors through one adapter instead of writing 500s
  inline; RFC 7807 problems are error values.
- Nullable columns scan straight into pointer fields; rows carry typed
  ids from the repository boundary up.
- Pure-projection dashboard pages render data rows directly via template
  funcs.
- Identifiers follow Go initialism style (`LongURL`, `ShortURLID`, `DB`).
  The JSON wire format and environment variables are unchanged.

## [0.1.0] - 2026-09-09

### Added

- First release: feature-for-feature Go port of Shortlink — short URLs
  with custom slugs, tags, lifetime controls and conditional redirect
  rules; multi-domain; visit analytics with GeoLite2 geolocation; REST API
  with API keys and RFC 7807 errors; htmx admin dashboard; QR codes;
  signed webhooks; robots.txt; SQLite (default) or PostgreSQL.
- OIDC single sign-on (Keycloak or any compliant IdP) with group-scoped
  link visibility and a configurable admin group.
- Single static binary releases for Linux and macOS (amd64, arm64).

[Unreleased]: https://github.com/olmesm/gort/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/olmesm/gort/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/olmesm/gort/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/olmesm/gort/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/olmesm/gort/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/olmesm/gort/releases/tag/v0.1.0
