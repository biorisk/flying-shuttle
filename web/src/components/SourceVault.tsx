import { useEffect } from "react";
import { useNodeStore } from "../stores/nodeStore";

export default function SourceVault() {
  const { nodes, loading, fetchNodes } = useNodeStore();

  useEffect(() => {
    fetchNodes();
  }, [fetchNodes]);

  if (loading) return <p className="pane-placeholder">Loading chunks...</p>;

  return (
    <div className="source-vault">
      {nodes.length === 0 ? (
        <p className="pane-placeholder">No source material yet</p>
      ) : (
        <ul className="source-list">
          {nodes
            .filter((n) => n.type === "chunk_ref")
            .map((n) => (
              <li key={n.id} className="source-item">
                {n.title || "(untitled chunk)"}
              </li>
            ))}
        </ul>
      )}
    </div>
  );
}
