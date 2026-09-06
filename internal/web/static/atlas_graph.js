/* Source Atlas — network view.
 *
 * One node per TRANSCRIPT (source file) — a disjoint, already-meaningful
 * partition, so no clustering runs to produce it. Each node's label comes from
 * that transcript's own digest (keywords, or a title, falling back to the
 * filename — see atlasGraphJSON). Node fill is that transcript's dominant
 * region colour. Edges are transcript-to-transcript similarity, sent
 * strongest-first and in full; the "links" slider caps how many are drawn.
 *
 * Fewer links == a sparser graph == the force layout (fcose) can push the
 * clusters apart instead of collapsing everything into one blob. Every slider
 * change re-runs the layout (debounced).
 *
 * Tapping a transcript fires 'atlastranscript' on #atlas-graph with the
 * source file; Datastar loads that transcript's chunk sequence into the right
 * pane (AtlasTranscript). There is no in-graph drill-down.
 *
 * The graph is NOT the outline. Region != node, link != edge — see
 * source_atlas_plan.md §0.
 */
(function () {
  "use strict";

  var cy = null;
  var host = function () { return document.getElementById("atlas-graph"); };
  var slider = function () { return document.getElementById("atlas-edge-slider"); };
  var readout = function () { return document.getElementById("atlas-edge-readout"); };

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
    { selector: "node:selected", style: { "border-width": 3, "border-color": "#ffffff" } },
    { selector: "edge", style: { "width": "data(width)", "line-color": "#33406a", "curve-style": "haystack", "opacity": 0.6 } },
  ];

  var DEFAULT_COLOR = "#4a5578";

  // fcose, tuned to spread a few hundred nodes apart rather than pile them up:
  // strong repulsion, long ideal edges, real node separation, and label boxes
  // counted as node area so two nodes never sit under each other's text.
  var LAYOUT = {
    name: "fcose", animate: false, quality: "default", randomize: true,
    nodeDimensionsIncludeLabels: true,
    nodeRepulsion: 30000, idealEdgeLength: 130, edgeElasticity: 0.12,
    gravity: 0.12, gravityRange: 4.0, nodeSeparation: 95,
    numIter: 2500, packComponents: true,
    tilingPaddingVertical: 24, tilingPaddingHorizontal: 24,
  };

  var allNodes = []; // every transcript, as cy elements
  var allEdges = []; // every candidate edge, strongest-first, as cy elements
  var shownEdges = 0; // how many of allEdges to draw
  var loadedBuildAt = 0;

  function emit(name, id) {
    var el = host();
    if (el) el.dispatchEvent(new CustomEvent(name, { detail: { id: id }, bubbles: true }));
  }

  function nodeElements(data) {
    var maxC = 1;
    (data.transcripts || []).forEach(function (t) { maxC = Math.max(maxC, t.chunks); });
    return (data.transcripts || []).map(function (t) {
      return { data: {
        id: t.id, kind: "transcript", label: t.label || t.id, tags: t.tags || [],
        color: t.color || DEFAULT_COLOR,
        size: 22 + 34 * Math.sqrt(t.chunks / maxC) } };
    });
  }

  function edgeElements(data) {
    // The server sends edges already sorted strongest-first.
    return (data.edges || []).map(function (e) {
      return { data: { id: e.a + "~" + e.b, source: e.a, target: e.b, w: e.w, width: 1 + 4 * e.w } };
    });
  }

  function forceLayout() {
    try {
      return cy.layout(LAYOUT).run();
    } catch (err) {
      console.error("atlas graph: fcose layout unavailable, falling back to cose", err);
      return cy.layout({ name: "cose", animate: false, nodeRepulsion: 30000, idealEdgeLength: 130, padding: 30 }).run();
    }
  }

  function updateReadout(n) {
    var r = readout();
    if (r) r.textContent = n + " / " + allEdges.length;
  }

  function render() {
    if (!cy) return;
    var n = Math.max(0, Math.min(shownEdges, allEdges.length));
    cy.elements().remove();
    cy.add(allNodes);
    if (n > 0) cy.add(allEdges.slice(0, n));
    cy.resize(); // the container may have been hidden (0×0) when cy was created
    forceLayout();
    cy.fit(undefined, 30);
    updateReadout(n);
  }

  var relayoutTimer = null;
  function setEdges(n) {
    shownEdges = n;
    updateReadout(Math.max(0, Math.min(n, allEdges.length))); // instant feedback
    clearTimeout(relayoutTimer);
    relayoutTimer = setTimeout(render, 250); // debounce the (expensive) relayout
  }

  function wireSlider() {
    var s = slider();
    if (!s || s._wired) return;
    s._wired = true;
    s.addEventListener("input", function () { setEdges(+s.value); });
  }

  function ensureCy() {
    if (cy || !window.cytoscape) return cy;
    cy = window.cytoscape({ container: host(), style: STYLE, minZoom: 0.12, maxZoom: 3, wheelSensitivity: 0.3 });
    cy.on("tap", 'node[kind="transcript"]', function (e) {
      emit("atlastranscript", e.target.id());
    });
    return cy;
  }

  function open() {
    if (!ensureCy()) return;
    wireSlider();
    fetch("/atlas/graph.json").then(function (r) { return r.json(); }).then(function (data) {
      loadedBuildAt = data.buildAt || 0;
      allNodes = nodeElements(data);
      allEdges = edgeElements(data);
      // Default to a light dusting of the strongest links (~1 per 4 nodes):
      // enough to show the shape of the clusters, sparse enough that they
      // don't fuse into one clump.
      shownEdges = Math.min(allEdges.length, Math.round(allNodes.length * 0.25));
      var s = slider();
      if (s) {
        s.max = String(allEdges.length || 1);
        s.value = String(shownEdges);
      }
      render();
    });
  }

  // Called when a rebuild lands (via a signal patch): reload the graph in place
  // so it swaps to the new build.
  function syncTo(buildAt) {
    if (buildAt && buildAt !== loadedBuildAt && cy) open();
  }

  window.atlasGraph = { open: open, syncTo: syncTo, setEdges: setEdges };

  // If the page loads with the graph already selected (e.g. after a reload),
  // render once the container is visible.
  document.addEventListener("DOMContentLoaded", function () {
    var el = host();
    if (el && el.offsetParent !== null) open();
  });
})();
