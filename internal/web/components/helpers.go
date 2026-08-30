package components

import (
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// intString renders an int for use in an attribute value.
func intString(n int) string { return strconv.Itoa(n) }

// evidenceText is the passage shown for an evidence (chunk_ref) bullet: the
// stored excerpt body, falling back to the title preview.
func evidenceText(n viewmodel.OutlineNode) string {
	if n.Body != "" {
		return n.Body
	}
	return n.Title
}
