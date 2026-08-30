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

    // No / collapsed selection: whole focus chunk.
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
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
  }

  document.addEventListener("selectionchange", function () {
    const sel = document.getSelection();
    const anchor = sel && sel.anchorNode;
    const inReader = anchor &&
      ((anchor.parentElement && anchor.parentElement.closest(".reader-body")) ||
        (anchor.nodeType === 1 && anchor.closest && anchor.closest(".reader-body")));
    fillExcerptForm(inReader ? sel : null);
  });
})();
