# Gort design preview

[Back to the README](../README.md)

The preview uses Gort's dashboard with navy navigation, white pages and compact
tables. Its forms and routes operate on a disposable database.

## Run the preview

Create a disposable PostgreSQL database with `createdb gort_design`, then run
from the repository root:

```sh
GORT_DB_CONNECTION=postgresql://localhost/gort_design \
GORT_DATA_DIR=/tmp/gort-design-preview/data \
GORT_PORT=18130 GORT_DEFAULT_DOMAIN=localhost:18130 \
GORT_AUTO_RESOLVE_TITLES=false GORT_WEBHOOKS_ENABLED=true \
GORT_INITIAL_ADMIN_USERNAME=demo \
GORT_INITIAL_ADMIN_PASSWORD=gort-design-preview \
uv run gort
```

In another terminal, seed the database:

```sh
uv run python design/seed.py postgresql://localhost/gort_design
```

The seed refuses to run if the database already contains links. It adds synthetic
links, visits, domains, tags, users, API-key labels, disabled webhooks and a
conditional redirect rule. Use it only with a disposable database.

Open <http://localhost:18130/admin> and sign in with `demo` / `gort-design-preview`.
These are demo credentials. Edits in the preview change its database.

The seed uses example destination domains; they are sample content, not live
sites. To reset the preview, stop Gort and drop the disposable database:

```sh
dropdb gort_design
```

The signing key remains in `/tmp/gort-design-preview/data/`. Remove that
directory when you no longer need the preview sessions.
