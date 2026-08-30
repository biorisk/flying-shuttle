package components

import (
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

// evidenceExpr fetches the evidence pane for the current bullet text. Datastar
// auto-cancels the previous in-flight request for the same element, so rapid
// typing collapses to the latest query.
func evidenceExpr(id string) string {
	return "@get('/app/evidence?node=" + id + "&q=' + encodeURIComponent(evt.target.value))"
}

// keydownExpr is the Datastar expression for a bullet input's keydown handler:
// Enter adds a sibling (via the form), Tab / Shift-Tab indent / unindent,
// Backspace-on-empty deletes, Arrow Up/Down move the focus signal. Each
// structural request carries the form so the current title is persisted first.
func keydownExpr(id string) string {
	base := "/app/outline/nodes/" + id
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
