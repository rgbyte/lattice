package server

const consoleHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Lattice — Interactive Context Graph Explorer &amp; Admin Console</title>
<style>
  :root {
    --bg: #0b1120;
    --panel: #151f32;
    --panel-alt: #1e293b;
    --border: #2e3f5c;
    --text: #f8fafc;
    --muted: #94a3b8;
    --accent: #38bdf8;
    --ok: #22c55e;
    --warn: #f59e0b;
    --err: #ef4444;
    --purple: #a855f7;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Inter, monospace;
    background: var(--bg);
    color: var(--text);
    line-height: 1.45;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    padding: 0.85rem 1.5rem;
    background: var(--panel);
    border-bottom: 1px solid var(--border);
    gap: 1rem;
  }
  header h1 {
    margin: 0;
    font-size: 1.1rem;
    color: var(--accent);
    letter-spacing: 0.02em;
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }
  .badge {
    font-size: 0.72rem;
    padding: 0.2rem 0.55rem;
    border-radius: 999px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .badge.live { background: rgba(34, 197, 94, 0.18); color: #4ade80; border: 1px solid rgba(34, 197, 94, 0.4); }
  .badge.dryrun { background: rgba(245, 158, 11, 0.2); color: #fbbf24; border: 1px solid rgba(245, 158, 11, 0.45); }
  .token-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  input, select, textarea, button {
    background: #090e1a;
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.45rem 0.7rem;
    font-family: inherit;
    font-size: 0.83rem;
  }
  input:focus { outline: 1px solid var(--accent); border-color: var(--accent); }
  button {
    background: #0284c7;
    border-color: #0369a1;
    cursor: pointer;
    font-weight: 600;
    transition: background 0.15s ease;
  }
  button:hover { background: #0369a1; }
  button.secondary { background: #334155; border-color: #475569; }
  button.secondary:hover { background: #475569; }
  button.warn { background: #d97706; border-color: #b45309; color: #fff; }
  button.warn:hover { background: #b45309; }
  button.success { background: #16a34a; border-color: #15803d; color: #fff; }
  button.success:hover { background: #15803d; }
  button.danger { background: #dc2626; border-color: #b91c1c; }

  .container {
    max-width: 1520px;
    margin: 0 auto;
    padding: 1rem 1.25rem 2rem;
    display: flex;
    flex-direction: column;
    gap: 1.15rem;
  }

  /* Folder Generator & Interactive Graph Studio */
  .graph-studio {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .crawl-bar {
    padding: 0.85rem 1.1rem;
    background: var(--panel-alt);
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.6rem;
  }
  .crawl-bar label {
    font-size: 0.8rem;
    color: var(--muted);
    font-weight: 600;
  }
  .crawl-bar input.path-input {
    flex: 1;
    min-width: 280px;
    font-family: monospace;
  }
  .graph-toolbar {
    padding: 0.55rem 1.1rem;
    background: #101829;
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    font-size: 0.8rem;
  }
  .legend {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    flex-wrap: wrap;
  }
  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    color: #cbd5e1;
    cursor: pointer;
    user-select: none;
  }
  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    display: inline-block;
  }
  .graph-body {
    display: grid;
    grid-template-columns: 1fr 360px;
    min-height: 540px;
  }
  @media (max-width: 1024px) {
    .graph-body { grid-template-columns: 1fr; }
  }
  .canvas-wrap {
    position: relative;
    background: radial-gradient(circle at center, #111a2e 0%, #090d18 100%);
    min-height: 540px;
    overflow: hidden;
  }
  canvas#graphCanvas {
    width: 100%;
    height: 540px;
    display: block;
    cursor: grab;
  }
  canvas#graphCanvas:active { cursor: grabbing; }
  .canvas-overlay {
    position: absolute;
    bottom: 12px;
    left: 14px;
    background: rgba(15, 23, 42, 0.85);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.65rem;
    font-size: 0.75rem;
    color: var(--muted);
    pointer-events: none;
  }
  .graph-sidebar {
    border-left: 1px solid var(--border);
    background: #0f172a;
    padding: 0.9rem;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    max-height: 540px;
    overflow-y: auto;
  }
  .graph-sidebar h3 {
    margin: 0;
    font-size: 0.88rem;
    color: var(--accent);
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.35rem;
  }
  .stat-chips {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.45rem;
  }
  .stat-chip {
    background: #090e1a;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.45rem 0.6rem;
  }
  .stat-chip .k { font-size: 0.7rem; color: var(--muted); }
  .stat-chip .v { font-size: 1rem; font-weight: 700; color: var(--text); }

  .grid-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(420px, 1fr));
    gap: 1.15rem;
  }
  .card {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
  }
  .card h2 {
    margin: 0;
    font-size: 0.92rem;
    color: var(--accent);
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.4rem;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    align-items: center;
  }
  .row input { flex: 1; min-width: 120px; }
  pre {
    margin: 0;
    padding: 0.7rem;
    background: #090e1a;
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: auto;
    max-height: 260px;
    font-size: 0.76rem;
    color: #e2e8f0;
  }
</style>
</head>
<body>
<header>
  <h1>
    LATTICE // Context Graph Visual Explorer &amp; Control Plane
    <span id="graphModeBadge" class="badge live">LIVE STORED GRAPH</span>
  </h1>
  <div class="token-bar">
    <label for="adminToken">Bearer Token:</label>
    <input id="adminToken" type="password" placeholder="Optional Admin Token" style="width:160px" />
    <button onclick="loadLiveGraph()">Load Stored Graph</button>
    <button class="secondary" onclick="runValidate()">Audit Graph Integrity</button>
  </div>
</header>

<div class="container">
  <!-- Interactive Visual Graph Studio + Folder Path Dry-Run Generator -->
  <section class="graph-studio">
    <div class="crawl-bar">
      <label for="crawlPath">Folder Path:</label>
      <input id="crawlPath" class="path-input" value="/Users/rohitgupta/Documents/glean-lite/lattice" placeholder="/absolute/or/relative/folder/path" />
      <label for="crawlPerms">ACL:</label>
      <input id="crawlPerms" value="*" style="width:105px" placeholder="*,group:eng" />
      <label style="display:inline-flex;align-items:center;gap:0.3rem;cursor:pointer">
        <input id="crawlTests" type="checkbox" /> Include *_test.go
      </label>
      <button class="warn" onclick="runFolderCrawl(true)">Dry Run Folder (Preview Graph)</button>
      <button class="success" onclick="runFolderCrawl(false)">Index Folder into Lattice</button>
      <button class="secondary" onclick="loadLiveGraph()">Reset to Stored DB Graph</button>
    </div>

    <div class="graph-toolbar">
      <div class="legend">
        <span style="color:var(--muted);font-weight:600">Node Kinds:</span>
        <span class="legend-item" onclick="setKindFilter('all')"><span class="dot" style="background:#fff"></span>All</span>
        <span class="legend-item" onclick="setKindFilter('document')"><span class="dot" style="background:#38bdf8"></span>Document</span>
        <span class="legend-item" onclick="setKindFilter('symbol')"><span class="dot" style="background:#a855f7"></span>Code Symbol / Pkg</span>
        <span class="legend-item" onclick="setKindFilter('person')"><span class="dot" style="background:#22c55e"></span>Person</span>
        <span class="legend-item" onclick="setKindFilter('ticket')"><span class="dot" style="background:#eab308"></span>Ticket / PR</span>
        <span class="legend-item" onclick="setKindFilter('other')"><span class="dot" style="background:#f97316"></span>Concept / Service</span>
      </div>
      <div class="row">
        <input id="graphFilterText" placeholder="Highlight node (e.g. engine, alice, csr)..." oninput="renderCanvas()" style="width:230px" />
        <button class="secondary" onclick="zoomBy(1.25)">Zoom +</button>
        <button class="secondary" onclick="zoomBy(0.8)">Zoom -</button>
        <button class="secondary" onclick="resetCamera()">Center</button>
        <button class="secondary" onclick="reheatSimulation()">Re-Layout</button>
      </div>
    </div>

    <div class="graph-body">
      <div class="canvas-wrap">
        <canvas id="graphCanvas"></canvas>
        <div class="canvas-overlay" id="canvasHint">
          Drag nodes to pin • Scroll wheel to zoom • Drag canvas to pan • Click any node to inspect neighborhood
        </div>
      </div>
      <aside class="graph-sidebar">
        <h3>Graph Telemetry &amp; Selected Vertex</h3>
        <div class="stat-chips">
          <div class="stat-chip"><div class="k">Visible Nodes</div><div class="v" id="statNodes">0</div></div>
          <div class="stat-chip"><div class="k">Visible Edges</div><div class="v" id="statEdges">0</div></div>
          <div class="stat-chip"><div class="k">Mode</div><div class="v" id="statMode">Live DB</div></div>
          <div class="stat-chip"><div class="k">Selected Degree</div><div class="v" id="statDegree">-</div></div>
        </div>
        <pre id="selectedNodeOut" style="max-height:360px;flex:1">Click any node in the visual graph (or run a Folder Dry Run above) to inspect its properties, ACL permissions, PageRank centrality, and incident edges.</pre>
      </aside>
    </div>
  </section>

  <!-- Detailed Control Plane Cards -->
  <div class="grid-cards">
    <section class="card">
      <h2>1. System Overview &amp; Maintenance</h2>
      <div class="row">
        <button onclick="loadOverview()">System Overview</button>
        <button class="secondary" onclick="triggerEnrich()">Enrich Sweep</button>
        <button class="secondary" onclick="triggerRebuild()">Rebuild Indices</button>
      </div>
      <pre id="overviewOut">Loading live CSR + HNSW + SQLite telemetry...</pre>
    </section>

    <section class="card">
      <h2>2. Graph &amp; Security Integrity Validator</h2>
      <div class="row">
        <button onclick="runValidate()">Run Full Audit (Cycles, Dangling Edges, Fail-Closed Nodes)</button>
      </div>
      <pre id="validateOut">Click "Run Full Audit" to verify all 8 structural &amp; security invariants.</pre>
    </section>

    <section class="card">
      <h2>3. Principal ACL Simulator</h2>
      <div class="row">
        <input id="simUser" placeholder="user_id (e.g. alice)" value="alice" />
        <input id="simEmail" placeholder="email (e.g. rohit@acme.io)" value="rohit@acme.io" />
        <input id="simGroups" placeholder="groups comma-sep (e.g. group:eng)" value="group:eng" />
      </div>
      <div class="row">
        <input id="simTargets" placeholder="target IDs comma-sep" value="doc:rfc-auth-v2,doc:board-merger" />
        <button onclick="runACLSim()">Simulate Access</button>
      </div>
      <pre id="simOut">Enter principal claims and target IDs to simulate fail-closed ACL evaluation.</pre>
    </section>

    <section class="card">
      <h2>4. Hybrid Context-Graph Search (BM25 + HNSW + PageRank)</h2>
      <div class="row">
        <input id="searchQuery" placeholder="Search query..." value="Passkey WebAuthn" />
        <input id="searchUser" placeholder="user_id" value="alice" />
        <input id="searchGroups" placeholder="groups (e.g. group:eng)" value="group:eng" />
        <button onclick="runSearch()">Search</button>
      </div>
      <pre id="searchOut">Run a principal-scoped query to inspect BM25 + HNSW + PageRank + Supersession breakdown.</pre>
    </section>

    <section class="card">
      <h2>5. Document &amp; Node Inspector</h2>
      <div class="row">
        <input id="inspectDocID" placeholder="Document ID (e.g. doc:rfc-auth-v1)" value="doc:rfc-auth-v1" />
        <button onclick="inspectDoc()">Inspect Doc</button>
        <button class="danger" onclick="deleteDoc()">Delete Doc</button>
      </div>
      <div class="row">
        <input id="inspectNodeID" placeholder="Node ID (e.g. person:alice)" value="person:alice" />
        <input id="inspectHops" type="number" value="2" style="max-width:70px" />
        <button onclick="inspectNode()">Inspect Subgraph</button>
      </div>
      <pre id="inspectOut">Inspect any document or vertex ego-network.</pre>
    </section>

    <section class="card">
      <h2>6. Path &amp; Bridge-ACL Explainer</h2>
      <div class="row">
        <input id="expFrom" placeholder="From Node ID" value="person:alice" />
        <input id="expTo" placeholder="To Node ID" value="doc:board-merger" />
        <input id="expUser" placeholder="Principal user_id" value="rohit" />
        <input id="expGroups" placeholder="Principal groups" value="group:eng" />
        <button onclick="explainPath()">Explain Connection</button>
      </div>
      <pre id="explainOut">Compare global shortest path vs principal-filtered path &amp; PageRank score.</pre>
    </section>
  </div>
</div>

<script>
// --- REST API Helper ---
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

// --- Interactive Force-Directed Graph Engine ---
const canvas = document.getElementById('graphCanvas');
const ctx = canvas.getContext('2d');

let graphNodes = [];
let graphEdges = [];
let nodeById = new Map();
let activeKindFilter = 'all';
let selectedNode = null;
let hoveredNode = null;
let draggingNode = null;
let isPanning = false;
let panStartX = 0, panStartY = 0;
let camX = 0, camY = 0, camScale = 1.0;
let simAlpha = 1.0;
let animFrameId = null;

function colorForKind(kind) {
  switch ((kind || '').toLowerCase()) {
    case 'doc':
    case 'document': return '#38bdf8';
    case 'symbol':   return '#a855f7';
    case 'person':   return '#22c55e';
    case 'ticket':   return '#eab308';
    default:         return '#f97316';
  }
}

function resizeCanvas() {
  const rect = canvas.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  canvas.width = rect.width * dpr;
  canvas.height = rect.height * dpr;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  renderCanvas();
}
window.addEventListener('resize', resizeCanvas);

function setGraphData(nodes, edges, pprMap, modeLabel, isDryRun) {
  const rect = canvas.getBoundingClientRect();
  const cx = rect.width / 2 || 480;
  const cy = rect.height / 2 || 270;

  nodeById.clear();
  const maxPPR = Object.values(pprMap || {}).reduce((m, v) => Math.max(m, v || 0), 0.001);

  graphNodes = (nodes || []).map((n, idx) => {
    const angle = (2 * Math.PI * idx) / Math.max(1, nodes.length);
    const radiusSpread = Math.min(cx, cy) * 0.65 * (0.35 + 0.65 * ((idx % 7) / 6));
    const score = (pprMap && pprMap[n.id]) ? pprMap[n.id] : 0;
    const normScore = score / maxPPR;
    const r = 7 + Math.min(11, Math.sqrt(normScore) * 10);
    const item = {
      id: n.id,
      title: n.title || n.id,
      kind: (n.kind || 'other').toLowerCase(),
      properties: n.properties || {},
      permissions: n.permissions || [],
      pagerank: score,
      r,
      x: cx + Math.cos(angle) * radiusSpread,
      y: cy + Math.sin(angle) * radiusSpread,
      vx: 0,
      vy: 0,
      pinned: false
    };
    nodeById.set(item.id, item);
    return item;
  });

  graphEdges = [];
  for (const e of (edges || [])) {
    const s = nodeById.get(e.source_id);
    const t = nodeById.get(e.target_id);
    if (s && t) {
      graphEdges.push({
        id: e.id,
        source: s,
        target: t,
        relation: e.relation || '',
        weight: e.weight || 1,
        evidence: e.evidence || '',
        provenance: e.provenance || ''
      });
    }
  }

  document.getElementById('statNodes').textContent = String(graphNodes.length);
  document.getElementById('statEdges').textContent = String(graphEdges.length);
  document.getElementById('statMode').textContent = modeLabel;

  const badge = document.getElementById('graphModeBadge');
  if (isDryRun) {
    badge.className = 'badge dryrun';
    badge.textContent = 'DRY-RUN PREVIEW (NOT PERSISTED)';
  } else {
    badge.className = 'badge live';
    badge.textContent = 'LIVE STORED GRAPH';
  }

  selectedNode = null;
  document.getElementById('statDegree').textContent = '-';
  resetCamera();
  reheatSimulation();
}

function reheatSimulation() {
  simAlpha = 1.0;
  if (!animFrameId) {
    animFrameId = requestAnimationFrame(tickSimulation);
  }
}

function tickSimulation() {
  animFrameId = null;
  const visibleNodes = getVisibleNodes();
  const visibleSet = new Set(visibleNodes.map(n => n.id));
  const visibleEdges = graphEdges.filter(e => visibleSet.has(e.source.id) && visibleSet.has(e.target.id));

  const rect = canvas.getBoundingClientRect();
  const cx = rect.width / 2 || 480;
  const cy = rect.height / 2 || 270;

  if (simAlpha > 0.01 && visibleNodes.length > 0) {
    // 1. Repulsion (Coulomb)
    const repulsion = 2600;
    for (let i = 0; i < visibleNodes.length; i++) {
      const a = visibleNodes[i];
      for (let j = i + 1; j < visibleNodes.length; j++) {
        const b = visibleNodes[j];
        let dx = b.x - a.x;
        let dy = b.y - a.y;
        let distSq = dx * dx + dy * dy + 16;
        if (distSq > 90000) continue;
        let dist = Math.sqrt(distSq);
        let force = (repulsion / distSq) * simAlpha;
        let fx = (dx / dist) * force;
        let fy = (dy / dist) * force;
        if (!a.pinned) { a.vx -= fx; a.vy -= fy; }
        if (!b.pinned) { b.vx += fx; b.vy += fy; }
      }
    }

    // 2. Spring attraction along edges (Hooke)
    const targetLen = visibleNodes.length > 60 ? 95 : 125;
    for (const e of visibleEdges) {
      const a = e.source;
      const b = e.target;
      let dx = b.x - a.x;
      let dy = b.y - a.y;
      let dist = Math.sqrt(dx * dx + dy * dy) || 1;
      let displacement = dist - targetLen;
      let force = displacement * 0.035 * simAlpha;
      let fx = (dx / dist) * force;
      let fy = (dy / dist) * force;
      if (!a.pinned) { a.vx += fx; a.vy += fy; }
      if (!b.pinned) { b.vx -= fx; b.vy -= fy; }
    }

    // 3. Center gravity + velocity integration
    for (const n of visibleNodes) {
      if (n.pinned) continue;
      n.vx += (cx - n.x) * 0.008 * simAlpha;
      n.vy += (cy - n.y) * 0.008 * simAlpha;
      n.vx *= 0.78;
      n.vy *= 0.78;
      n.x += n.vx;
      n.y += n.vy;
    }

    simAlpha *= 0.96;
    if (simAlpha > 0.012) {
      animFrameId = requestAnimationFrame(tickSimulation);
    }
  }
  renderCanvas();
}

function getVisibleNodes() {
  if (activeKindFilter === 'all') return graphNodes;
  return graphNodes.filter(n => {
    if (activeKindFilter === 'document') {
      return n.kind === 'doc' || n.kind === 'document';
    }
    if (activeKindFilter === 'other') {
      return !['doc', 'document', 'symbol', 'person', 'ticket'].includes(n.kind);
    }
    return n.kind === activeKindFilter;
  });
}

function setKindFilter(kind) {
  activeKindFilter = kind;
  reheatSimulation();
}

function resetCamera() {
  camX = 0;
  camY = 0;
  camScale = 1.0;
  renderCanvas();
}

function zoomBy(factor) {
  const rect = canvas.getBoundingClientRect();
  const mx = rect.width / 2;
  const my = rect.height / 2;
  const nextScale = Math.min(4.0, Math.max(0.2, camScale * factor));
  camX = mx - (mx - camX) * (nextScale / camScale);
  camY = my - (my - camY) * (nextScale / camScale);
  camScale = nextScale;
  renderCanvas();
}

function screenToWorld(sx, sy) {
  return {
    x: (sx - camX) / camScale,
    y: (sy - camY) / camScale
  };
}

function renderCanvas() {
  const rect = canvas.getBoundingClientRect();
  ctx.clearRect(0, 0, rect.width, rect.height);

  const visibleNodes = getVisibleNodes();
  const visibleSet = new Set(visibleNodes.map(n => n.id));
  const visibleEdges = graphEdges.filter(e => visibleSet.has(e.source.id) && visibleSet.has(e.target.id));
  const searchSub = (document.getElementById('graphFilterText').value || '').trim().toLowerCase();

  const neighborSet = new Set();
  if (selectedNode) {
    neighborSet.add(selectedNode.id);
    for (const e of visibleEdges) {
      if (e.source.id === selectedNode.id) neighborSet.add(e.target.id);
      if (e.target.id === selectedNode.id) neighborSet.add(e.source.id);
    }
  }

  ctx.save();
  ctx.translate(camX, camY);
  ctx.scale(camScale, camScale);

  // Draw directed edges
  for (const e of visibleEdges) {
    const isHighlighted = selectedNode && (e.source.id === selectedNode.id || e.target.id === selectedNode.id);
    const isDimmed = selectedNode && !isHighlighted;

    ctx.strokeStyle = isHighlighted ? '#38bdf8' : (isDimmed ? 'rgba(71, 85, 105, 0.22)' : 'rgba(100, 116, 139, 0.55)');
    ctx.lineWidth = isHighlighted ? 2.0 : 1.1;

    const dx = e.target.x - e.source.x;
    const dy = e.target.y - e.source.y;
    const dist = Math.sqrt(dx * dx + dy * dy) || 1;
    const ux = dx / dist;
    const uy = dy / dist;

    const startX = e.source.x + ux * e.source.r;
    const startY = e.source.y + uy * e.source.r;
    const endX = e.target.x - ux * (e.target.r + 5);
    const endY = e.target.y - uy * (e.target.r + 5);

    ctx.beginPath();
    ctx.moveTo(startX, startY);
    ctx.lineTo(endX, endY);
    ctx.stroke();

    // Arrowhead
    const arrowLen = 6;
    const angle = Math.atan2(dy, dx);
    ctx.fillStyle = ctx.strokeStyle;
    ctx.beginPath();
    ctx.moveTo(endX + ux * 4, endY + uy * 4);
    ctx.lineTo(endX - arrowLen * Math.cos(angle - Math.PI / 6), endY - arrowLen * Math.sin(angle - Math.PI / 6));
    ctx.lineTo(endX - arrowLen * Math.cos(angle + Math.PI / 6), endY - arrowLen * Math.sin(angle + Math.PI / 6));
    ctx.closePath();
    ctx.fill();

    // Edge relation label on highlighted edges or when zoomed in
    if (isHighlighted || (camScale > 1.35 && visibleEdges.length < 60)) {
      const mx = (startX + endX) / 2;
      const my = (startY + endY) / 2;
      ctx.font = '10px monospace';
      ctx.fillStyle = isHighlighted ? '#7dd3fc' : '#94a3b8';
      ctx.textAlign = 'center';
      ctx.fillText(e.relation, mx, my - 4);
    }
  }

  // Draw nodes
  for (const n of visibleNodes) {
    const matchesSearch = searchSub && (n.id.toLowerCase().includes(searchSub) || n.title.toLowerCase().includes(searchSub));
    const isDimmed = (selectedNode && !neighborSet.has(n.id)) || (searchSub && !matchesSearch);

    ctx.save();
    ctx.globalAlpha = isDimmed ? 0.25 : 1.0;

    // Outer glow for selected, hovered, or search-matched node
    if (n === selectedNode || n === hoveredNode || matchesSearch) {
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.r + 5, 0, 2 * Math.PI);
      ctx.fillStyle = matchesSearch ? 'rgba(250, 204, 21, 0.35)' : 'rgba(56, 189, 248, 0.32)';
      ctx.fill();
    }

    ctx.beginPath();
    ctx.arc(n.x, n.y, n.r, 0, 2 * Math.PI);
    ctx.fillStyle = colorForKind(n.kind);
    ctx.fill();
    ctx.lineWidth = (n === selectedNode) ? 2.5 : 1.2;
    ctx.strokeStyle = (n === selectedNode) ? '#ffffff' : '#0f172a';
    ctx.stroke();

    // Node label
    if (!isDimmed || n === selectedNode) {
      const shortLabel = n.title.length > 28 ? n.title.slice(0, 26) + '…' : n.title;
      ctx.font = (n === selectedNode ? 'bold ' : '') + '11px -apple-system, BlinkMacSystemFont, monospace';
      ctx.fillStyle = '#f1f5f9';
      ctx.textAlign = 'center';
      ctx.fillText(shortLabel, n.x, n.y + n.r + 13);
    }
    ctx.restore();
  }

  ctx.restore();
}

// --- Canvas Pointer Interactions ---
function findNodeAt(sx, sy) {
  const pt = screenToWorld(sx, sy);
  const visibleNodes = getVisibleNodes();
  for (let i = visibleNodes.length - 1; i >= 0; i--) {
    const n = visibleNodes[i];
    const dx = pt.x - n.x;
    const dy = pt.y - n.y;
    if (dx * dx + dy * dy <= (n.r + 4) * (n.r + 4)) {
      return n;
    }
  }
  return null;
}

canvas.addEventListener('mousedown', (ev) => {
  const rect = canvas.getBoundingClientRect();
  const sx = ev.clientX - rect.left;
  const sy = ev.clientY - rect.top;
  const hit = findNodeAt(sx, sy);
  if (hit) {
    draggingNode = hit;
    hit.pinned = true;
    selectNodeInSidebar(hit);
  } else {
    isPanning = true;
    panStartX = sx - camX;
    panStartY = sy - camY;
    selectedNode = null;
    renderCanvas();
  }
});

window.addEventListener('mousemove', (ev) => {
  const rect = canvas.getBoundingClientRect();
  const sx = ev.clientX - rect.left;
  const sy = ev.clientY - rect.top;
  if (draggingNode) {
    const pt = screenToWorld(sx, sy);
    draggingNode.x = pt.x;
    draggingNode.y = pt.y;
    reheatSimulation();
    return;
  }
  if (isPanning) {
    camX = sx - panStartX;
    camY = sy - panStartY;
    renderCanvas();
    return;
  }
  if (sx >= 0 && sy >= 0 && sx <= rect.width && sy <= rect.height) {
    const hit = findNodeAt(sx, sy);
    if (hit !== hoveredNode) {
      hoveredNode = hit;
      canvas.style.cursor = hit ? 'pointer' : 'grab';
      renderCanvas();
    }
  }
});

window.addEventListener('mouseup', () => {
  draggingNode = null;
  isPanning = false;
});

canvas.addEventListener('wheel', (ev) => {
  ev.preventDefault();
  const rect = canvas.getBoundingClientRect();
  const mx = ev.clientX - rect.left;
  const my = ev.clientY - rect.top;
  const factor = ev.deltaY < 0 ? 1.12 : 0.89;
  const nextScale = Math.min(4.0, Math.max(0.2, camScale * factor));
  camX = mx - (mx - camX) * (nextScale / camScale);
  camY = my - (my - camY) * (nextScale / camScale);
  camScale = nextScale;
  renderCanvas();
}, { passive: false });

function selectNodeInSidebar(n) {
  selectedNode = n;
  const incident = graphEdges.filter(e => e.source.id === n.id || e.target.id === n.id).map(e => ({
    relation: e.relation,
    from: e.source.id,
    to: e.target.id,
    evidence: e.evidence,
    provenance: e.provenance
  }));
  document.getElementById('statDegree').textContent = String(incident.length);
  show('selectedNodeOut', {
    id: n.id,
    title: n.title,
    kind: n.kind,
    pagerank_score: Number(n.pagerank.toFixed(6)),
    permissions: n.permissions,
    properties: n.properties,
    incident_edges: incident
  });
  if (n.kind === 'document' || n.kind === 'doc') {
    document.getElementById('inspectDocID').value = n.id;
  }
  document.getElementById('inspectNodeID').value = n.id;
  renderCanvas();
}

// --- Graph Loading & Folder Dry-Run Actions ---
async function loadLiveGraph() {
  try {
    const snap = await apiCall('/api/v1/introspect/graph?max_nodes=400');
    setGraphData(snap.nodes || [], snap.edges || [], snap.pagerank_scores || {}, 'Live DB', false);
    show('selectedNodeOut', {
      status: 'Loaded live Context Graph from SQLite + CSR',
      stats: snap.stats
    });
    await loadOverview();
  } catch (e) {
    show('selectedNodeOut', { error: e.message });
  }
}

async function runFolderCrawl(dryRun) {
  const rootPath = document.getElementById('crawlPath').value.trim();
  const perms = document.getElementById('crawlPerms').value.split(',').map(s => s.trim()).filter(Boolean);
  const includeTestGo = document.getElementById('crawlTests').checked;
  if (!rootPath) {
    show('selectedNodeOut', { error: 'Please enter a folder path to crawl.' });
    return;
  }
  show('selectedNodeOut', { status: dryRun ? 'Running in-memory Dry Run on folder...' : 'Indexing folder into Lattice...', root_path: rootPath });
  try {
    const rep = await apiCall('/api/v1/admin/crawl', 'POST', {
      root_path: rootPath,
      dry_run: dryRun,
      include_test_go: includeTestGo,
      permissions: perms.length ? perms : ['*'],
      max_files: 300
    });
    const sg = rep.subgraph || { nodes: [], edges: [] };
    setGraphData(
      sg.nodes || [],
      sg.edges || [],
      rep.pagerank_scores || {},
      dryRun ? 'Dry-Run Preview' : 'Indexed Folder',
      dryRun
    );
    show('selectedNodeOut', {
      mode: dryRun ? 'DRY_RUN_PREVIEW (Zero DB writes)' : 'LIVE_COMMIT (Persisted to SQLite + HNSW + CSR)',
      root_path: rep.root_path,
      duration_ms: rep.duration_ms,
      files_scanned: rep.files_scanned,
      files_matched: rep.files_matched,
      chunks_generated: rep.chunks_generated,
      symbols_extracted: rep.symbols_extracted,
      nodes_by_kind: rep.nodes_by_kind,
      edges_by_relation: rep.edges_by_relation,
      sample_files: (rep.files || []).slice(0, 15)
    });
    if (!dryRun) {
      await loadOverview();
    }
  } catch (e) {
    show('selectedNodeOut', { error: e.message });
  }
}

// --- Control Plane Card Actions ---
async function loadOverview() {
  try { show('overviewOut', await apiCall('/api/v1/introspect/overview')); }
  catch (e) { show('overviewOut', { error: e.message }); }
}
async function runValidate() {
  try { show('validateOut', await apiCall('/api/v1/validate', 'POST', {})); }
  catch (e) { show('validateOut', { error: e.message }); }
}
async function triggerEnrich() {
  try {
    show('overviewOut', await apiCall('/api/v1/admin/enrich-sweep', 'POST', { limit: 50 }));
    await loadLiveGraph();
  } catch (e) { show('overviewOut', { error: e.message }); }
}
async function triggerRebuild() {
  try {
    show('overviewOut', await apiCall('/api/v1/admin/rebuild', 'POST', {}));
    await loadLiveGraph();
  } catch (e) { show('overviewOut', { error: e.message }); }
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
  try {
    show('inspectOut', await apiCall('/api/v1/admin/documents?id=' + id, 'DELETE'));
    await loadLiveGraph();
  } catch (e) { show('inspectOut', { error: e.message }); }
}
async function inspectNode() {
  const id = encodeURIComponent(document.getElementById('inspectNodeID').value.trim());
  const hops = encodeURIComponent(document.getElementById('inspectHops').value || '2');
  try {
    const rep = await apiCall('/api/v1/introspect/node?id=' + id + '&hops=' + hops);
    show('inspectOut', rep);
    if (rep.subgraph && rep.subgraph.nodes) {
      setGraphData(rep.subgraph.nodes, rep.subgraph.edges || [], {}, 'Subgraph (' + decodeURIComponent(id) + ')', false);
    }
  } catch (e) { show('inspectOut', { error: e.message }); }
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

resizeCanvas();
loadLiveGraph();
</script>
</body>
</html>
`
