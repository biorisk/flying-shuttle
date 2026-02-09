import { useEffect } from "react";
import { useNodeStore } from "../stores/nodeStore";

export default function LivingOutline() {
  const { nodes, loading, fetchNodes, selected, selectNode } = useNodeStore();

  useEffect(() => {
    fetchNodes();
  }, [fetchNodes]);

  if (loading) return <p className="pane-placeholder">Loading outline...</p>;

  const outlineNodes = nodes.filter((n) => n.type === "outline");

  return (
    <div className="living-outline">
      {outlineNodes.length === 0 ? (
        <p className="pane-placeholder">No outline nodes yet. Create one to begin.</p>
      ) : (
        <ul className="outline-list">
          {outlineNodes.map((n) => (
            <li
              key={n.id}
              className={`outline-item ${selected?.id === n.id ? "selected" : ""}`}
              onClick={() => selectNode(n.id)}
            >
              <strong>{n.title || "(untitled)"}</strong>
              {n.body && <p className="outline-body">{n.body}</p>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
