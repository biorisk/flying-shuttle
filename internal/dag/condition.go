package dag

import "strings"

// EvalContext provides the variables available when evaluating edge conditions.
type EvalContext struct {
	// Audience is the current audience name (e.g. "novice", "expert").
	Audience string
	// ReadNodes is the set of node IDs the reader has already visited.
	ReadNodes map[string]bool
	// Labels are arbitrary key-value pairs for flexible matching.
	Labels map[string]string
}

// EvalCondition evaluates a simple condition expression against the context.
// Supported forms:
//   - "audience==novice"       — matches if ctx.Audience == "novice"
//   - "audience!=expert"       — matches if ctx.Audience != "expert"
//   - "has_read(node_id)"      — matches if node_id is in ctx.ReadNodes
//   - "!has_read(node_id)"     — matches if node_id is NOT in ctx.ReadNodes
//   - "label.key==value"       — matches if ctx.Labels["key"] == "value"
//   - ""                       — empty condition always matches
//
// Returns true if the condition is satisfied or empty.
func EvalCondition(cond string, ctx *EvalContext) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true
	}
	if ctx == nil {
		ctx = &EvalContext{}
	}

	// has_read(node_id) / !has_read(node_id)
	negated := false
	expr := cond
	if strings.HasPrefix(expr, "!") {
		negated = true
		expr = expr[1:]
	}
	if strings.HasPrefix(expr, "has_read(") && strings.HasSuffix(expr, ")") {
		nodeID := expr[len("has_read(") : len(expr)-1]
		nodeID = strings.TrimSpace(nodeID)
		result := ctx.ReadNodes[nodeID]
		if negated {
			return !result
		}
		return result
	}

	// audience==value / audience!=value
	if strings.HasPrefix(cond, "audience==") {
		val := strings.TrimPrefix(cond, "audience==")
		return strings.EqualFold(ctx.Audience, strings.TrimSpace(val))
	}
	if strings.HasPrefix(cond, "audience!=") {
		val := strings.TrimPrefix(cond, "audience!=")
		return !strings.EqualFold(ctx.Audience, strings.TrimSpace(val))
	}

	// label.key==value
	if strings.HasPrefix(cond, "label.") {
		rest := strings.TrimPrefix(cond, "label.")
		if idx := strings.Index(rest, "=="); idx >= 0 {
			key := rest[:idx]
			val := rest[idx+2:]
			return ctx.Labels[key] == val
		}
	}

	// Unrecognized condition — default to true (permissive).
	return true
}
