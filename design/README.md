# Goto design preview

[Back to the README](../README.md)

The preview uses Goto's dashboard with navy navigation, white pages and compact
tables. Its forms and routes operate on a disposable database.

## Run the preview

Follow the [Python and PostgreSQL setup](../README.md#python-312), including
mise activation. From the repository root, run `mise install` and
`uv sync --frozen` to install the configured tools and dependencies. Create a
disposable database with `createdb goto_design`, then start the preview:

```sh
GOTO_DB_CONNECTION=postgresql://localhost/goto_design \
GOTO_DATA_DIR=/tmp/goto-design-preview/data \
GOTO_PORT=18130 GOTO_DEFAULT_DOMAIN=localhost:18130 \
GOTO_AUTO_RESOLVE_TITLES=false GOTO_WEBHOOKS_ENABLED=true \
GOTO_INITIAL_ADMIN_USERNAME=demo \
GOTO_INITIAL_ADMIN_PASSWORD=goto-design-preview \
uv run goto
```

In another terminal, seed the database:

```sh
./scripts/seed-preview.sh postgresql://localhost/goto_design
```

The seed refuses to run if the database already contains links. It adds synthetic
links, visits, domains, tags, users, API-key labels, disabled webhooks and a
conditional redirect rule. Use it only with a disposable database.

Open <http://localhost:18130/admin> and sign in with `demo` / `goto-design-preview`.
These are demo credentials. Edits in the preview change its database.

The seed uses example destination domains; they are sample content, not live
sites. To reset the preview, stop Goto and drop the disposable database:

```sh
dropdb goto_design
```

The signing key remains in `/tmp/goto-design-preview/data/`. Remove that
directory when you no longer need the preview sessions.
