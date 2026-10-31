package server

const consoleHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="color-scheme" content="light dark">
<script>
try {
  const saved = localStorage.getItem("color-scheme");
  if (saved === "light" || saved === "dark") {
    document.querySelector('meta[name="color-scheme"]').content = saved;
  }
} catch (_) {}
</script>
<title>Lattice — Context Graph Workbench</title>
<style>
  :root {
    color-scheme: light dark;
    --bg-canvas: light-dark(#fafafa, #09090b);
    --bg-surface: light-dark(#ffffff, #121215);
    --bg-elevated: light-dark(#f4f4f5, #18181b);
    --bg-subtle: light-dark(rgba(244, 244, 245, 0.7), rgba(24, 24, 27, 0.65));
    --border: light-dark(#e4e4e7, rgba(39, 39, 42, 0.85));
    --border-strong: light-dark(#d4d4d8, #3f3f46);
    --text-primary: light-dark(#09090b, #fafafa);
    --text-secondary: light-dark(#52525b, #a1a1aa);
    --text-muted: light-dark(#71717a, #71717a);
    --btn-primary-bg: light-dark(#18181b, #f4f4f5);
    --btn-primary-fg: light-dark(#ffffff, #09090b);
    --btn-primary-hover: light-dark(#27272a, #ffffff);
    --ring: rgba(161, 161, 170, 0.35);
    --ok-fg: light-dark(#15803d, #4ade80);
    --ok-bg: light-dark(rgba(22, 163, 74, 0.08), rgba(34, 197, 94, 0.12));
    --ok-border: light-dark(rgba(22, 163, 74, 0.25), rgba(34, 197, 94, 0.3));
    --warn-fg: light-dark(#b45309, #fbbf24);
    --warn-bg: light-dark(rgba(245, 158, 11, 0.1), rgba(245, 158, 11, 0.14));
    --warn-border: light-dark(rgba(245, 158, 11, 0.3), rgba(245, 158, 11, 0.35));
    --err-fg: light-dark(#b91c1c, #f87171);
    --err-bg: light-dark(rgba(239, 68, 68, 0.08), rgba(239, 68, 68, 0.14));
    --err-border: light-dark(rgba(239, 68, 68, 0.25), rgba(239, 68, 68, 0.35));
    --font-sans: "Inter", "Geist", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    --font-mono: "Geist Mono", "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  :root:has(> head > meta[name="color-scheme"][content="dark"]) {
    color-scheme: dark;
  }
  :root:has(> head > meta[name="color-scheme"][content="light"]) {
    color-scheme: light;
  }
  @media (forced-colors: active) {
    :root {
      --border: CanvasText;
      --border-strong: Highlight;
    }
  }

  * { box-sizing: border-box; }
  html, body {
    margin: 0;
    padding: 0;
    background: var(--bg-canvas);
    color: var(--text-primary);
    font-family: var(--font-sans);
    font-size: 14px;
    line-height: 1.55;
    -webkit-font-smoothing: antialiased;
  }
  .tabular { font-variant-numeric: tabular-nums; }
  .mono { font-family: var(--font-mono); }
  .truncate { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Top Navigation & Telemetry Header */
  .topbar {
    position: sticky;
    top: 0;
    z-index: 30;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.625rem 1.25rem;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.625rem;
  }
  .brand-mark {
    width: 24px;
    height: 24px;
    border-radius: 6px;
    background: var(--btn-primary-bg);
    color: var(--btn-primary-fg);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .brand-title {
    font-size: 0.95rem;
    font-weight: 600;
    letter-spacing: -0.025em;
    margin: 0;
  }
  .brand-sep {
    color: var(--border-strong);
    font-weight: 300;
  }
  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.15rem 0.55rem;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 500;
    letter-spacing: -0.01em;
    border: 1px solid var(--ok-border);
    background: var(--ok-bg);
    color: var(--ok-fg);
  }
  .status-pill.dryrun {
    border-color: var(--warn-border);
    background: var(--warn-bg);
    color: var(--warn-fg);
  }

  /* Telemetry KPI Strip */
  .kpi-strip {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.2rem 0.35rem;
  }
  .kpi-item {
    display: flex;
    align-items: baseline;
    gap: 0.35rem;
    padding: 0.15rem 0.5rem;
    border-right: 1px solid var(--border);
  }
  .kpi-item:last-child { border-right: none; }
  .kpi-label {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
  }
  .kpi-val {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    flex-wrap: wrap;
  }

  /* Controls: Inputs & Buttons */
  input, select, button {
    font-family: inherit;
    font-size: 13px;
    line-height: 1.25;
    border-radius: 6px;
    transition: background-color 120ms ease, border-color 120ms ease, color 120ms ease, opacity 120ms ease;
  }
  input[type="text"], input[type="password"], input[type="number"], select {
    background: var(--bg-canvas);
    color: var(--text-primary);
    border: 1px solid var(--border);
    padding: 0.375rem 0.625rem;
    height: 30px;
  }
  input::placeholder { color: var(--text-muted); }
  input:hover, select:hover { border-color: var(--border-strong); }
  input:focus-visible, select:focus-visible, button:focus-visible {
    outline: none;
    border-color: var(--text-secondary);
    box-shadow: 0 0 0 2px var(--ring);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.375rem;
    height: 30px;
    padding: 0 0.68rem;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid var(--border);
    background: var(--bg-surface);
    color: var(--text-primary);
    cursor: pointer;
    user-select: none;
    white-space: nowrap;
  }
  .btn:hover:not(:disabled) {
    background: var(--bg-elevated);
    border-color: var(--border-strong);
  }
  .btn:active:not(:disabled) {
    transform: translateY(0.5px);
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .btn-primary {
    background: var(--btn-primary-bg);
    color: var(--btn-primary-fg);
    border-color: transparent;
    font-weight: 600;
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--btn-primary-hover);
    border-color: transparent;
  }
  .btn-subtle {
    background: var(--bg-elevated);
    border-color: var(--border);
  }
  .btn-danger {
    color: var(--err-fg);
    border-color: var(--err-border);
    background: var(--err-bg);
  }
  .btn-danger:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.22);
    border-color: var(--err-fg);
  }
  .btn-icon {
    width: 28px;
    height: 28px;
    padding: 0;
  }

  /* Layout Shell */
  .shell {
    max-width: 1600px;
    margin: 0 auto;
    padding: 0.875rem 1.25rem 2.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.875rem;
  }

  /* Command Strip: Folder Crawler & Dry-Run */
  .command-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    padding: 0.55rem 0.75rem;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .input-group {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0 0.5rem;
    height: 30px;
  }
  .input-group:focus-within {
    border-color: var(--text-secondary);
    box-shadow: 0 0 0 2px var(--ring);
  }
  .input-group input {
    border: none;
    background: transparent;
    padding: 0;
    height: 28px;
  }
  .input-group input:focus-visible {
    box-shadow: none;
  }
  .field-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-muted);
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .checkbox-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.375rem;
    font-size: 12px;
    font-weight: 500;
    color: var(--text-secondary);
    padding: 0 0.55rem;
    height: 30px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-canvas);
    cursor: pointer;
    user-select: none;
  }
  .checkbox-pill:hover { border-color: var(--border-strong); color: var(--text-primary); }

  /* Upper Workbench: Split Graph Canvas + Inspector Drawer */
  .workbench {
    display: grid;
    grid-template-columns: 1fr 400px;
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
    min-height: 540px;
  }
  @media (max-width: 1100px) {
    .workbench { grid-template-columns: 1fr; }
  }

  .graph-stage {
    position: relative;
    display: flex;
    flex-direction: column;
    background: var(--bg-canvas);
    min-height: 540px;
  }
  .canvas-header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
  }
  .segmented {
    display: inline-flex;
    align-items: center;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 2px;
    gap: 2px;
  }
  .seg-btn {
    border: none;
    background: transparent;
    color: var(--text-secondary);
    height: 24px;
    padding: 0 0.5rem;
    border-radius: 4px;
    font-size: 11.5px;
    font-weight: 500;
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    cursor: pointer;
  }
  .seg-btn:hover { color: var(--text-primary); }
  .seg-btn.active {
    background: var(--bg-surface);
    color: var(--text-primary);
    border: 1px solid var(--border);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 999px;
    display: inline-block;
  }
  .count-chip {
    font-size: 10.5px;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .canvas-viewport {
    position: relative;
    flex: 1;
    min-height: 490px;
    overflow: hidden;
  }
  canvas#graphCanvas {
    width: 100%;
    height: 490px;
    display: block;
    cursor: grab;
  }
  canvas#graphCanvas:active { cursor: grabbing; }

  .canvas-footer {
    position: absolute;
    bottom: 10px;
    left: 12px;
    right: 12px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    pointer-events: none;
  }
  .hint-pill {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.25rem 0.55rem;
    font-size: 11px;
    color: var(--text-muted);
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }

  /* Empty / Loading / Error Overlays */
  .state-overlay {
    position: absolute;
    inset: 0;
    display: none;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    background: var(--bg-canvas);
    z-index: 10;
    padding: 1.5rem;
    text-align: center;
  }
  .state-overlay.visible { display: flex; }
  .skeleton-bar {
    height: 10px;
    border-radius: 4px;
    background: linear-gradient(90deg, var(--bg-elevated) 25%, var(--border) 50%, var(--bg-elevated) 75%);
    background-size: 200% 100%;
    animation: shimmer 1.4s infinite;
  }
  @keyframes shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  /* Right Drawer: Vertex & Crawl Inspector */
  .inspector-drawer {
    border-left: 1px solid var(--border);
    background: var(--bg-surface);
    display: flex;
    flex-direction: column;
    max-height: 540px;
  }
  .drawer-header {
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .drawer-body {
    padding: 0.75rem;
    overflow-y: auto;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  /* Structured Data Components */
  .kv-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 0.4rem;
  }
  .kv-box {
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.4rem 0.55rem;
  }
  .kv-box .label {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
  }
  .kv-box .value {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
    margin-top: 1px;
  }

  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .data-table th {
    text-align: left;
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    padding: 0.35rem 0.45rem;
    border-bottom: 1px solid var(--border);
  }
  .data-table td {
    padding: 0.38rem 0.45rem;
    border-bottom: 1px solid var(--border);
    color: var(--text-primary);
    vertical-align: middle;
  }
  .data-table tr.clickable-row { cursor: pointer; }
  .data-table tr.clickable-row:hover td { background: var(--bg-elevated); }

  .tag {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.1rem 0.42rem;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
    font-family: var(--font-mono);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-secondary);
  }
  .tag-ok { color: var(--ok-fg); background: var(--ok-bg); border-color: var(--ok-border); }
  .tag-warn { color: var(--warn-fg); background: var(--warn-bg); border-color: var(--warn-border); }
  .tag-err { color: var(--err-fg); background: var(--err-bg); border-color: var(--err-border); }

  /* Lower Bento Grid */
  .bento-grid {
    display: grid;
    grid-template-columns: repeat(12, 1fr);
    gap: 0.875rem;
  }
  .bento-col-6 { grid-column: span 6; }
  @media (max-width: 1100px) {
    .bento-col-6 { grid-column: span 12; }
  }
  .bento-card {
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .card-head {
    padding: 0.65rem 0.875rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .card-title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    letter-spacing: -0.02em;
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
  }
  .card-controls {
    padding: 0.65rem 0.875rem;
    background: var(--bg-subtle);
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.45rem;
  }
  .card-controls input { flex: 1; min-width: 115px; }
  .card-body {
    padding: 0.75rem 0.875rem;
    max-height: 310px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }

  /* Search Hit & Path Visualizers */
  .hit-item {
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 0.6rem 0.7rem;
    background: var(--bg-canvas);
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .score-bars {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0.4rem;
    margin-top: 0.15rem;
  }
  .score-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .score-cell .s-head {
    display: flex;
    justify-content: space-between;
    font-size: 10.5px;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .bar-track {
    height: 4px;
    background: var(--bg-elevated);
    border-radius: 99px;
    overflow: hidden;
  }
  .bar-fill {
    height: 100%;
    background: var(--text-primary);
    border-radius: 99px;
  }

  .hop-chain {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
    padding: 0.5rem;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  pre.json-block {
    margin: 0;
    padding: 0.65rem;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-family: var(--font-mono);
    font-size: 11.5px;
    line-height: 1.45;
    color: var(--text-secondary);
    overflow-x: auto;
  }
  .banner {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.65rem;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 500;
  }
  .banner.ok { background: var(--ok-bg); border: 1px solid var(--ok-border); color: var(--ok-fg); }
  .banner.warn { background: var(--warn-bg); border: 1px solid var(--warn-border); color: var(--warn-fg); }
  .banner.err { background: var(--err-bg); border: 1px solid var(--err-border); color: var(--err-fg); }
</style>
</head>
<body>

<!-- Top Header Bar -->
<header class="topbar">
  <div class="brand">
    <span class="brand-mark" aria-hidden="true">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="3"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="18" r="3"/><line x1="8.5" y1="7.5" x2="15.5" y2="16.5"/><line x1="8.5" y1="6" x2="15.5" y2="6"/><line x1="6" y1="8.5" x2="6" y2="15.5"/></svg>
    </span>
    <h1 class="brand-title">Lattice</h1>
    <span class="brand-sep">/</span>
    <span style="font-size:12px;color:var(--text-secondary);font-weight:500">Context Graph Engine</span>
    <span id="graphModeBadge" class="status-pill">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
      <span id="graphModeText">Live Stored Graph</span>
    </span>
  </div>

  <!-- Live Telemetry Strip (tabular-nums) -->
  <div class="kpi-strip" aria-label="Engine Telemetry">
    <div class="kpi-item"><span class="kpi-label">Nodes</span><span class="kpi-val" id="kpiNodes">0</span></div>
    <div class="kpi-item"><span class="kpi-label">Edges</span><span class="kpi-val" id="kpiEdges">0</span></div>
    <div class="kpi-item"><span class="kpi-label">Vectors</span><span class="kpi-val" id="kpiVectors">0</span></div>
    <div class="kpi-item"><span class="kpi-label">Dim</span><span class="kpi-val" id="kpiDim">384</span></div>
    <div class="kpi-item"><span class="kpi-label">Queue</span><span class="kpi-val" id="kpiPending">0</span></div>
  </div>

  <div class="header-actions">
    <div class="input-group" title="Optional Bearer Token for authenticated API calls">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
      <input id="adminToken" type="password" placeholder="Bearer token" style="width:110px" />
    </div>
    <button class="btn" onclick="loadLiveGraph()" title="Reload live CSR + SQLite graph">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
      Sync DB
    </button>
    <button class="btn" onclick="cycleTheme()" id="themeBtn" title="Switch color scheme (System / Dark / Light)">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
      <span id="themeLabel">Theme</span>
    </button>
  </div>
</header>

<main class="shell">
  <!-- Folder Crawler & Dry-Run Command Bar -->
  <section class="command-bar" aria-label="Folder Graph Generator">
    <span class="field-label">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
      Folder
    </span>
    <select id="crawlPreset" onchange="applyCrawlPreset(this.value)" title="Quick Folder Presets" style="max-width:145px">
      <option value="/Users/rohitgupta/Documents/glean-lite/docs">Preset: /docs (KB)</option>
      <option value="/Users/rohitgupta/Documents/glean-lite">Preset: Workspace</option>
      <option value="/Users/rohitgupta/Documents/glean-lite/lattice">Preset: /lattice</option>
    </select>
    <input id="crawlPath" type="text" class="mono" style="flex:1;min-width:220px" value="/Users/rohitgupta/Documents/glean-lite/docs" placeholder="/path/to/docs/or/repository" />

    <div class="segmented" role="group" aria-label="Crawl Scope">
      <button class="seg-btn active" id="scopeBtnDocs" onclick="setCrawlScope('docs')">Docs Only</button>
      <button class="seg-btn" id="scopeBtnAll" onclick="setCrawlScope('all')">Docs + Code</button>
      <button class="seg-btn" id="scopeBtnCode" onclick="setCrawlScope('code')">Code Only</button>
    </div>

    <span class="field-label">ACL</span>
    <input id="crawlPerms" type="text" class="mono" style="width:90px" value="*" placeholder="*,group:eng" />

    <button id="btnDryRun" class="btn btn-subtle" onclick="runFolderCrawl(true)">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
      Dry-Run Preview
    </button>
    <button id="btnIndexFolder" class="btn btn-primary" onclick="runFolderCrawl(false)">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
      Index Folder
    </button>
  </section>

  <!-- Main Interactive Graph Workbench -->
  <section class="workbench">
    <!-- Left: Interactive Force-Directed Graph Stage -->
    <div class="graph-stage">
      <div class="canvas-header">
        <!-- Node Kind Segmented Filter -->
        <div class="segmented" role="tablist" aria-label="Filter by Node Kind">
          <button class="seg-btn active" data-kind="all" onclick="setKindFilter('all')">
            All <span class="count-chip" id="cntAll">0</span>
          </button>
          <button class="seg-btn" data-kind="document" onclick="setKindFilter('document')">
            <span class="dot" style="background:#38bdf8"></span>Doc <span class="count-chip" id="cntDoc">0</span>
          </button>
          <button class="seg-btn" data-kind="topic" onclick="setKindFilter('topic')">
            <span class="dot" style="background:#f97316"></span>Topic <span class="count-chip" id="cntTop">0</span>
          </button>
          <button class="seg-btn" data-kind="decision" onclick="setKindFilter('decision')">
            <span class="dot" style="background:#ec4899"></span>Decision/Svc <span class="count-chip" id="cntDec">0</span>
          </button>
          <button class="seg-btn" data-kind="person" onclick="setKindFilter('person')">
            <span class="dot" style="background:#22c55e"></span>Person <span class="count-chip" id="cntPer">0</span>
          </button>
          <button class="seg-btn" data-kind="ticket" onclick="setKindFilter('ticket')">
            <span class="dot" style="background:#eab308"></span>Ticket <span class="count-chip" id="cntTkt">0</span>
          </button>
          <button class="seg-btn" data-kind="symbol" onclick="setKindFilter('symbol')">
            <span class="dot" style="background:#a855f7"></span>Symbol <span class="count-chip" id="cntSym">0</span>
          </button>
        </div>

        <!-- Search-in-Graph & Camera Toolbar -->
        <div style="display:flex;align-items:center;gap:0.375rem">
          <div class="input-group">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input id="graphFilterText" type="text" placeholder="Filter nodes (⌘K)..." oninput="renderCanvas()" style="width:155px" />
          </div>
          <button class="btn btn-icon" onclick="zoomBy(1.25)" title="Zoom In" aria-label="Zoom In">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          </button>
          <button class="btn btn-icon" onclick="zoomBy(0.8)" title="Zoom Out" aria-label="Zoom Out">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/></svg>
          </button>
          <button class="btn btn-icon" onclick="resetCamera()" title="Center Camera" aria-label="Center Camera">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M3 7V5a2 2 0 0 1 2-2h2"/><path d="M17 3h2a2 2 0 0 1 2 2v2"/><path d="M21 17v2a2 2 0 0 1-2 2h-2"/><path d="M7 21H5a2 2 0 0 1-2-2v-2"/></svg>
          </button>
          <button class="btn btn-icon" onclick="reheatSimulation()" title="Re-run Force Layout" aria-label="Re-run Force Layout">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
          </button>
        </div>
      </div>

      <div class="canvas-viewport">
        <canvas id="graphCanvas"></canvas>

        <!-- Loading / Empty Overlay -->
        <div id="canvasOverlayState" class="state-overlay"></div>

        <div class="canvas-footer">
          <span class="hint-pill">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            Drag node to pin · Scroll to zoom · Click vertex to inspect ego-network
          </span>
          <span class="hint-pill tabular" id="canvasStatsPill">0 visible vertices · 0 edges</span>
        </div>
      </div>
    </div>

    <!-- Right: Structured Vertex & Crawl Inspector Drawer -->
    <aside class="inspector-drawer">
      <div class="drawer-header">
        <span style="font-size:12px;font-weight:600;letter-spacing:-0.01em">Inspector</span>
        <div class="segmented">
          <button class="seg-btn active" id="tabBtnVertex" onclick="switchDrawerTab('vertex')">Vertex</button>
          <button class="seg-btn" id="tabBtnCrawl" onclick="switchDrawerTab('crawl')">Crawl Report</button>
          <button class="seg-btn" id="tabBtnRaw" onclick="switchDrawerTab('raw')">JSON</button>
        </div>
      </div>

      <div class="drawer-body" id="drawerContent"></div>
    </aside>
  </section>

  <!-- Lower Bento Grid (4 High-Craft Control Plane Modules) -->
  <section class="bento-grid">
    <!-- Module 1: Hybrid RRF Search Workbench -->
    <article class="bento-card bento-col-6">
      <div class="card-head">
        <h2 class="card-title">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          Hybrid RRF &amp; PageRank Search
        </h2>
        <div style="display:flex;align-items:center;gap:0.45rem">
          <div class="segmented" role="group" aria-label="Search Corpus Filter">
            <button class="seg-btn active" id="srchTypeDocs" onclick="setSearchDocFilter('docs')">Docs &amp; PDFs</button>
            <button class="seg-btn" id="srchTypePDF" onclick="setSearchDocFilter('pdf')">PDF Only</button>
            <button class="seg-btn" id="srchTypeAll" onclick="setSearchDocFilter('all')">All</button>
            <button class="seg-btn" id="srchTypeCode" onclick="setSearchDocFilter('code')">Code</button>
          </div>
          <span class="field-label tabular" id="searchLatencyBadge">BM25 + HNSW + PPR</span>
        </div>
      </div>
      <div class="card-controls">
        <input id="searchQuery" type="text" placeholder="Search across docs, PDFs, RFCs &amp; code..." value="Passkey WebAuthn zero-trust" />
        <input id="searchUser" type="text" class="mono" placeholder="user_id" value="alice" style="max-width:85px" />
        <input id="searchGroups" type="text" class="mono" placeholder="groups" value="group:eng" style="max-width:105px" />
        <button class="btn btn-primary" onclick="runSearch()">Search</button>
      </div>
      <div class="card-body" id="searchOut"></div>
    </article>

    <!-- Module 2: Shortest Path & Zero-Trust ACL Debugger -->
    <article class="bento-card bento-col-6">
      <div class="card-head">
        <h2 class="card-title">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
          Path &amp; Bridge-ACL Debugger
        </h2>
        <div class="segmented">
          <button class="seg-btn active" id="modeBtnPath" onclick="setSecurityTab('path')">Explain Path</button>
          <button class="seg-btn" id="modeBtnACL" onclick="setSecurityTab('acl')">Simulate ACL</button>
        </div>
      </div>

      <!-- Path Controls -->
      <div class="card-controls" id="controlsPath">
        <input id="expFrom" type="text" class="mono" placeholder="From Node" value="person:alice" />
        <input id="expTo" type="text" class="mono" placeholder="To Node" value="doc:board-merger" />
        <input id="expUser" type="text" class="mono" placeholder="User" value="rohit" style="max-width:80px" />
        <input id="expGroups" type="text" class="mono" placeholder="Groups" value="group:eng" style="max-width:105px" />
        <button class="btn btn-primary" onclick="explainPath()">Explain</button>
      </div>

      <!-- ACL Simulator Controls -->
      <div class="card-controls" id="controlsACL" style="display:none">
        <input id="simUser" type="text" class="mono" placeholder="user_id" value="alice" style="max-width:80px" />
        <input id="simEmail" type="text" class="mono" placeholder="email" value="rohit@acme.io" style="max-width:125px" />
        <input id="simGroups" type="text" class="mono" placeholder="groups" value="group:eng" style="max-width:95px" />
        <input id="simTargets" type="text" class="mono" placeholder="target IDs" value="doc:rfc-auth-v2,doc:board-merger" />
        <button class="btn btn-primary" onclick="runACLSim()">Simulate</button>
      </div>

      <div class="card-body" id="securityOut"></div>
    </article>

    <!-- Module 3: Graph & Security Integrity Validator -->
    <article class="bento-card bento-col-6">
      <div class="card-head">
        <h2 class="card-title">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
          Structural &amp; Security Integrity Audit
        </h2>
        <div style="display:flex;gap:0.4rem">
          <button class="btn" onclick="triggerEnrich()">Enrich Sweep</button>
          <button class="btn" onclick="triggerRebuild()">Rebuild CSR/HNSW</button>
          <button class="btn btn-primary" onclick="runValidate()">Run Audit</button>
        </div>
      </div>
      <div class="card-body" id="validateOut"></div>
    </article>

    <!-- Module 4: Document & Ego-Subgraph Inspector -->
    <article class="bento-card bento-col-6">
      <div class="card-head">
        <h2 class="card-title">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
          Document &amp; Subgraph Operations
        </h2>
      </div>
      <div class="card-controls">
        <input id="inspectDocID" type="text" class="mono" placeholder="Document ID" value="doc:rfc-auth-v1" />
        <button class="btn btn-primary" onclick="inspectDoc()">Inspect Doc</button>
        <button class="btn btn-danger" onclick="deleteDoc()">Delete</button>
        <input id="inspectNodeID" type="text" class="mono" placeholder="Node ID" value="person:alice" />
        <input id="inspectHops" type="number" class="tabular" value="2" min="1" max="6" style="max-width:54px" title="Max Hops" />
        <button class="btn" onclick="inspectNode()">Load Ego-Graph</button>
      </div>
      <div class="card-body" id="inspectOut"></div>
    </article>
  </section>
</main>

<script>
// --- Theme Switcher (modern-web-guidance dark-mode-toggle pattern) ---
function getThemeMeta() {
  return document.querySelector('meta[name="color-scheme"]');
}
function updateThemeLabel() {
  const cur = getThemeMeta().content;
  const lbl = document.getElementById('themeLabel');
  if (cur === 'dark') lbl.textContent = 'Dark';
  else if (cur === 'light') lbl.textContent = 'Light';
  else lbl.textContent = 'System';
}
function cycleTheme() {
  const meta = getThemeMeta();
  const cur = meta.content;
  const next = cur === 'light dark' ? 'dark' : (cur === 'dark' ? 'light' : 'light dark');
  meta.content = next;
  try {
    if (next === 'light dark') localStorage.removeItem('color-scheme');
    else localStorage.setItem('color-scheme', next);
  } catch (_) {}
  updateThemeLabel();
  renderCanvas();
}
window.addEventListener('storage', (e) => {
  if (e.key === 'color-scheme') {
    getThemeMeta().content = e.newValue || 'light dark';
    updateThemeLabel();
    renderCanvas();
  }
});
updateThemeLabel();

// Keyboard shortcut Cmd/Ctrl+K to focus graph filter
window.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    document.getElementById('graphFilterText').focus();
  }
});

// --- Safe HTML Escaping & Skeleton/State Renderers ---
function esc(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}
function renderSkeleton(lines = 3) {
  let html = '<div style="display:flex;flex-direction:column;gap:0.5rem;padding:0.25rem 0">';
  const widths = ['92%', '76%', '84%', '60%'];
  for (let i = 0; i < lines; i++) {
    html += '<div class="skeleton-bar" style="width:' + widths[i % widths.length] + '"></div>';
  }
  html += '</div>';
  return html;
}
function renderErrorBanner(msg) {
  return '<div class="banner err">' +
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>' +
    '<span>' + esc(msg) + '</span></div>';
}

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

// --- Interactive Force-Directed Graph Canvas Engine ---
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

// Inspector Drawer & Crawl/Search Filter State
let activeDrawerTab = 'vertex';
let activeCrawlScope = 'docs';
let activeSearchDocFilter = 'docs';
let lastCrawlReport = null;
let lastRawPayload = null;

function applyCrawlPreset(path) {
  if (!path) return;
  document.getElementById('crawlPath').value = path;
  if (path.endsWith('/docs')) {
    setCrawlScope('docs');
  } else {
    setCrawlScope('all');
  }
}

function setCrawlScope(scope) {
  activeCrawlScope = scope;
  document.getElementById('scopeBtnDocs').classList.toggle('active', scope === 'docs');
  document.getElementById('scopeBtnAll').classList.toggle('active', scope === 'all');
  document.getElementById('scopeBtnCode').classList.toggle('active', scope === 'code');
}

function setSearchDocFilter(mode) {
  activeSearchDocFilter = mode;
  document.getElementById('srchTypeDocs').classList.toggle('active', mode === 'docs');
  document.getElementById('srchTypePDF').classList.toggle('active', mode === 'pdf');
  document.getElementById('srchTypeAll').classList.toggle('active', mode === 'all');
  document.getElementById('srchTypeCode').classList.toggle('active', mode === 'code');
  runSearch();
}

function isDarkActive() {
  const m = getThemeMeta().content;
  if (m === 'dark') return true;
  if (m === 'light') return false;
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function colorForKind(kind) {
  switch ((kind || '').toLowerCase()) {
    case 'doc':
    case 'document': return '#38bdf8';
    case 'topic':    return '#f97316';
    case 'decision':
    case 'service':  return '#ec4899';
    case 'symbol':   return '#a855f7';
    case 'person':   return '#22c55e';
    case 'ticket':   return '#eab308';
    default:         return '#71717a';
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

function setCanvasOverlay(html) {
  const el = document.getElementById('canvasOverlayState');
  if (!html) {
    el.className = 'state-overlay';
    el.innerHTML = '';
  } else {
    el.className = 'state-overlay visible';
    el.innerHTML = html;
  }
}

function setGraphData(nodes, edges, pprMap, modeLabel, isDryRun) {
  const rect = canvas.getBoundingClientRect();
  const cx = rect.width / 2 || 480;
  const cy = rect.height / 2 || 245;

  nodeById.clear();
  const maxPPR = Object.values(pprMap || {}).reduce((m, v) => Math.max(m, v || 0), 0.001);

  let cntDoc = 0, cntTop = 0, cntDec = 0, cntSym = 0, cntPer = 0, cntTkt = 0;

  graphNodes = (nodes || []).map((n, idx) => {
    const kind = (n.kind || 'other').toLowerCase();
    if (kind === 'doc' || kind === 'document') cntDoc++;
    else if (kind === 'topic') cntTop++;
    else if (kind === 'decision' || kind === 'service') cntDec++;
    else if (kind === 'symbol') cntSym++;
    else if (kind === 'person') cntPer++;
    else if (kind === 'ticket') cntTkt++;

    const angle = (2 * Math.PI * idx) / Math.max(1, nodes.length);
    const radiusSpread = Math.min(cx, cy) * 0.68 * (0.3 + 0.7 * ((idx % 9) / 8));
    const score = (pprMap && pprMap[n.id]) ? pprMap[n.id] : 0;
    const normScore = score / maxPPR;
    const r = 6.5 + Math.min(10.5, Math.sqrt(normScore) * 9.5);
    const item = {
      id: n.id,
      title: n.title || n.id,
      kind,
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

  document.getElementById('cntAll').textContent = String(graphNodes.length);
  document.getElementById('cntDoc').textContent = String(cntDoc);
  document.getElementById('cntTop').textContent = String(cntTop);
  document.getElementById('cntDec').textContent = String(cntDec);
  document.getElementById('cntSym').textContent = String(cntSym);
  document.getElementById('cntPer').textContent = String(cntPer);
  document.getElementById('cntTkt').textContent = String(cntTkt);

  const badge = document.getElementById('graphModeBadge');
  const badgeText = document.getElementById('graphModeText');
  badge.className = isDryRun ? 'status-pill dryrun' : 'status-pill';
  badgeText.textContent = modeLabel;

  if (graphNodes.length === 0) {
    setCanvasOverlay(
      '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="color:var(--text-muted)"><circle cx="12" cy="12" r="10"/><line x1="8" y1="12" x2="16" y2="12"/></svg>' +
      '<div style="font-weight:600;font-size:13px">No Graph Vertices Found</div>' +
      '<div style="font-size:12px;color:var(--text-muted);max-width:320px">Click "Dry-Run Preview" in the Folder Path bar above to generate a Context Graph from any local directory.</div>'
    );
  } else {
    setCanvasOverlay('');
  }

  selectedNode = graphNodes[0] || null;
  resetCamera();
  reheatSimulation();
  renderDrawer();
}

function getVisibleNodes() {
  if (activeKindFilter === 'all') return graphNodes;
  return graphNodes.filter(n => {
    if (activeKindFilter === 'document') {
      return n.kind === 'doc' || n.kind === 'document';
    }
    if (activeKindFilter === 'decision') {
      return n.kind === 'decision' || n.kind === 'service';
    }
    return n.kind === activeKindFilter;
  });
}

function setKindFilter(kind) {
  activeKindFilter = kind;
  document.querySelectorAll('.canvas-header .seg-btn').forEach(btn => {
    btn.classList.toggle('active', btn.getAttribute('data-kind') === kind);
  });
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
  const cy = rect.height / 2 || 245;

  if (simAlpha > 0.01 && visibleNodes.length > 0) {
    const repulsion = 2400;
    for (let i = 0; i < visibleNodes.length; i++) {
      const a = visibleNodes[i];
      for (let j = i + 1; j < visibleNodes.length; j++) {
        const b = visibleNodes[j];
        let dx = b.x - a.x;
        let dy = b.y - a.y;
        let distSq = dx * dx + dy * dy + 16;
        if (distSq > 85000) continue;
        let dist = Math.sqrt(distSq);
        let force = (repulsion / distSq) * simAlpha;
        let fx = (dx / dist) * force;
        let fy = (dy / dist) * force;
        if (!a.pinned) { a.vx -= fx; a.vy -= fy; }
        if (!b.pinned) { b.vx += fx; b.vy += fy; }
      }
    }

    const targetLen = visibleNodes.length > 60 ? 90 : 120;
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

    for (const n of visibleNodes) {
      if (n.pinned) continue;
      n.vx += (cx - n.x) * 0.0085 * simAlpha;
      n.vy += (cy - n.y) * 0.0085 * simAlpha;
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
  return { x: (sx - camX) / camScale, y: (sy - camY) / camScale };
}

function renderCanvas() {
  const rect = canvas.getBoundingClientRect();
  ctx.clearRect(0, 0, rect.width, rect.height);

  const dark = isDarkActive();
  const visibleNodes = getVisibleNodes();
  const visibleSet = new Set(visibleNodes.map(n => n.id));
  const visibleEdges = graphEdges.filter(e => visibleSet.has(e.source.id) && visibleSet.has(e.target.id));
  const searchSub = (document.getElementById('graphFilterText').value || '').trim().toLowerCase();

  document.getElementById('canvasStatsPill').textContent =
    visibleNodes.length + ' vertices · ' + visibleEdges.length + ' edges';

  // Subtle dot matrix grid on canvas
  ctx.save();
  ctx.fillStyle = dark ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.05)';
  const step = 28;
  for (let gx = step / 2; gx < rect.width; gx += step) {
    for (let gy = step / 2; gy < rect.height; gy += step) {
      ctx.fillRect(gx, gy, 1, 1);
    }
  }
  ctx.restore();

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

    ctx.strokeStyle = isHighlighted
      ? (dark ? '#e4e4e7' : '#18181b')
      : (isDimmed
          ? (dark ? 'rgba(63, 63, 70, 0.22)' : 'rgba(212, 212, 216, 0.35)')
          : (dark ? 'rgba(113, 113, 122, 0.45)' : 'rgba(161, 161, 170, 0.55)'));
    ctx.lineWidth = isHighlighted ? 1.65 : 1.0;

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

    const arrowLen = 5.5;
    const angle = Math.atan2(dy, dx);
    ctx.fillStyle = ctx.strokeStyle;
    ctx.beginPath();
    ctx.moveTo(endX + ux * 3.5, endY + uy * 3.5);
    ctx.lineTo(endX - arrowLen * Math.cos(angle - Math.PI / 6), endY - arrowLen * Math.sin(angle - Math.PI / 6));
    ctx.lineTo(endX - arrowLen * Math.cos(angle + Math.PI / 6), endY - arrowLen * Math.sin(angle + Math.PI / 6));
    ctx.closePath();
    ctx.fill();

    if (isHighlighted || (camScale > 1.4 && visibleEdges.length < 55)) {
      const mx = (startX + endX) / 2;
      const my = (startY + endY) / 2;
      ctx.font = '10px "Geist Mono", ui-monospace, monospace';
      ctx.fillStyle = isHighlighted ? (dark ? '#fafafa' : '#09090b') : '#71717a';
      ctx.textAlign = 'center';
      ctx.fillText(e.relation, mx, my - 4);
    }
  }

  // Draw vertices
  for (const n of visibleNodes) {
    const matchesSearch = searchSub && (n.id.toLowerCase().includes(searchSub) || n.title.toLowerCase().includes(searchSub));
    const isDimmed = (selectedNode && !neighborSet.has(n.id)) || (searchSub && !matchesSearch);

    ctx.save();
    ctx.globalAlpha = isDimmed ? 0.22 : 1.0;

    if (n === selectedNode || n === hoveredNode || matchesSearch) {
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.r + 4.5, 0, 2 * Math.PI);
      ctx.strokeStyle = matchesSearch ? '#eab308' : (dark ? 'rgba(244, 244, 245, 0.5)' : 'rgba(24, 24, 27, 0.35)');
      ctx.lineWidth = 1.5;
      ctx.stroke();
    }

    ctx.beginPath();
    ctx.arc(n.x, n.y, n.r, 0, 2 * Math.PI);
    ctx.fillStyle = colorForKind(n.kind);
    ctx.fill();
    ctx.lineWidth = (n === selectedNode) ? 2.0 : 1.0;
    ctx.strokeStyle = (n === selectedNode) ? (dark ? '#ffffff' : '#09090b') : (dark ? '#09090b' : '#ffffff');
    ctx.stroke();

    if (!isDimmed || n === selectedNode) {
      const shortLabel = n.title.length > 26 ? n.title.slice(0, 24) + '…' : n.title;
      ctx.font = (n === selectedNode ? '600 ' : '500 ') + '11px Inter, -apple-system, sans-serif';
      ctx.fillStyle = dark ? '#e4e4e7' : '#18181b';
      ctx.textAlign = 'center';
      ctx.fillText(shortLabel, n.x, n.y + n.r + 12);
    }
    ctx.restore();
  }

  ctx.restore();
}

// --- Canvas Pointer Events ---
function findNodeAt(sx, sy) {
  const pt = screenToWorld(sx, sy);
  const visibleNodes = getVisibleNodes();
  for (let i = visibleNodes.length - 1; i >= 0; i--) {
    const n = visibleNodes[i];
    const dx = pt.x - n.x;
    const dy = pt.y - n.y;
    if (dx * dx + dy * dy <= (n.r + 4) * (n.r + 4)) return n;
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
    selectNodeById(hit.id);
  } else {
    isPanning = true;
    panStartX = sx - camX;
    panStartY = sy - camY;
    selectedNode = null;
    renderCanvas();
    renderDrawer();
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

function selectNodeById(id) {
  const n = nodeById.get(id);
  if (!n) return;
  selectedNode = n;
  if (n.kind === 'doc' || n.kind === 'document') {
    document.getElementById('inspectDocID').value = n.id;
  }
  document.getElementById('inspectNodeID').value = n.id;
  if (activeDrawerTab === 'crawl') {
    switchDrawerTab('vertex');
  } else {
    renderDrawer();
  }
  renderCanvas();
}

// --- Inspector Drawer Rendering ---
function switchDrawerTab(tab) {
  activeDrawerTab = tab;
  document.getElementById('tabBtnVertex').classList.toggle('active', tab === 'vertex');
  document.getElementById('tabBtnCrawl').classList.toggle('active', tab === 'crawl');
  document.getElementById('tabBtnRaw').classList.toggle('active', tab === 'raw');
  renderDrawer();
}

function renderDrawer() {
  const container = document.getElementById('drawerContent');
  if (activeDrawerTab === 'raw') {
    const payload = selectedNode || lastCrawlReport || lastRawPayload || { info: 'Select a vertex or run a crawl to inspect JSON.' };
    container.innerHTML = '<pre class="json-block">' + esc(JSON.stringify(payload, null, 2)) + '</pre>';
    return;
  }

  if (activeDrawerTab === 'crawl') {
    if (!lastCrawlReport) {
      container.innerHTML =
        '<div style="color:var(--text-muted);font-size:12px;padding:1rem 0;text-align:center">' +
        'No folder crawl run in this session yet. Click <strong>Dry-Run Preview</strong> or <strong>Index Folder</strong> above.' +
        '</div>';
      return;
    }
    const r = lastCrawlReport;
    const filesRows = (r.files || []).slice(0, 40).map(f =>
      '<tr class="clickable-row" onclick="selectNodeById(\'' + esc(f.doc_id) + '\')">' +
      '<td class="mono truncate" style="max-width:165px" title="' + esc(f.rel_path) + '">' + esc(f.rel_path) + '</td>' +
      '<td><span class="tag">' + esc(f.doc_type) + '</span></td>' +
      '<td class="tabular">' + f.symbols_count + '</td>' +
      '<td class="tabular">' + f.edges_extracted + '</td>' +
      '</tr>'
    ).join('');

    container.innerHTML =
      '<div class="banner ' + (r.dry_run ? 'warn' : 'ok') + '">' +
        '<span>' + (r.dry_run ? 'Dry-Run Preview (Zero SQLite writes)' : 'Live Indexed into Lattice DB') + ' · ' + r.duration_ms.toFixed(1) + ' ms</span>' +
      '</div>' +
      '<div class="kv-grid">' +
        '<div class="kv-box"><div class="label">Files Matched / Scanned</div><div class="value">' + r.files_matched + ' / ' + r.files_scanned + '</div></div>' +
        '<div class="kv-box"><div class="label">AST Symbols</div><div class="value">' + r.symbols_extracted + '</div></div>' +
        '<div class="kv-box"><div class="label">Subgraph Nodes</div><div class="value">' + ((r.subgraph && r.subgraph.nodes) ? r.subgraph.nodes.length : 0) + '</div></div>' +
        '<div class="kv-box"><div class="label">Subgraph Edges</div><div class="value">' + ((r.subgraph && r.subgraph.edges) ? r.subgraph.edges.length : 0) + '</div></div>' +
      '</div>' +
      '<div style="font-size:11px;font-weight:500;color:var(--text-muted)">Extracted Files (click row to focus vertex)</div>' +
      '<table class="data-table"><thead><tr><th>File</th><th>Type</th><th>Syms</th><th>Edges</th></tr></thead><tbody>' +
        filesRows +
      '</tbody></table>';
    return;
  }

  // Default: 'vertex' tab
  if (!selectedNode) {
    container.innerHTML =
      '<div style="color:var(--text-muted);font-size:12px;padding:1.25rem 0;text-align:center">' +
      'Click any vertex in the graph canvas to inspect its ACL permissions, PageRank centrality, properties, and incident edges.' +
      '</div>';
    return;
  }

  const n = selectedNode;
  const incident = graphEdges.filter(e => e.source.id === n.id || e.target.id === n.id);
  const permsHtml = (n.permissions && n.permissions.length)
    ? n.permissions.map(p => '<span class="tag tag-ok">' + esc(p) + '</span>').join(' ')
    : '<span class="tag tag-err">fail-closed (none)</span>';

  const propEntries = Object.entries(n.properties || {});
  const propsHtml = propEntries.length
    ? '<table class="data-table"><tbody>' +
      propEntries.map(([k, v]) => '<tr><td style="color:var(--text-muted);width:36%">' + esc(k) + '</td><td class="mono" style="word-break:break-all">' + esc(v) + '</td></tr>').join('') +
      '</tbody></table>'
    : '<div style="font-size:12px;color:var(--text-muted)">No custom vertex properties.</div>';

  const edgesHtml = incident.slice(0, 35).map(e => {
    const isOut = e.source.id === n.id;
    const otherId = isOut ? e.target.id : e.source.id;
    const dirArrow = isOut ? '→' : '←';
    return '<tr class="clickable-row" onclick="selectNodeById(\'' + esc(otherId) + '\')">' +
      '<td><span class="tag">' + esc(e.relation) + '</span></td>' +
      '<td class="mono">' + dirArrow + ' ' + esc(otherId) + '</td>' +
      '</tr>';
  }).join('');

  container.innerHTML =
    '<div style="display:flex;align-items:center;justify-content:space-between;gap:0.5rem">' +
      '<span class="mono" style="font-weight:600;font-size:12.5px;word-break:break-all">' + esc(n.id) + '</span>' +
      '<span class="tag"><span class="dot" style="background:' + colorForKind(n.kind) + '"></span>' + esc(n.kind) + '</span>' +
    '</div>' +
    '<div class="kv-grid">' +
      '<div class="kv-box"><div class="label">PageRank Authority</div><div class="value">' + n.pagerank.toFixed(5) + '</div></div>' +
      '<div class="kv-box"><div class="label">Incident Degree</div><div class="value">' + incident.length + ' edges</div></div>' +
    '</div>' +
    '<div><div style="font-size:11px;font-weight:500;color:var(--text-muted);margin-bottom:0.25rem">ACL Permissions</div><div>' + permsHtml + '</div></div>' +
    '<div><div style="font-size:11px;font-weight:500;color:var(--text-muted);margin-bottom:0.25rem">Properties</div>' + propsHtml + '</div>' +
    '<div><div style="font-size:11px;font-weight:500;color:var(--text-muted);margin-bottom:0.25rem">Incident Edges (' + incident.length + ') — click to jump</div>' +
      (edgesHtml ? '<table class="data-table"><thead><tr><th>Relation</th><th>Neighbor</th></tr></thead><tbody>' + edgesHtml + '</tbody></table>' : '<div style="font-size:12px;color:var(--text-muted)">Isolated vertex.</div>') +
    '</div>';
}

// --- Core Actions: Live Graph & Folder Dry-Run ---
async function loadOverview() {
  try {
    const ov = await apiCall('/api/v1/introspect/overview');
    lastRawPayload = ov;
    document.getElementById('kpiNodes').textContent = String(ov.graph_stats?.node_count ?? 0);
    document.getElementById('kpiEdges').textContent = String(ov.graph_stats?.edge_count ?? 0);
    document.getElementById('kpiVectors').textContent = String(ov.vector_count ?? 0);
    document.getElementById('kpiDim').textContent = String(ov.vector_dimension ?? 384);
    document.getElementById('kpiPending').textContent = String(ov.pending_enrich_count ?? 0);
  } catch (_) {}
}

async function loadLiveGraph() {
  setCanvasOverlay(renderSkeleton(4));
  try {
    const snap = await apiCall('/api/v1/introspect/graph?max_nodes=450');
    lastRawPayload = snap;
    setGraphData(snap.nodes || [], snap.edges || [], snap.pagerank_scores || {}, 'Live Stored Graph', false);
    await loadOverview();
  } catch (e) {
    setCanvasOverlay(renderErrorBanner(e.message));
  }
}

async function runFolderCrawl(dryRun) {
  const rootPath = document.getElementById('crawlPath').value.trim();
  const perms = document.getElementById('crawlPerms').value.split(',').map(s => s.trim()).filter(Boolean);
  if (!rootPath) {
    switchDrawerTab('crawl');
    document.getElementById('drawerContent').innerHTML = renderErrorBanner('Folder path is required.');
    return;
  }

  const btnDry = document.getElementById('btnDryRun');
  const btnIdx = document.getElementById('btnIndexFolder');
  btnDry.disabled = true;
  btnIdx.disabled = true;
  setCanvasOverlay(renderSkeleton(4));

  try {
    const rep = await apiCall('/api/v1/admin/crawl', 'POST', {
      root_path: rootPath,
      dry_run: dryRun,
      scope: activeCrawlScope,
      include_test_go: false,
      permissions: perms.length ? perms : ['*'],
      max_files: 300
    });
    lastCrawlReport = rep;
    lastRawPayload = rep;
    const sg = rep.subgraph || { nodes: [], edges: [] };
    setGraphData(
      sg.nodes || [],
      sg.edges || [],
      rep.pagerank_scores || {},
      dryRun ? 'Dry-Run Preview (' + activeCrawlScope.toUpperCase() + ')' : 'Indexed Folder (' + activeCrawlScope.toUpperCase() + ')',
      dryRun
    );
    switchDrawerTab('crawl');
    if (!dryRun) {
      await loadOverview();
    }
  } catch (e) {
    setCanvasOverlay('');
    switchDrawerTab('crawl');
    document.getElementById('drawerContent').innerHTML = renderErrorBanner(e.message);
  } finally {
    btnDry.disabled = false;
    btnIdx.disabled = false;
  }
}

// --- Module 1: Hybrid RRF Search ---
async function runSearch() {
  const out = document.getElementById('searchOut');
  out.innerHTML = renderSkeleton(3);
  const groups = document.getElementById('searchGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  let docTypes = [];
  if (activeSearchDocFilter === 'docs') {
    docTypes = ['pdf', 'markdown', 'text', 'html', 'docx', 'rfc', 'postmortem', 'memo', 'runbook', 'doc', 'yaml', 'json'];
  } else if (activeSearchDocFilter === 'pdf') {
    docTypes = ['pdf'];
  } else if (activeSearchDocFilter === 'code') {
    docTypes = ['go', 'typescript', 'javascript', 'python', 'sql', 'shell'];
  }
  try {
    const res = await apiCall('/api/v1/search', 'POST', {
      query: document.getElementById('searchQuery').value.trim(),
      user_id: document.getElementById('searchUser').value.trim(),
      groups,
      doc_types: docTypes,
      limit: 8
    });
    document.getElementById('searchLatencyBadge').textContent =
      (res.hits ? res.hits.length : 0) + ' hits · ' + (res.latency_ms || 0).toFixed(2) + ' ms';

    if (!res.hits || res.hits.length === 0) {
      out.innerHTML = '<div style="font-size:12px;color:var(--text-muted);padding:0.75rem 0">No authorized documents matched the query in the "' + esc(activeSearchDocFilter) + '" filter.</div>';
      return;
    }

    out.innerHTML = res.hits.map(h => {
      const s = h.score || {};
      const dt = (h.document?.doc_type || 'doc').toLowerCase();
      const dtBadge = '<span class="tag">' + esc(dt.toUpperCase()) + '</span>';
      const supBadge = (h.supersession && h.supersession.superseded)
        ? '<span class="tag tag-warn">Superseded by ' + esc(h.supersession.superseded_by_id) + '</span>'
        : '<span class="tag tag-ok">' + esc(h.document?.status || 'normal') + '</span>';
      const headingLine = h.matched_chunk?.heading_path
        ? '<div style="font-size:11px;color:var(--text-muted)" class="mono truncate">§ ' + esc(h.matched_chunk.heading_path) + '</div>'
        : '';
      const pct = v => Math.min(100, Math.max(4, Math.round((v || 0) * 100)));
      return '<div class="hit-item">' +
        '<div style="display:flex;align-items:center;justify-content:space-between;gap:0.5rem">' +
          '<span class="mono" style="font-weight:600;font-size:12px;cursor:pointer" onclick="selectNodeById(\'' + esc(h.document?.id) + '\')">' + esc(h.document?.title || h.document?.id) + '</span>' +
          '<div style="display:flex;align-items:center;gap:0.35rem">' +
            dtBadge +
            supBadge +
            '<span class="tag tabular">Score ' + (s.final_score || 0).toFixed(3) + '</span>' +
          '</div>' +
        '</div>' +
        headingLine +
        '<div style="font-size:12px;color:var(--text-secondary)" class="truncate">' + esc(h.matched_chunk?.content || '') + '</div>' +
        '<div class="score-bars">' +
          '<div class="score-cell"><div class="s-head"><span>BM25</span><span>' + (s.lexical_rrf || 0).toFixed(2) + '</span></div><div class="bar-track"><div class="bar-fill" style="width:' + pct(s.lexical_rrf * 4) + '%"></div></div></div>' +
          '<div class="score-cell"><div class="s-head"><span>HNSW</span><span>' + (s.vector_similarity || 0).toFixed(2) + '</span></div><div class="bar-track"><div class="bar-fill" style="width:' + pct(s.vector_similarity) + '%"></div></div></div>' +
          '<div class="score-cell"><div class="s-head"><span>PageRank</span><span>' + (s.graph_pagerank || 0).toFixed(2) + '</span></div><div class="bar-track"><div class="bar-fill" style="width:' + pct(s.graph_pagerank) + '%"></div></div></div>' +
          '<div class="score-cell"><div class="s-head"><span>Lifecycle</span><span>' + (s.supersession_ratio || 1).toFixed(2) + 'x</span></div><div class="bar-track"><div class="bar-fill" style="width:' + pct(s.supersession_ratio) + '%"></div></div></div>' +
        '</div>' +
      '</div>';
    }).join('');
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

// --- Module 2: Path Explainer & ACL Simulator ---
function setSecurityTab(mode) {
  document.getElementById('modeBtnPath').classList.toggle('active', mode === 'path');
  document.getElementById('modeBtnACL').classList.toggle('active', mode === 'acl');
  document.getElementById('controlsPath').style.display = mode === 'path' ? 'flex' : 'none';
  document.getElementById('controlsACL').style.display = mode === 'acl' ? 'flex' : 'none';
}

async function explainPath() {
  const out = document.getElementById('securityOut');
  out.innerHTML = renderSkeleton(3);
  const groups = document.getElementById('expGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  try {
    const rep = await apiCall('/api/v1/introspect/explain', 'POST', {
      from_id: document.getElementById('expFrom').value.trim(),
      to_id: document.getElementById('expTo').value.trim(),
      max_hops: 5,
      user_id: document.getElementById('expUser').value.trim(),
      groups
    });
    const statusHtml = rep.blocked_by_acl
      ? '<div class="banner err"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg><span>Path Blocked by Zero-Trust ACL Bridge · Principal PageRank = ' + (rep.pagerank_score || 0).toFixed(5) + '</span></div>'
      : '<div class="banner ok"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"><polyline points="20 6 9 17 4 12"/></svg><span>Authorized Path (' + (rep.principal_path ? rep.principal_path.length : 0) + ' hops) · Principal PageRank = ' + (rep.pagerank_score || 0).toFixed(5) + '</span></div>';

    const renderChain = (edges, label) => {
      if (!edges || edges.length === 0) {
        return '<div style="font-size:12px;color:var(--text-muted)">' + esc(label) + ': No reachable path.</div>';
      }
      const parts = ['<span class="tag">' + esc(edges[0].source_id) + '</span>'];
      for (const e of edges) {
        parts.push('<span style="color:var(--text-muted);font-size:11px" class="mono">──[' + esc(e.relation) + ']──▶</span>');
        parts.push('<span class="tag">' + esc(e.target_id) + '</span>');
      }
      return '<div><div style="font-size:11px;font-weight:500;color:var(--text-muted);margin-bottom:0.25rem">' + esc(label) + '</div><div class="hop-chain">' + parts.join('') + '</div></div>';
    };

    out.innerHTML = statusHtml +
      renderChain(rep.principal_path, 'Principal-Scoped Shortest Path') +
      renderChain(rep.global_path, 'Unfiltered Global Shortest Path');
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

async function runACLSim() {
  const out = document.getElementById('securityOut');
  out.innerHTML = renderSkeleton(3);
  const groups = document.getElementById('simGroups').value.split(',').map(s => s.trim()).filter(Boolean);
  const targets = document.getElementById('simTargets').value.split(',').map(s => s.trim()).filter(Boolean);
  try {
    const rep = await apiCall('/api/v1/validate/acl-simulate', 'POST', {
      user_id: document.getElementById('simUser').value.trim(),
      user_email: document.getElementById('simEmail').value.trim(),
      groups,
      target_ids: targets
    });
    const rows = (rep.decisions || []).map(d =>
      '<tr>' +
      '<td class="mono">' + esc(d.target_id) + '</td>' +
      '<td>' + (d.allowed ? '<span class="tag tag-ok">ALLOWED</span>' : '<span class="tag tag-err">DENIED</span>') + '</td>' +
      '<td class="mono">' + esc(d.matched_rule || '-') + '</td>' +
      '<td style="color:var(--text-secondary)">' + esc(d.reason || '') + '</td>' +
      '</tr>'
    ).join('');
    out.innerHTML = '<table class="data-table"><thead><tr><th>Target ID</th><th>Decision</th><th>Rule</th><th>Reason</th></tr></thead><tbody>' + rows + '</tbody></table>';
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

// --- Module 3: Integrity Audit & Maintenance ---
async function runValidate() {
  const out = document.getElementById('validateOut');
  out.innerHTML = renderSkeleton(3);
  try {
    const rep = await apiCall('/api/v1/validate', 'POST', {});
    const banner = rep.valid
      ? '<div class="banner ok"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg><span>All 8 Graph &amp; Security Invariants Passed (' + rep.checked_nodes + ' nodes, ' + rep.checked_edges + ' edges)</span></div>'
      : '<div class="banner err"><span>Integrity Audit Detected ' + rep.error_count + ' Errors &amp; ' + rep.warning_count + ' Warnings</span></div>';

    const issuesRows = (rep.issues || []).map(iss =>
      '<tr>' +
      '<td><span class="tag ' + (iss.severity === 'error' ? 'tag-err' : 'tag-warn') + '">' + esc(iss.severity) + '</span></td>' +
      '<td class="mono">' + esc(iss.code) + '</td>' +
      '<td class="mono">' + esc(iss.entity_id) + '</td>' +
      '<td style="color:var(--text-secondary)">' + esc(iss.message) + '</td>' +
      '</tr>'
    ).join('');

    out.innerHTML = banner +
      '<div class="kv-grid">' +
        '<div class="kv-box"><div class="label">Checked Nodes</div><div class="value">' + rep.checked_nodes + '</div></div>' +
        '<div class="kv-box"><div class="label">Checked Edges</div><div class="value">' + rep.checked_edges + '</div></div>' +
        '<div class="kv-box"><div class="label">Errors</div><div class="value">' + rep.error_count + '</div></div>' +
        '<div class="kv-box"><div class="label">Warnings</div><div class="value">' + rep.warning_count + '</div></div>' +
      '</div>' +
      (issuesRows ? '<table class="data-table"><thead><tr><th>Sev</th><th>Code</th><th>Entity</th><th>Message</th></tr></thead><tbody>' + issuesRows + '</tbody></table>' : '');
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

async function triggerEnrich() {
  const out = document.getElementById('validateOut');
  out.innerHTML = renderSkeleton(2);
  try {
    const rep = await apiCall('/api/v1/admin/enrich-sweep', 'POST', { limit: 50 });
    out.innerHTML = '<div class="banner ok"><span>Enrichment Sweep Completed: ' + rep.succeeded + '/' + rep.processed + ' documents enriched in ' + rep.duration_ms.toFixed(1) + ' ms</span></div>';
    await loadLiveGraph();
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

async function triggerRebuild() {
  const out = document.getElementById('validateOut');
  out.innerHTML = renderSkeleton(2);
  try {
    const rep = await apiCall('/api/v1/admin/rebuild', 'POST', {});
    out.innerHTML = '<div class="banner ok"><span>Re-hydrated CSR &amp; HNSW Indices (' + (rep.graph_stats?.node_count ?? 0) + ' nodes, ' + (rep.vector_count ?? 0) + ' vectors)</span></div>';
    await loadLiveGraph();
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

// --- Module 4: Document & Ego-Subgraph Inspector ---
async function inspectDoc() {
  const out = document.getElementById('inspectOut');
  out.innerHTML = renderSkeleton(3);
  const id = encodeURIComponent(document.getElementById('inspectDocID').value.trim());
  try {
    const rep = await apiCall('/api/v1/introspect/document?id=' + id);
    const d = rep.document || {};
    const sup = rep.supersession || {};
    out.innerHTML =
      '<div style="display:flex;align-items:center;justify-content:space-between">' +
        '<span class="mono" style="font-weight:600">' + esc(d.id) + ' — ' + esc(d.title) + '</span>' +
        (sup.superseded ? '<span class="tag tag-warn">Superseded by ' + esc(sup.superseded_by_id) + '</span>' : '<span class="tag tag-ok">Active</span>') +
      '</div>' +
      '<div class="kv-grid">' +
        '<div class="kv-box"><div class="label">Chunks Indexed</div><div class="value">' + (rep.chunks ? rep.chunks.length : 0) + '</div></div>' +
        '<div class="kv-box"><div class="label">Incident Edges</div><div class="value">' + (rep.incident_edges ? rep.incident_edges.length : 0) + '</div></div>' +
      '</div>' +
      '<pre class="json-block">' + esc(JSON.stringify(rep, null, 2)) + '</pre>';
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

async function deleteDoc() {
  const out = document.getElementById('inspectOut');
  const id = encodeURIComponent(document.getElementById('inspectDocID').value.trim());
  try {
    const rep = await apiCall('/api/v1/admin/documents?id=' + id, 'DELETE');
    out.innerHTML = '<div class="banner ok"><span>Deleted document ' + esc(rep.deleted) + ' across SQLite, HNSW, and CSR.</span></div>';
    await loadLiveGraph();
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

async function inspectNode() {
  const out = document.getElementById('inspectOut');
  out.innerHTML = renderSkeleton(3);
  const id = encodeURIComponent(document.getElementById('inspectNodeID').value.trim());
  const hops = encodeURIComponent(document.getElementById('inspectHops').value || '2');
  try {
    const rep = await apiCall('/api/v1/introspect/node?id=' + id + '&hops=' + hops);
    if (rep.subgraph && rep.subgraph.nodes) {
      setGraphData(rep.subgraph.nodes, rep.subgraph.edges || [], {}, 'Ego-Subgraph (' + decodeURIComponent(id) + ')', false);
    }
    out.innerHTML =
      '<div class="banner ok"><span>Loaded ' + (rep.subgraph?.nodes?.length || 0) + '-node ego-subgraph onto canvas for ' + esc(rep.node?.id) + '</span></div>' +
      '<pre class="json-block">' + esc(JSON.stringify(rep, null, 2)) + '</pre>';
  } catch (e) {
    out.innerHTML = renderErrorBanner(e.message);
  }
}

// Initial boot
resizeCanvas();
loadLiveGraph();
runValidate();
runSearch();
explainPath();
</script>
</body>
</html>
`
