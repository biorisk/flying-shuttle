import { useCallback } from "react";
import type { GhostProposal } from "../types/ghost";
import { useGhostStore } from "../stores/ghostStore";
import { useOutlineStore } from "../stores/outlineStore";

interface GhostSubListProps {
  nodeId: string;
  depth: number;
}

export default function GhostSubList({ nodeId, depth }: GhostSubListProps) {
  const proposals = useGhostStore((s) => s.proposals[nodeId] ?? []);
  const loading = useGhostStore((s) => s.loading[nodeId] ?? false);
  const dismissProposal = useGhostStore((s) => s.dismissProposal);
  const addChild = useOutlineStore((s) => s.addChild);
  const updateTitle = useOutlineStore((s) => s.updateTitle);

  const handleAccept = useCallback(
    async (proposal: GhostProposal) => {
      const newId = await addChild(nodeId);
      if (newId) {
        await updateTitle(newId, proposal.label);
      }
      dismissProposal(nodeId, proposal.id);
    },
    [nodeId, addChild, updateTitle, dismissProposal]
  );

  if (loading) {
    return (
      <div className="ghost-sublist" style={{ paddingLeft: (depth + 1) * 24 }}>
        <div className="ghost-loading">Thinking...</div>
      </div>
    );
  }

  if (proposals.length === 0) return null;

  return (
    <div className="ghost-sublist" style={{ paddingLeft: (depth + 1) * 24 }}>
      {proposals.map((p) => (
        <div key={p.id} className="ghost-item">
          <span className="ghost-dot" />
          <span className="ghost-label">{p.label}</span>
          <span className="ghost-meta">
            {p.chunkCount} chunk{p.chunkCount !== 1 ? "s" : ""}
          </span>
          <button
            className="ghost-accept"
            onClick={() => handleAccept(p)}
            title="Accept proposal"
          >
            +
          </button>
          <button
            className="ghost-dismiss"
            onClick={() => dismissProposal(nodeId, p.id)}
            title="Dismiss"
          >
            x
          </button>
        </div>
      ))}
    </div>
  );
}
