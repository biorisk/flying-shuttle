package components

import (
	"fmt"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// intString renders an int for use in an attribute value.
func intString(n int) string { return strconv.Itoa(n) }

func pluralN(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func activeBranchName(vm viewmodel.BranchBar) string {
	for _, b := range vm.Branches {
		if b.Active {
			return b.Name
		}
	}
	return "main"
}

// pageRangeLabel renders the evidence pane's count line, e.g.
// "13–24 of 137 passages". Semantic mode's total is bounded by
// search.MaxVectorPoolSize (HNSW is approximate top-k, not exhaustive), so
// it's labeled "nearest neighbors" rather than implying every match is
// accounted for.
func pageRangeLabel(vm viewmodel.EvidencePane) string {
	if vm.TotalMatches == 0 {
		return "0 passages"
	}
	start := (vm.Page-1)*vm.PageSize + 1
	end := start + len(vm.Candidates) - 1
	noun := "passages"
	if vm.Mode == "semantic" {
		noun = "nearest neighbors"
	}
	return fmt.Sprintf("%d–%d of %d %s", start, end, vm.TotalMatches, noun)
}

// evidenceFetchTail is the part of the @get(...) URL every evidence-pane
// trigger (bullet input, mode toggle, pager) shares once the node id segment
// is in place: current query, mode, and pagination off their page signals.
const evidenceFetchTail = "&q=' + encodeURIComponent($evidenceQuery) + '&mode=' + $searchMode + '&page=' + $evidencePage + '&page_size=' + $evidencePageSize)"

// evidenceExpr fetches the evidence pane for the current bullet text. Datastar
// auto-cancels the previous in-flight request for the same element, so rapid
// typing collapses to the latest query. $evidenceQuery is kept in step with
// the bullet text so the mode toggle and pager can re-fetch it without
// needing a fresh keystroke. A new query always jumps back to page 1 — a
// stale page number from the last bullet wouldn't mean anything here.
//
// The node id is spliced directly into the URL literal (not as a separate
// JS string operand) so a plain substring match on "/evidence?node=<id>"
// still finds it server-rendered, same as before pagination existed.
func evidenceExpr(id string) string {
	return "$evidenceQuery = evt.target.value; $evidencePage = 1; " +
		"@get('/evidence?node=" + id + evidenceFetchTail
}

// evidenceModeExpr switches the evidence pane's retrieval mode and re-issues
// the last query under it, back at page 1 (each mode has its own ranking and
// total, so the page you were on in another mode isn't meaningful here). A
// no-op fetch (blank $evidenceQuery) still lands — the handler just
// re-renders the idle placeholder — which is fine since the button is rarely
// reachable before a first query anyway.
func evidenceModeExpr(mode string) string {
	return "$searchMode = '" + mode + "'; $evidencePage = 1; " +
		"@get('/evidence?node=' + ($focusId||'') + '" + evidenceFetchTail
}

// evidencePageExpr moves the evidence pane's pager by delta pages (-1/+1),
// clamped to [1, totalPages], and re-fetches. totalPages comes from the just
// rendered EvidencePane so the client never has to guess it.
func evidencePageExpr(delta int, totalPages int) string {
	return "$evidencePage = Math.max(1, Math.min(" + intString(totalPages) + ", $evidencePage + (" + intString(delta) + "))); " +
		"@get('/evidence?node=' + ($focusId||'') + '" + evidenceFetchTail
}

// evidencePageSizeExpr changes the evidence pane's page size from the pager's
// <select> and re-fetches at page 1 (the old page number belongs to the old
// size's pagination, not the new one's).
func evidencePageSizeExpr() string {
	return "$evidencePageSize = parseInt(evt.target.value); $evidencePage = 1; " +
		"@get('/evidence?node=' + ($focusId||'') + '" + evidenceFetchTail
}

// threadToggleExpr toggles a bullet's thread membership, or in Brush mode
// appends it to the end of the thread's reading path.
func threadToggleExpr(id string) string {
	base := "/threads/' + $threadId + '/nodes/" + id
	return "$brushMode ? @post('" + base + "/append') : @post('" + base + "/toggle')"
}

// keydownExpr is the Datastar expression for a bullet input's keydown handler:
// Enter adds a sibling (via the form), Tab / Shift-Tab indent / unindent,
// Backspace-on-empty deletes, Arrow Up/Down move the focus signal. Each
// structural request carries the form so the current title is persisted first.
func keydownExpr(id string) string {
	base := "/outline/nodes/" + id
	return "" +
		"evt.key==='Tab' ? (evt.preventDefault(), evt.shiftKey ? " +
		"@post('" + base + "/unindent', {contentType:'form'}) : " +
		"@post('" + base + "/indent', {contentType:'form'})) : " +
		"(evt.key==='Backspace' && evt.target.value==='') ? " +
		"(evt.preventDefault(), @delete('" + base + "')) : " +
		"evt.key==='ArrowUp' ? " +
		"($focusId = evt.target.closest('[data-node-id]').dataset.prevId || $focusId) : " +
		"evt.key==='ArrowDown' ? " +
		"($focusId = evt.target.closest('[data-node-id]').dataset.nextId || $focusId) : null"
}

// evidenceText is the passage shown for an evidence (chunk_ref) bullet: the
// stored excerpt body, falling back to the title preview.
func evidenceText(n viewmodel.OutlineNode) string {
	if n.Body != "" {
		return n.Body
	}
	return n.Title
}

func exitGlyph(t string) string {
	switch t {
	case "jump":
		return "⇥"
	case "branch":
		return "⋔"
	default:
		return "→"
	}
}

// scorePct renders a 0..1 score as a clamped integer percentage like "72%".
func scorePct(v float64) string {
	p := int(v*100 + 0.5)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return strconv.Itoa(p) + "%"
}

// sentShade maps a 0..1 sentence relevance score to a background style,
// light → dark in the accent hue. Below a small floor it stays transparent.
func sentShade(score float64) string {
	if score < 0.05 {
		return "background:transparent"
	}
	a := 0.08 + 0.30*score // 0.08 .. 0.38
	return "background:rgba(100,108,255," + strconv.FormatFloat(a, 'f', 2, 64) + ")"
}

// boolAttr renders a boolean as "1"/"" for a data- attribute.
func boolAttr(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// excerptOffset renders a rune offset for the excerpt form, or "" when there
// is no preselected span (so the handler falls back to the whole chunk).
func excerptOffset(n int, has bool) string {
	if !has {
		return ""
	}
	return strconv.Itoa(n)
}

func orEmpty(s string) string {
	if s == "" {
		return "(untitled)"
	}
	return s
}
