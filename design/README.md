# Gort design preview

Index is the selected design: white surfaces, navy navigation and compact tables.
The preview uses the existing routes, forms, filters and pagination.

## Run a disposable preview

From the repository root, run:

```sh
GORT_DATA_DIR=/tmp/gort-design-preview/data \
GORT_PORT=18130 GORT_DEFAULT_DOMAIN=go.gort.test \
GORT_AUTO_RESOLVE_TITLES=false GORT_WEBHOOKS_ENABLED=true \
GORT_INITIAL_ADMIN_USERNAME=demo \
GORT_INITIAL_ADMIN_PASSWORD=gort-design-preview \
go run ./cmd/gort
```

Then, in another terminal, seed the **disposable** database:

```sh
python3 design/seed.py /tmp/gort-design-preview/data/gort.db
```

The seed refuses to overwrite a database containing links. It adds synthetic
links, visits, domains, tags, users, API-key labels, disabled webhooks and a
conditional redirect rule. No production data is used.

Open http://localhost:18130/admin and sign in with `demo` / `gort-design-preview`.
The preview is fully functional, so edits change its disposable database.
