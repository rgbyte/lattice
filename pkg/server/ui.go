package server

const consoleHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Lattice — Introspection, Validation & Admin Console</title>
<style>
  :root {
    --bg: #0f172a;
    --panel: #1e293b;
    --border: #334155;
    --text: #f8fafc;
    --muted: #94a3b8;
    --accent: #38bdf8;
    --ok: #22c55e;
    --warn: #eab308;
    --err: #ef4444;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, monospace;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    padding: 1rem 1.5rem;
    background: var(--panel);
    border-bottom: 1px solid var(--border);
    gap: 1rem;
  }
  header h1 {
    margin: 0;
    font-size: 1.15rem;
    color: var(--accent);
    letter-spacing: 0.02em;
  }
  .token-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  input, select, textarea, button {
    background: #0b1120;
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.45rem 0.7rem;
    font-family: inherit;
    font-size: 0.85rem;
  }
  button {
    background: #0284c7;
    border-color: #0369a1;
    cursor: pointer;
    font-weight: 600;
  }
  button:hover { background: #0369a1; }
  button.secondary { background: #334155; border-color: #475569; }
  button.danger { background: #dc2626; border-color: #b91c1c; }
  main {
    max-width: 1400px;
    margin: 0 auto;
    padding: 1.25rem;
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(410px, 1fr));
    gap: 1.25rem;
  }
  .card {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .card h2 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--accent);
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.4rem;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .row input { flex: 1; min-width: 120px; }
  pre {
    margin: 0;
    padding: 0.75rem;
    background: #0b1120;
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: auto;
    max-height: 280px;
    font-size: 0.78rem;
    color: #e2e8f0;
  }
</style>
</head>
<body>
<header>
  <h1>LATTICE // Context Graph Introspection, Validation &amp; Admin Console</h1>
  <div class="token-bar">
    <label for="adminToken">Bearer Token:</label>
    <input id="adminToken" type="password" placeholder="Optional Admin Token" />
    <button onclick="loadOverview()">Refresh Overview</button>
    <button class="secondary" onclick="runValidate()">Validate Graph Integrity</button>
  </div>
</header>

<main>
  <section class="card">
    <h2>1. System Overview &amp; Maintenance</h2>
    <div class="row">
      <button onclick="loadOverview()">System Overview</button>
      <button class="secondary" onclick="triggerEnrich()">Enrich Sweep</button>
      <button class="secondary" onclick="triggerRebuild()">Rebuild Indices</button>
    </div>
    <pre id="overviewOut">Click "System Overview" to inspect live CSR + HNSW + SQLite telemetry.</pre>
  </section>

  <section class="card">
    <h2>2. Graph &amp; Security Integrity Validator</h2>
    <div class="row">
      <button onclick="runValidate()">Run Full Audit (Cycles, Dangling Edges, Fail-Closed Nodes)</button>
    </div>
    <pre id="validateOut">No validation report run yet.</pre>
  </section>

  <section class="card">
    <h2>3. Principal ACL Simulator</h2>
    <div class="row">
      <input id="simUser" placeholder="user_id (e.g. alice)" value="alice" />
      <input id="simEmail" placeholder="email (e.g. alice@acme.com)" value="alice@acme.com" />
      <input id="simGroups" placeholder="groups comma-sep (e.g. eng,sre)" value="eng" />
    </div>
    <div class="row">
      <input id="simTargets" placeholder="target IDs comma-sep (e.g. doc:rfc-1,doc:secret)" />
      <button onclick="runACLSim()">Simulate Access</button>
    </div>
    <pre id="simOut">Enter principal claims and target IDs to simulate fail-closed ACL evaluation.</pre>
  </section>

  <section class="card">
    <h2>4. Hybrid Context-Graph Search</h2>
    <div class="row">
      <input id="searchQuery" placeholder="Search query..." />
      <input id="searchUser" placeholder="user_id" value="alice" />
      <input id="searchGroups" placeholder="groups (e.g. eng)" value="eng" />
      <button onclick="runSearch()">Search</button>
    </div>
    <pre id="searchOut">Run a principal-scoped query to inspect BM25 + HNSW + PageRank + Supersession breakdown.</pre>
  </section>

  <section class="card">
    <h2>5. Document &amp; Node Inspector</h2>
    <div class="row">
      <input id="inspectDocID" placeholder="Document ID (e.g. doc:rfc-1)" />
      <button onclick="inspectDoc()">Inspect Doc</button>
      <button class="danger" onclick="deleteDoc()">Delete Doc</button>
    </div>
    <div class="row">
      <input id="inspectNodeID" placeholder="Node ID (e.g. person:alice)" />
      <input id="inspectHops" type="number" value="2" style="max-width:70px" />
      <button onclick="inspectNode()">Inspect Node Subgraph</button>
    </div>
    <pre id="inspectOut">Inspect any document or vertex ego-network.</pre>
  </section>

  <section class="card">
    <h2>6. Path &amp; Bridge-ACL Explainer</h2>
    <div class="row">
      <input id="expFrom" placeholder="From Node ID (e.g. person:alice)" />
      <input id="expTo" placeholder="To Node ID (e.g. doc:rfc-2)" />
      <input id="expUser" placeholder="Principal user_id" />
      <input id="expGroups" placeholder="Principal groups" />
      <button onclick="explainPath()">Explain Connection</button>
    </div>
    <pre id="explainOut">Compare global shortest path vs principal-filtered path &amp; PageRank score.</pre>
  </section>
</main>

<script>
async function apiCall(path, method = 'GET', body = null) {
  const headers = { 'Content-Type': 'application/json' };
  const tok = document.getElementById('adminToken').value.trim();
  if (tok) headers['Authorization'] = 'Bearer ' + tok;
  const opts = { method, headers };
  if (body !== null) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  const data = await res.json().catch(() => ({ error: res.statusText }));
  if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}
function show(id, data) {
  document.getElementById(id).textContent = JSON.stringify(data, null, 2);
}
async function loadOverview() {
  try { show('overviewOut', await apiCall('/api/v1/introspect/overview')); }
  catch (e) { show('overviewOut', { error: e.message }); }
}
async function runValidate() {
  try { show('validateOut', await apiCall('/api/v1/validate', 'POST', {})); }
  catch (e) { show('validateOut', { error: e.message }); }
}
async function triggerEnrich() {
  try { show('overviewOut', await apiCall('/api/v1/admin/enrich-sweep', 'POST', { limit: 50 })); }
  catch (e) { show('overviewOut', { error: e.message }); }
}
async function triggerRebuild() {
  try { show('overviewOut', await apiCall('/api/v1/admin/rebuild', 'POST', {})); }
  catch (e) { show('overviewOut', { error: e.message }); }
}
async function runACLSim() {
  const groups = document.getElementById('simGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  const targets = document.getElementById('simTargets').value.split(',').map(s => s.trim()).filter(Boolean);
  try {
    show('simOut', await apiCall('/api/v1/validate/acl-simulate', 'POST', {
      user_id: document.getElementById('simUser').value.trim(),
      user_email: document.getElementById('simEmail').value.trim(),
      groups,
      target_ids: targets
    }));
  } catch (e) { show('simOut', { error: e.message }); }
}
async function runSearch() {
  const groups = document.getElementById('searchGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  try {
    show('searchOut', await apiCall('/api/v1/search', 'POST', {
      query: document.getElementById('searchQuery').value.trim(),
      user_id: document.getElementById('searchUser').value.trim(),
      groups,
      limit: 10
    }));
  } catch (e) { show('searchOut', { error: e.message }); }
}
async function inspectDoc() {
  const id = encodeURIComponent(document.getElementById('inspectDocID').value.trim());
  try { show('inspectOut', await apiCall('/api/v1/introspect/document?id=' + id)); }
  catch (e) { show('inspectOut', { error: e.message }); }
}
async function deleteDoc() {
  const id = encodeURIComponent(document.getElementById('inspectDocID').value.trim());
  try { show('inspectOut', await apiCall('/api/v1/admin/documents?id=' + id, 'DELETE')); }
  catch (e) { show('inspectOut', { error: e.message }); }
}
async function inspectNode() {
  const id = encodeURIComponent(document.getElementById('inspectNodeID').value.trim());
  const hops = encodeURIComponent(document.getElementById('inspectHops').value || '2');
  try { show('inspectOut', await apiCall('/api/v1/introspect/node?id=' + id + '&hops=' + hops)); }
  catch (e) { show('inspectOut', { error: e.message }); }
}
async function explainPath() {
  const groups = document.getElementById('expGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  try {
    show('explainOut', await apiCall('/api/v1/introspect/explain', 'POST', {
      from_id: document.getElementById('expFrom').value.trim(),
      to_id: document.getElementById('expTo').value.trim(),
      max_hops: 5,
      user_id: document.getElementById('expUser').value.trim(),
      groups
    }));
  } catch (e) { show('explainOut', { error: e.message }); }
}
loadOverview();
</script>
</body>
</html>
`
