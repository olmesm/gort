# Changelog

All notable changes to Gort are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
uses [Semantic Versioning](https://semver.org/).

The release workflow publishes the section matching the tag as the GitHub
release notes, so every release needs an entry below.

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

[0.1.1]: https://github.com/olmesm/gort/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/olmesm/gort/releases/tag/v0.1.0
