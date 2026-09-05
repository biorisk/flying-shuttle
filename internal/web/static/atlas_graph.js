/* Source Atlas — network view.
 *
 * Top level: one node per TRANSCRIPT (source file) — a disjoint,
 * already-meaningful partition, so no clustering runs to produce it. Each
 * node's label comes from that transcript's own digest (keywords, or a title,
 * falling back to the filename — see atlasGraphJSON). Edges are
 * transcript-to-transcript, aggregated server-side from a chunk-level
 * similarity graph (internal/atlas.BuildTranscriptEdges — the MAX chunk-chunk
 * weight crossing between two files, and only each transcript's K strongest
 * cross-transcript edges, so a big corpus doesn't collapse into one clump).
 *
 * Tapping a transcript drills into that file's own chunks, laid out in
 * document order as a filmstrip (wrapping rows, not a force layout — it's a
 * path graph, and forcing it just coils it into a mess), each labelled with
 * its own keywords rather than a text snippet, and connected only by
 * adjacency (chunk[i]-chunk[i+1]) — never by embedding similarity. Tapping
 * the background while drilled in returns to the transcript level.
 *
 * Selection is reported back to Datastar via custom events on #atlas-graph
 * ('atlasnode' for a transcript's primary region tag, 'atlaschunk' for a
 * tapped chunk) so the list pane and the graph stay in sync.
 *
 * The graph is NOT the outline. Region ≠ node, link ≠ edge — see
 * source_atlas_plan.md §0.
 */
(function () {
  "use strict";

  var cy = null;
  var drilled = null; // transcript id currently drilled into, or null at top level
  var host = function () { return document.getElementById("atlas-graph"); };

  // Nodes are filled with their region colour (data(color), from the server —
  // see regionColor). A dark outline plus a text outline keeps the label
  // readable over any palette colour.
  var STYLE = [
    { selector: "node", style: {
        "label": "data(label)", "font-size": 9, "color": "#eef1f8",
        "text-wrap": "wrap", "text-max-width": 90, "text-valign": "center",
        "text-outline-width": 2, "text-outline-color": "#0e1220",
        "background-color": "data(color)", "border-color": "#0e1220",
        "width": "data(size)", "height": "data(size)", "border-width": 1 } },
    { selector: 'node[kind="chunk"]', style: { "shape": "round-rectangle" } },
    { selector: "node:selected", style: { "border-width": 3, "border-color": "#ffffff" } },
    { selector: "edge", style: { "width": "data(w)", "line-color": "#33406a", "curve-style": "haystack", "opacity": 0.7 } },
  ];

  var DEFAULT_COLOR = "#4a5578";

  function emit(name, id) {
    var el = host();
    if (el) el.dispatchEvent(new CustomEvent(name, { detail: { id: id }, bubbles: true }));
  }

  function transcriptElements(data) {
    var maxC = 1;
    (data.transcripts || []).forEach(function (t) { maxC = Math.max(maxC, t.chunks); });
    var els = (data.transcripts || []).map(function (t) {
      return { data: {
        id: t.id, kind: "transcript", label: t.label || t.id, tags: t.tags || [],
        color: t.color || DEFAULT_COLOR,
        size: 22 + 34 * Math.sqrt(t.chunks / maxC) } };
    });
    (data.edges || []).forEach(function (e) {
      els.push({ data: { id: e.a + "~" + e.b, source: e.a, target: e.b, w: 1 + 5 * e.w } });
    });
    return els;
  }

  function chunkElements(data) {
    var els = (data.chunks || []).map(function (c) {
      return { data: { id: c.id, kind: "chunk", label: c.label, color: c.color || DEFAULT_COLOR, size: 26 } };
    });
    (data.edges || []).forEach(function (e) {
      els.push({ data: { id: e.a + "~" + e.b, source: e.a, target: e.b, w: 2 } });
    });
    return els;
  }

  // A path graph coils into a mess under a force layout — lay it out
  // explicitly instead: left-to-right in document order, wrapping to a new
  // row every PER_ROW nodes, so the sequence itself stays legible.
  var PER_ROW = 12, SPACING = 90;
  function filmstripLayout() {
    var nodes = cy.nodes('[kind="chunk"]');
    nodes.forEach(function (n, i) {
      var row = Math.floor(i / PER_ROW), col = i % PER_ROW;
      n.position({ x: col * SPACING, y: row * SPACING });
    });
    cy.fit(undefined, 30);
  }

  // fcose is required, not a preference, once the corpus runs into the
  // thousands — cose is O(n^2)-ish per iteration and hangs past a few
  // hundred nodes. The transcript-level graph is small (one node per file),
  // so this mainly matters if a corpus ever has many hundreds of transcripts.
  function forceLayout() {
    var opts = {
      name: "fcose", animate: false, quality: "draft", randomize: true,
      nodeRepulsion: 6000, idealEdgeLength: 90, padding: 30,
    };
    try {
      return cy.layout(opts).run();
    } catch (err) {
      console.error("atlas graph: fcose layout unavailable, falling back to cose", err);
      return cy.layout({ name: "cose", animate: false, nodeRepulsion: 6000, idealEdgeLength: 90, padding: 30 }).run();
    }
  }

  function ensureCy() {
    if (cy || !window.cytoscape) return cy;
    cy = window.cytoscape({ container: host(), style: STYLE, minZoom: 0.2, maxZoom: 3, wheelSensitivity: 0.3 });

    cy.on("tap", 'node[kind="transcript"]', function (e) {
      var n = e.target;
      var tags = n.data("tags") || [];
      if (tags.length) emit("atlasnode", tags[0]); // primary tag keeps the list pane in sync
      drillInto(n.id());
    });
    cy.on("tap", 'node[kind="chunk"]', function (e) { emit("atlaschunk", e.target.id()); });
    cy.on("tap", function (e) { if (e.target === cy && drilled) open(); }); // background tap -> back out
    return cy;
  }

  var loadedBuildAt = 0; // which atlas build the top-level graph currently shows

  function open() {
    if (!ensureCy()) return;
    drilled = null;
    fetch("/atlas/graph.json").then(function (r) { return r.json(); }).then(function (data) {
      loadedBuildAt = data.buildAt || 0;
      cy.elements().remove();
      cy.add(transcriptElements(data));
      forceLayout();
      cy.fit(undefined, 30);
    });
  }

  // Called when a rebuild lands (via a signal patch): reload the top-level
  // graph in place, but never yank a drill-down out from under the user.
  function syncTo(buildAt) {
    if (buildAt && buildAt !== loadedBuildAt && !drilled && cy) open();
  }

  function drillInto(transcriptID) {
    if (!ensureCy()) return;
    fetch("/atlas/graph.json?transcript=" + encodeURIComponent(transcriptID))
      .then(function (r) { return r.json(); })
      .then(function (data) {
        drilled = transcriptID;
        cy.elements().remove();
        cy.add(chunkElements(data));
        filmstripLayout();
      });
  }

  window.atlasGraph = { open: open, syncTo: syncTo };

  // If the page loads with the graph already selected (e.g. after a reload),
  // render once the container is visible.
  document.addEventListener("DOMContentLoaded", function () {
    var el = host();
    if (el && el.offsetParent !== null) open();
  });
})();
