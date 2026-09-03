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

  function setField(form, name, val) {
    const el = form.elements[name];
    if (el) el.value = val;
  }

  function fillExcerptForm(sel) {
    const form = document.getElementById("excerpt-form");
    if (!form) return;
    const reader = form.closest(".transcript-reader");

    // No / collapsed selection. Keep the server-side prefill (the located
    // span) until the user makes a real selection; otherwise attach the whole
    // focus chunk.
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
      if (form.dataset.prefilled === "1") return;
      const focusSeg = reader &&
        (reader.querySelector(".reader-seg.focus") || reader.querySelector(".reader-seg"));
      if (focusSeg) setField(form, "chunk_id", focusSeg.dataset.chunk);
      setField(form, "char_start", "");
      setField(form, "char_end", "");
      setField(form, "text", "");
      form.dataset.hasSelection = "";
      return;
    }

    const range = sel.getRangeAt(0);
    const startSeg = segOf(range.startContainer);
    if (!startSeg) return;

    const start = offsetInSeg(startSeg, range.startContainer, range.startOffset);
    const endSeg = segOf(range.endContainer);
    const end = endSeg === startSeg
      ? offsetInSeg(startSeg, range.endContainer, range.endOffset)
      : startSeg.textContent.length; // clamp to end of the start chunk
    if (end <= start) return;

    const text = startSeg.textContent.slice(start, end).trim();
    if (!text) return;

    setField(form, "chunk_id", startSeg.dataset.chunk);
    setField(form, "char_start", String(start));
    setField(form, "char_end", String(end));
    setField(form, "text", text);
    form.dataset.hasSelection = "1";
    form.dataset.prefilled = ""; // user has taken over from the located span
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

  document.addEventListener("selectionchange", function () {
    const sel = document.getSelection();
    const anchor = sel && sel.anchorNode;
    const inReader = anchor &&
      ((anchor.parentElement && anchor.parentElement.closest(".reader-body")) ||
        (anchor.nodeType === 1 && anchor.closest && anchor.closest(".reader-body")));
    fillExcerptForm(inReader ? sel : null);
  });
})();
