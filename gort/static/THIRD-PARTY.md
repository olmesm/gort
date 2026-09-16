# API documentation assets

`scalar-1.68.0.js` contains the standalone browser build of `@scalar/api-reference`
1.68.0 from npm. Gort bundles it under the MIT license in
[SCALAR-LICENSE.txt](SCALAR-LICENSE.txt). `api-docs.js` disables its agent, MCP,
telemetry and credential-persistence features.

To update Scalar:

1. Extract `dist/browser/standalone.js` from the chosen package version's tarball.
2. Rename the local file and update its references in `gort/app.py` and
   `gort/templates/rest-docs.html`.
3. Run the browser API-documentation test. It checks JavaScript errors and
   external requests.
