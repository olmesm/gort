# API reference

[Back to the README](../README.md) · [Configuration](configuration.md)

- [REST](#rest-api)
- [GraphQL](#graphql)

## REST API

Open `/rest/docs` for the interactive reference. Download the OpenAPI document
at `/rest/openapi.json` or `/rest/openapi.yaml`. FastAPI generates it from the
registered operations, including health. Webhook operations appear when enabled.
The API keys page links to the documentation and includes a curl example.

Create a key in the dashboard or through the API, and copy it when shown.
Authenticate with `X-Api-Key: <key>` or `Authorization: Bearer <key>`.

| Key role | Access |
|---|---|
| `admin` | All API operations |
| `author` | Short URLs created with that key |
| `domain` | Links and permitted statistics for one domain |

Only admin keys can rename or delete shared tags, read statistics or visits for
a tag, or view global/orphan statistics. Domain keys can view statistics for their own
domain; author keys must select an owned short code. Tag names and registered
domain names remain visible to authenticated API keys.

API operation errors use RFC 7807 `application/problem+json` responses.
Rate-limit responses use HTTP 429 with a plain-text message and `Retry-After`.

Short URL, tag and visit lists support `page` and `itemsPerPage` and return a
`pagination` envelope. Omitted or malformed page sizes default to 20 for links
and visits, or 500 for tags. Nonpositive sizes become 20; sizes above 500 are
capped at 500. Invalid page numbers default to 1, and out-of-range pages resolve
to the nearest valid page. Domain,
API-key and webhook REST lists return an unpaginated `data` envelope.
Dashboard lists have separate pagination and filters.

### Short URLs

| Method & path | Notes |
|---|---|
| `GET /rest/v1/short-urls` | `searchTerm`, `tags`, `tagsMode=any\|all`, `group` (`group=` alone filters to ungrouped), `startDate`, `endDate`, `domain`, `orderBy=dateCreated\|shortCode\|longUrl\|title\|visits` + `-ASC/-DESC`, `excludeMaxVisitsReached`, `excludePastValidUntil` |
| `POST /rest/v1/short-urls` | body: `longUrl` (required), `customSlug`, `shortCodeLength`, `domain`, `title`, `tags`, `group`, `maxVisits`, `validSince`, `validUntil`, `forwardQuery`, `crawlable`, `redirectStatus`, `findIfExists` |
| `GET /rest/v1/short-urls/{code}` | optional `?domain=` on all `{code}` routes |
| `PATCH /rest/v1/short-urls/{code}` | partial update; omitted fields stay unchanged; see null behavior below |
| `DELETE /rest/v1/short-urls/{code}` | |
| `GET/POST /rest/v1/short-urls/{code}/redirect-rules` | POST replaces all rules; conditions: `device`, `language`, `query-param`, `ip-address` |
| `GET /rest/v1/short-urls/{code}/visits` | `startDate`, `endDate`, `excludeBots` |
| `DELETE /rest/v1/short-urls/{code}/visits` | |

For link updates, `null` clears `title`, `group`, `maxVisits`, `validSince` and
`validUntil`. Both `tags: null` and `tags: []` remove all tags. Passing `null` for
`forwardQuery` or `crawlable` sets it to `false`; `longUrl` and `redirectStatus`
cannot be cleared.

Example:

```sh
curl -H "X-Api-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"longUrl":"https://example.com/landing","customSlug":"promo","tags":["marketing"]}' \
  http://localhost:8080/rest/v1/short-urls
```

`maxVisits` counts recorded visits, including bots. Disabling tracking or using
the configured tracking-skip parameter prevents that counter from advancing.
Concurrent requests can exceed the visit limit because checking the count and
recording a visit are separate operations.

### Tags, domains, visits and statistics

| Method & path | Notes |
|---|---|
| `GET /rest/v1/tags` | `withStats=true` (admin), `searchTerm` |
| `PUT /rest/v1/tags` | admin; `{"oldName":"a","newName":"b"}` |
| `DELETE /rest/v1/tags?tags=a,b` | admin |
| `GET /rest/v1/tags/{tag}/visits` | admin |
| `GET /rest/v1/domains` | |
| `POST /rest/v1/domains` | admin; `{"domain":"links.example.com"}` |
| `PATCH /rest/v1/domains/redirects` | admin; replaces all three redirect settings; omitted fields are cleared |
| `DELETE /rest/v1/domains/{authority}` | admin; default domain is protected |
| `GET /rest/v1/domains/{authority}/visits` | |
| `GET /rest/v1/visits` | admin; global counters |
| `GET /rest/v1/visits/non-orphan` · `GET/DELETE /rest/v1/visits/orphan` | admin |
| `GET /rest/v1/stats/visits-per-day` | scope with `shortCode`, `tag`, `domain` or `orphan=true`; `startDate`/`endDate` |
| `GET /rest/v1/stats/breakdown?by=browser\|os\|referer\|device` | same scoping |

### API keys and webhooks

These operations require an admin key.

| Method & path | Notes |
|---|---|
| `GET/POST /rest/v1/api-keys` | create returns `apiKey` once; body: `name`, `role`, `domain`, `expiresAt` |
| `PATCH /rest/v1/api-keys/{id}` | `{"enabled":false}` |
| `DELETE /rest/v1/api-keys/{id}` | |
| `GET/POST /rest/v1/webhooks` | create returns the signing `secret` once |
| `PATCH /rest/v1/webhooks/{id}` · `DELETE /rest/v1/webhooks/{id}` | |

Webhooks are disabled by default. Set `GOTO_WEBHOOKS_ENABLED=true` and restart
to enable the dashboard page, API operations and delivery workers. While disabled,
Goto keeps existing webhook configurations and pending deliveries, but does not
queue or deliver new events. Pending deliveries resume when re-enabled; events
that occurred while disabled are not replayed.

Webhook deliveries contain JSON with `event`, `occurredAt` and `data` fields.
The `X-Goto-Event` header identifies the event. `X-Goto-Signature: sha256=<hex>`
contains the HMAC-SHA256 of the raw body, signed with the webhook secret.
Goto retries failed deliveries with exponential backoff, up to 6 attempts.
The delivery queue survives restarts. Receivers must tolerate duplicate deliveries;
see [delivery guarantees](deployment.md#processes-and-background-work).

### Health, QR codes and robots.txt

- `GET /rest/health` returns an unauthenticated health check.
- `GET /{code}/qr-code?size=300&format=png|svg&margin=1&errorCorrection=L|M|Q|H`
- `GET /robots.txt`

## GraphQL

Open `/graphql/docs` for examples and a request editor. Download the schema at
`/graphql/schema.graphql`, or use authenticated introspection from your own
GraphQL client. Documentation and schemas are public; API operations require a
key. Documentation assets are bundled, and the built-in editors do not persist
credentials or send requests through an external proxy.

```sh
curl http://localhost:8080/graphql \
  -H "X-Api-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"query":"query Links($count: Int!) { shortURLs(filter: {itemsPerPage: $count}) { data { shortCode shortUrl visitsSummary { total } visits(filter: {itemsPerPage: 2}) { data { date browser } } } pagination { totalItems } } }","variables":{"count":5}}'
```

Queries cover links, tags, domains, visits, statistics, API keys and webhooks.
Mutations create, update and delete those resources, replace redirect rules and
clear visits. `updateShortURL` uses the same omitted-field and null behavior as
[REST link updates](#short-urls), including `null` booleans becoming `false`.
In a link filter, `group: ""` selects ungrouped links; omitting it selects all
accessible groups.

Queries accept GET or POST; mutations require POST. There are no subscriptions.
Request bodies are limited to 1 MiB. Queries are limited to 10,000 parser tokens
and 10,000 complexity points. Page sizes multiply query cost, including nested
visit pages. POST requests count against `GOTO_RATE_LIMIT_PER_MINUTE`, including
queries. GraphQL resolves fields sequentially in a worker thread, using one
database session per request.

Check `errors` even when HTTP status is 200. Resolver errors include
`extensions.code` and `extensions.status`; authentication failures return HTTP
401 with a problem document. Schema validation and complexity errors use GraphQL's
standard error format. API keys and webhook secrets are shown once on creation;
selecting those fields in list queries returns an empty string. Disabled webhook
operations return a GraphQL error with status 404.

The `/graphql` path and its children are reserved for API routes.

### Update the GraphQL schema

Edit `goto/schema.graphql` and its resolver bindings in `goto/graphql.py`, then
run `uv run pytest tests/test_graphql.py`. The schema loads at startup without
code generation. REST and GraphQL call the same application operations.
