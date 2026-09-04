/* Source Atlas — network view.
 *
 * Renders the region graph (regions as nodes, similarity links as edges) on a
 * pan/zoom canvas with semantic level-of-detail: zooming past a threshold onto
 * a region, or tapping "expand", swaps that region node for its member chunk
 * nodes. Selection is reported back to Datastar via custom events on #atlas-graph
 * ('atlasnode' for a region, 'atlaschunk' for a chunk) so the list pane and the
 * graph stay in sync.
 *
 * The graph is NOT the outline. Region ≠ node, link ≠ edge — see
 * source_atlas_plan.md §0.
 */
(function () {
  "use strict";

  var cy = null;
  var expanded = null; // region id currently exploded into chunk nodes
  var host = function () { return document.getElementById("atlas-graph"); };

  var STYLE = [
    { selector: "node", style: {
        "label": "data(label)", "font-size": 9, "color": "#c9d1e6",
        "text-wrap": "wrap", "text-max-width": 90, "text-valign": "center",
        "background-color": "#3a4a7a", "width": "data(size)", "height": "data(size)",
        "border-width": 1, "border-color": "#5566aa" } },
    { selector: 'node[kind="chunk"]', style: { "background-color": "#7a5a3a", "border-color": "#aa8855", "shape": "round-rectangle" } },
    { selector: "node:selected", style: { "border-width": 3, "border-color": "#646cff" } },
    { selector: "edge", style: { "width": "data(w)", "line-color": "#33406a", "curve-style": "haystack", "opacity": 0.7 } },
  ];

  function emit(name, id) {
    var el = host();
    if (el) el.dispatchEvent(new CustomEvent(name, { detail: { id: id }, bubbles: true }));
  }

  function regionElements(data) {
    var maxC = 1;
    (data.regions || []).forEach(function (r) { maxC = Math.max(maxC, r.chunks); });
    var els = (data.regions || []).map(function (r) {
      return { data: {
        id: r.id, kind: "region",
        label: (r.keywords && r.keywords.length ? r.keywords.slice(0, 3).join(" · ") : (r.title || "region")),
        size: 22 + 34 * Math.sqrt(r.chunks / maxC) } };
    });
    (data.links || []).forEach(function (l) {
      els.push({ data: { id: l.a + "~" + l.b, source: l.a, target: l.b, w: 1 + 5 * l.w } });
    });
    return els;
  }

  function chunkElements(data) {
    var els = (data.chunks || []).map(function (c) {
      return { data: { id: c.id, kind: "chunk", label: c.label || c.source, size: 26 } };
    });
    (data.edges || []).forEach(function (e) {
      els.push({ data: { id: e.a + "~" + e.b, source: e.a, target: e.b, w: 1 + 4 * e.w } });
    });
    return els;
  }

  function layout() {
    return cy.layout({ name: "cose", animate: false, nodeRepulsion: 6000, idealEdgeLength: 90, padding: 20 }).run();
  }

  function ensureCy() {
    if (cy || !window.cytoscape) return cy;
    cy = window.cytoscape({ container: host(), style: STYLE, minZoom: 0.2, maxZoom: 3, wheelSensitivity: 0.3 });

    cy.on("tap", 'node[kind="region"]', function (e) {
      emit("atlasnode", e.target.id());
      explode(e.target.id());
    });
    cy.on("tap", 'node[kind="chunk"]', function (e) { emit("atlaschunk", e.target.id()); });
    cy.on("tap", function (e) { if (e.target === cy && expanded) collapse(); });

    // Semantic zoom: past a threshold, explode the region nearest the viewport.
    cy.on("zoom", function () {
      if (cy.zoom() > 1.8 && !expanded) {
        var c = cy.nodes('[kind="region"]');
        if (c.length) explode(c[0].id());
      } else if (cy.zoom() < 1.1 && expanded) {
        collapse();
      }
    });
    return cy;
  }

  function open() {
    if (!ensureCy()) return;
    fetch("/atlas/graph.json").then(function (r) { return r.json(); }).then(function (data) {
      expanded = null;
      cy.elements().remove();
      cy.add(regionElements(data));
      layout();
      cy.fit(undefined, 30);
    });
  }

  function explode(regionId) {
    if (expanded === regionId) return;
    fetch("/atlas/graph.json?region=" + encodeURIComponent(regionId))
      .then(function (r) { return r.json(); })
      .then(function (data) {
        expanded = regionId;
        cy.elements().remove();
        cy.add(chunkElements(data));
        layout();
        cy.fit(undefined, 30);
      });
  }

  function collapse() { expanded = null; open(); }

  window.atlasGraph = { open: open };

  // If the page loads with the graph already selected (e.g. after a reload),
  // render once the container is visible.
  document.addEventListener("DOMContentLoaded", function () {
    var el = host();
    if (el && el.offsetParent !== null) open();
  });
})();
