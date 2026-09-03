// Flying Shuttle — the small amount of imperative JS that Datastar expressions
// can't express cleanly. Currently: mapping a text selection inside the
// transcript reader onto a {chunk_id, char_start, char_end, text} excerpt.
//
// Evidence offsets are relative to a single chunk's content. A selection that
// crosses chunk boundaries is clamped to the chunk where it starts.

(function () {
  "use strict";

  // Nearest ancestor <span class="reader-seg"> of a DOM node, or null.
  function segOf(node) {
    while (node) {
      if (node.nodeType === 1 && node.classList && node.classList.contains("reader-seg")) {
        return node;
      }
      node = node.parentNode;
    }
    return null;
  }

  // Character offset of (container, offset) measured from the start of seg's
  // text content.
  function offsetInSeg(seg, container, offset) {
    if (container === seg) {
      // offset is a child-node index; sum text of preceding children.
      let total = 0;
      for (let i = 0; i < offset && i < seg.childNodes.length; i++) {
        total += seg.childNodes[i].textContent.length;
      }
      return total;
    }
    let total = 0;
    const walker = document.createTreeWalker(seg, NodeFilter.SHOW_TEXT);
    let n;
    while ((n = walker.nextNode())) {
      if (n === container) return total + offset;
      total += n.textContent.length;
    }
    return total;
  }

  // ---- outline drag-and-drop reorder -------------------------------------
  //
  // Drag a bullet's handle; the drop zone (before / after / child) is chosen
  // from the pointer's vertical position over the target row. On drop we
  // compute {parent_id, position} from the DOM tree and submit #move-form,
  // which posts to /outline/move and morphs #outline back.

  let dragId = null;

  function bulletLi(node) {
    while (node && !(node.nodeType === 1 && node.hasAttribute && node.hasAttribute("data-node-id"))) {
      node = node.parentNode;
    }
    return node || null;
  }

  function zoneFor(li, clientY) {
    const row = li.querySelector(".bullet-row") || li;
    const r = row.getBoundingClientRect();
    const rel = (clientY - r.top) / r.height;
    if (rel < 0.25) return "before";
    if (rel > 0.75) return "after";
    return "child";
  }

  function childrenList(li) {
    return li.querySelector(":scope > ul.bullet-children");
  }

  function computeTarget(li, zone) {
    if (zone === "child") {
      const kids = childrenList(li);
      return { parentId: li.dataset.nodeId, position: kids ? kids.children.length : 0 };
    }
    // sibling of li: parent is the li owning li's containing <ul>, or root.
    const ul = li.parentElement;
    const parentLi = ul && ul.classList.contains("bullet-children") ? bulletLi(ul.parentElement) : null;
    const siblings = Array.prototype.filter.call(ul.children, (c) => c.hasAttribute("data-node-id"));
    let idx = siblings.indexOf(li);
    if (zone === "after") idx += 1;
    return { parentId: parentLi ? parentLi.dataset.nodeId : "", position: idx };
  }

  function clearDropHints() {
    document.querySelectorAll(".bullet.drop-before, .bullet.drop-after, .bullet.drop-child")
      .forEach((el) => el.classList.remove("drop-before", "drop-after", "drop-child"));
  }

  document.addEventListener("dragstart", function (e) {
    const h = e.target.closest && e.target.closest(".drag-handle");
    if (!h) return;
    const li = bulletLi(h);
    if (!li) return;
    dragId = li.dataset.nodeId;
    e.dataTransfer.effectAllowed = "move";
    try { e.dataTransfer.setData("text/plain", dragId); } catch (_) {}
  });

  document.addEventListener("dragover", function (e) {
    if (!dragId) return;
    const li = bulletLi(e.target);
    if (!li || li.dataset.nodeId === dragId) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    clearDropHints();
    li.classList.add("drop-" + zoneFor(li, e.clientY));
  });

  document.addEventListener("dragend", function () { dragId = null; clearDropHints(); });

  document.addEventListener("drop", function (e) {
    if (!dragId) return;
    const li = bulletLi(e.target);
    clearDropHints();
    if (!li || li.dataset.nodeId === dragId) { dragId = null; return; }
    e.preventDefault();

    // Reject dropping onto own subtree.
    if (li.closest('[data-node-id="' + CSS.escape(dragId) + '"] ul.bullet-children')) {
      dragId = null;
      return;
    }

    const { parentId, position } = computeTarget(li, zoneFor(li, e.clientY));
    const form = document.getElementById("move-form");
    if (form) {
      form.elements["node_id"].value = dragId;
      form.elements["parent_id"].value = parentId;
      form.elements["position"].value = String(position);
      form.requestSubmit();
    }
    dragId = null;
  });

  let markIdx = -1; // cursor into the highlighted spans for n / N cycling

  // When the transcript reader patches in with a located span, scroll it into
  // view once. The reader body is replaced on every open / scrub, so a fresh
  // #reader-focus (without our marker) means a new span to reveal.
  new MutationObserver(function () {
    const el = document.getElementById("reader-focus");
    if (el && !el.dataset.scrolled) {
      el.dataset.scrolled = "1";
      el.scrollIntoView({ block: "center", behavior: "smooth" });
    }
    markIdx = -1; // evidence fragment changed — restart span cycling
    updateEvidenceActions();
  }).observe(document.body, { childList: true, subtree: true });

  // ---- keyboard span cycling (n / N) -----------------------------------
  //
  // n / N steps forward / back through the highlighted spans — the reader's
  // marks when it's open, otherwise every candidate card's marks — scrolling
  // each into view. Keyboard-first, like the outline editor.

  function cyclableMarks() {
    const reader = document.getElementById("transcript-reader");
    if (reader && reader.offsetParent !== null) {
      return Array.from(reader.querySelectorAll("mark"));
    }
    return Array.from(document.querySelectorAll("#evidence-candidates mark"));
  }

  function cycleMark(dir) {
    const marks = cyclableMarks();
    document.querySelectorAll("mark.mark-active")
      .forEach((m) => m.classList.remove("mark-active"));
    if (!marks.length) return;
    markIdx = (markIdx + dir + marks.length) % marks.length;
    const m = marks[markIdx];
    m.classList.add("mark-active");
    m.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  document.addEventListener("keydown", function (e) {
    if (e.key !== "n" && e.key !== "N") return;
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.target.closest('input, textarea, [contenteditable="true"]')) return;
    e.preventDefault();
    cycleMark(e.key === "N" ? -1 : 1);
  });

  // ---- docked "Add as evidence" bar (bottom of the right pane) ---------
  //
  // Always reachable — no scrolling to a button. Its mode follows the current
  // selection: attach the selected text (candidate card OR transcript reader),
  // or, with the reader open and nothing selected, attach the located passage.

  function cleanSelText(s) {
    s = s.replace(/\r?\n/g, " ");
    if (s.indexOf("…") >= 0) {
      s = s.split("…").sort((a, b) => b.length - a.length)[0] || ""; // longest contiguous piece
    }
    return s.replace(/\s+/g, " ").trim();
  }

  function candidateTextOf(node) {
    while (node) {
      if (node.nodeType === 1 && node.classList &&
        (node.classList.contains("candidate-text") || node.classList.contains("candidate-full"))) {
        return node;
      }
      node = node.parentNode;
    }
    return null;
  }

  // chunk id for the current selection, if it sits in one candidate card or in
  // the reader body; null otherwise.
  function selectionChunkId(range) {
    const ctStart = candidateTextOf(range.startContainer);
    const card = ctStart && ctStart.closest(".candidate");
    if (card) {
      const ctEnd = candidateTextOf(range.endContainer);
      if (ctEnd && ctEnd.closest(".candidate") === card) return card.dataset.chunk;
    }
    const seg = segOf(range.startContainer);
    if (seg && seg.closest(".reader-body")) return seg.dataset.chunk;
    return null;
  }

  function updateEvidenceActions() {
    const bar = document.getElementById("evidence-actions");
    if (!bar) return;
    const preview = bar.querySelector("[data-ea-preview]");
    const btn = document.getElementById("ea-add");
    const attachForm = document.getElementById("evidence-attach-form");
    if (!preview || !btn || !attachForm) return;

    const sel = document.getSelection();
    const range = sel && !sel.isCollapsed && sel.rangeCount ? sel.getRangeAt(0) : null;

    if (range) {
      const chunkId = selectionChunkId(range);
      const text = chunkId ? cleanSelText(sel.toString()) : "";
      if (chunkId && text.length >= 2) {
        attachForm.elements["chunk_id"].value = chunkId;
        attachForm.elements["text"].value = text;
        bar.dataset.mode = "selection";
        preview.textContent = "“" + (text.length > 90 ? text.slice(0, 90) + "…" : text) + "”";
        btn.textContent = "Add selection";
        bar.hidden = false;
        return;
      }
    }

    const reader = document.getElementById("transcript-reader");
    if (reader && reader.offsetParent !== null) {
      const ef = document.getElementById("excerpt-form");
      bar.dataset.mode = "passage";
      preview.textContent = ef && ef.dataset.prefilled === "1"
        ? "The located passage" : "This whole passage";
      btn.textContent = "Add passage";
      bar.hidden = false;
      return;
    }

    bar.hidden = true;
  }

  document.addEventListener("selectionchange", updateEvidenceActions);

  document.addEventListener("click", function (e) {
    if (!e.target.closest || !e.target.closest("#ea-add")) return;
    const bar = document.getElementById("evidence-actions");
    const s = document.getSelection();
    if (bar && bar.dataset.mode === "selection") {
      const f = document.getElementById("evidence-attach-form");
      if (!f || !f.elements["chunk_id"].value || !f.elements["text"].value) return;
      if (s) s.removeAllRanges();
      bar.hidden = true;
      f.requestSubmit();
    } else {
      const f = document.getElementById("excerpt-form");
      if (f) f.requestSubmit();
    }
  });

  // ---- quote trim / splice from an outline text selection --------------
  //
  // Selecting text inside a .bullet-evidence blockquote pops a small toolbar:
  // "Trim to selection" keeps only that range, "Remove selection" splices it
  // out. Both fill and submit #quote-edit-form (rune offsets into the quote).

  function evidenceOf(node) {
    while (node) {
      if (node.nodeType === 1 && node.classList && node.classList.contains("bullet-evidence")) {
        return node;
      }
      node = node.parentNode;
    }
    return null;
  }

  // Position a .float-tools element above (or below, if clipped) a range.
  function positionFloat(t, range) {
    const r = range.getBoundingClientRect();
    t.hidden = false;
    const tw = t.offsetWidth || 220;
    let left = r.left + r.width / 2 - tw / 2;
    left = Math.max(6, Math.min(left, window.innerWidth - tw - 6));
    let top = r.top - t.offsetHeight - 6;
    if (top < 6) top = r.bottom + 6;
    t.style.left = left + "px";
    t.style.top = top + "px";
  }

  function hideFloat(id) {
    const t = document.getElementById(id);
    if (t) t.hidden = true;
  }

  // -- quote trim / splice (selection inside .bullet-evidence) --

  function showQuoteTools(bq, range) {
    const t = document.getElementById("quote-tools");
    const li = bq.closest("[data-node-id]");
    const form = document.getElementById("quote-edit-form");
    if (!t || !li || !form) return;

    const start = offsetInSeg(bq, range.startContainer, range.startOffset);
    const end = offsetInSeg(bq, range.endContainer, range.endOffset);
    if (end - start < 1) { hideFloat("quote-tools"); return; }

    form.elements["node_id"].value = li.dataset.nodeId;
    form.elements["start"].value = String(start);
    form.elements["end"].value = String(end);
    positionFloat(t, range);
  }

  document.addEventListener("selectionchange", function () {
    const sel = document.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) { hideFloat("quote-tools"); return; }
    const range = sel.getRangeAt(0);
    const bq = evidenceOf(range.startContainer);
    if (bq && bq === evidenceOf(range.endContainer)) {
      showQuoteTools(bq, range);
    } else {
      hideFloat("quote-tools");
    }
  });

  // Don't drop the selection when reaching for a floating tool button.
  document.addEventListener("mousedown", function (e) {
    const t = e.target.closest && e.target.closest(".float-tools");
    if (t && !t.hidden) e.preventDefault();
  });

  document.addEventListener("click", function (e) {
    const qt = e.target.closest && e.target.closest("#quote-tools button[data-qt]");
    if (!qt) return;
    const form = document.getElementById("quote-edit-form");
    if (form && form.elements["node_id"].value) {
      form.elements["op"].value = qt.dataset.qt;
      hideFloat("quote-tools");
      const s = document.getSelection();
      if (s) s.removeAllRanges();
      form.requestSubmit();
    }
  });
})();
