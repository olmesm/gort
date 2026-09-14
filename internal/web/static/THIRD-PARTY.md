# API documentation assets

`scalar-1.68.0.js` is the standalone browser build of `@scalar/api-reference`
1.68.0 from the npm registry, distributed under MIT. See `SCALAR-LICENSE.txt`.
It is embedded in the Gort binary. Agent, MCP, telemetry and credential
persistence are disabled in `api-docs.js`.

To update, extract `dist/browser/standalone.js` from the pinned package tarball,
update its filename in `app.go` and `templates/api_docs.html`, and run the browser
documentation test. It checks for JavaScript errors and external requests.
