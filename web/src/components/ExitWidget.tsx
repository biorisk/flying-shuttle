import { useCallback, useEffect, useState } from "react";
import { useOutlineStore } from "../stores/outlineStore";
import { useGhostStore } from "../stores/ghostStore";
import { edges as edgesApi } from "../services/api";

interface ExitWidgetProps {
  nodeId: string;
  depth: number;
}

export default function ExitWidget({ nodeId, depth }: ExitWidgetProps) {
  const allEdges = useOutlineStore((s) => s.allEdges);
  const allNodes = useOutlineStore((s) => s.allNodes);
  const fetchOutline = useOutlineStore((s) => s.fetchOutline);
  const proposals = useGhostStore((s) => s.proposals[nodeId] ?? []);

  const [adding, setAdding] = useState(false);
  const [exitType, setExitType] = useState<"linear" | "branch">("linear");

  // Get existing exits (outgoing edges from this node).
  const exits = allEdges.filter((e) => e.from_node === nodeId);

  const getNodeTitle = useCallback(
    (id: string) => {
      const n = allNodes.find((n) => n.id === id);
      return n?.title || "(untitled)";
    },
    [allNodes],
  );

  // Available nodes for creating new exits (exclude self and existing targets).
  const existingTargets = new Set(exits.map((e) => e.to_node));
  const availableNodes = allNodes.filter(
    (n) => n.id !== nodeId && !existingTargets.has(n.id) && n.type === "outline",
  );

  const [selectedTarget, setSelectedTarget] = useState("");

  useEffect(() => {
    if (availableNodes.length > 0 && !selectedTarget) {
      setSelectedTarget(availableNodes[0].id);
    }
  }, [availableNodes, selectedTarget]);

  const handleAddExit = useCallback(async () => {
    if (!selectedTarget) return;
    try {
      await edgesApi.create({
        from_node: nodeId,
        to_node: selectedTarget,
        type: exitType,
        weight: exits.length,
      });
      await fetchOutline();
      setAdding(false);
      setSelectedTarget("");
    } catch {
      // Edge creation may fail (cycle), silently handled.
    }
  }, [nodeId, selectedTarget, exitType, exits.length, fetchOutline]);

  const handleRemoveExit = useCallback(
    async (edgeId: string) => {
      try {
        await edgesApi.delete(edgeId);
        await fetchOutline();
      } catch {
        // silently ignore
      }
    },
    [fetchOutline],
  );

  const handleAddFromProposal = useCallback(
    async (targetNodeId: string) => {
      try {
        await edgesApi.create({
          from_node: nodeId,
          to_node: targetNodeId,
          type: "branch",
          weight: exits.length,
        });
        await fetchOutline();
      } catch {
        // silently ignore
      }
    },
    [nodeId, exits.length, fetchOutline],
  );

  if (exits.length === 0 && !adding && proposals.length === 0) {
    return (
      <div className="exit-widget" style={{ paddingLeft: depth * 24 + 24 }}>
        <button
          className="exit-add-btn"
          onClick={() => setAdding(true)}
          title="Add continuation"
        >
          + Add Exit
        </button>
      </div>
    );
  }

  return (
    <div className="exit-widget" style={{ paddingLeft: depth * 24 + 24 }}>
      {exits.length > 0 && (
        <div className="exit-list">
          {exits.map((e) => (
            <div key={e.id} className={`exit-item exit-item--${e.type}`}>
              <span className="exit-type-badge">{e.type === "linear" ? "\u2192" : "\u2194"}</span>
              <span className="exit-target">{getNodeTitle(e.to_node)}</span>
              {e.condition && (
                <span className="exit-condition" title={e.condition}>
                  if: {e.condition}
                </span>
              )}
              <button
                className="exit-remove-btn"
                onClick={() => handleRemoveExit(e.id)}
                title="Remove exit"
              >
                &#x2715;
              </button>
            </div>
          ))}
        </div>
      )}

      {proposals.length > 0 && (
        <div className="exit-proposals">
          <span className="exit-proposals-label">Suggested continuations:</span>
          {proposals.slice(0, 3).map((p) => (
            <button
              key={p.id}
              className="exit-proposal-btn"
              onClick={() => {
                // If proposal references a real node, link to it.
                const targetNode = allNodes.find((n) =>
                  n.title.toLowerCase().includes(p.label.split(" — ")[0].toLowerCase()),
                );
                if (targetNode) {
                  handleAddFromProposal(targetNode.id);
                }
              }}
              title={`${p.chunkCount} chunks, ${Math.round(p.confidence * 100)}% confidence`}
            >
              {p.label}
            </button>
          ))}
        </div>
      )}

      {!adding ? (
        <button
          className="exit-add-btn"
          onClick={() => setAdding(true)}
          title="Add continuation"
        >
          +
        </button>
      ) : (
        <div className="exit-add-form">
          <select
            className="exit-type-select"
            value={exitType}
            onChange={(e) => setExitType(e.target.value as "linear" | "branch")}
          >
            <option value="linear">Linear (auto-transition)</option>
            <option value="branch">Branch (reader chooses)</option>
          </select>
          <select
            className="exit-target-select"
            value={selectedTarget}
            onChange={(e) => setSelectedTarget(e.target.value)}
          >
            {availableNodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.title || "(untitled)"}
              </option>
            ))}
          </select>
          <button className="exit-confirm-btn" onClick={handleAddExit}>
            &#x2713;
          </button>
          <button className="exit-cancel-btn" onClick={() => setAdding(false)}>
            &#x2715;
          </button>
        </div>
      )}
    </div>
  );
}
