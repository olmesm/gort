/* API documentation clients. Credentials are never persisted. */
if (document.getElementById('rest-reference')) {
  Scalar.createApiReference('#rest-reference', {
    url: '/rest/openapi.json', theme: 'default', darkMode: false,
    servers: [{ url: window.location.origin }], agent: { disabled: true }, mcp: { disabled: true },
    persistAuth: false, withDefaultFonts: false, hideClientButton: true,
    showDeveloperTools: 'never', telemetry: false,
    customCss: '.light-mode { --scalar-color-accent: #173e70; --scalar-background-accent: #e4ecf5; }',
  });
}
const graphForm = document.getElementById('graphql-form');
if (graphForm) {
  graphForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = document.getElementById('graphql-run');
    const output = document.getElementById('graphql-result');
    button.disabled = true;
    output.textContent = 'Running…';
    try {
      const response = await fetch('/graphql', {
        method: 'POST', credentials: 'omit',
        headers: { 'Content-Type': 'application/json', 'X-Api-Key': document.getElementById('graphql-key').value.trim() },
        body: JSON.stringify({ query: document.getElementById('graphql-query').value, variables: JSON.parse(document.getElementById('graphql-variables').value || '{}') }),
      });
      output.textContent = JSON.stringify(await response.json(), null, 2);
    } catch (error) { output.textContent = error.message; }
    finally { button.disabled = false; }
  });
  document.getElementById('graphql-schema').addEventListener('toggle', async (event) => {
    if (!event.target.open || event.target.dataset.loaded) return;
    const output = document.getElementById('graphql-schema-text');
    try {
      const response = await fetch('/graphql/schema.graphql');
      if (!response.ok) throw new Error('Could not load the schema.');
      output.textContent = await response.text();
      event.target.dataset.loaded = 'true';
    } catch (error) { output.textContent = error.message; }
  });
}
