import { useCallback } from "react";
import type { GhostProposal } from "../types/ghost";
import { useGhostStore } from "../stores/ghostStore";
import { useOutlineStore } from "../stores/outlineStore";
import { nodes as nodesApi } from "../services/api";

interface GhostSubListProps {
  nodeId: string;
  depth: number;
}

export default function GhostSubList({ nodeId, depth }: GhostSubListProps) {
  const proposals = useGhostStore((s) => s.proposals[nodeId] ?? []);
  const loading = useGhostStore((s) => s.loading[nodeId] ?? false);
  const dismissProposal = useGhostStore((s) => s.dismissProposal);
  const rejectProposal = useGhostStore((s) => s.rejectProposal);
  const addChild = useOutlineStore((s) => s.addChild);
  const updateTitle = useOutlineStore((s) => s.updateTitle);
  const fetchOutline = useOutlineStore((s) => s.fetchOutline);

  const handleAccept = useCallback(
    async (proposal: GhostProposal) => {
      const newId = await addChild(nodeId);
      if (newId) {
        await updateTitle(newId, proposal.label);
        // Lock the accepted node and store chunk count in labels.
        const node = useOutlineStore.getState().allNodes.find((n) => n.id === newId);
        if (node) {
          await nodesApi.update(newId, {
            ...node,
            locked: true,
            labels: {
              ...node.labels,
              _chunkCount: String(proposal.chunkCount),
              _sourceProposal: proposal.id,
            },
          });
          // Associate chunks with the node so they're tracked as "used".
          if (proposal.chunkIds.length > 0) {
            await nodesApi.setChunks(newId, proposal.chunkIds);
          }
          await fetchOutline();
        }
      }
      dismissProposal(nodeId, proposal.id);
    },
    [nodeId, addChild, updateTitle, dismissProposal, fetchOutline]
  );

  const handleReject = useCallback(
    (proposal: GhostProposal) => {
      rejectProposal(nodeId, proposal.id, proposal.label);
    },
    [nodeId, rejectProposal]
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
            &#x2713;
          </button>
          <button
            className="ghost-dismiss"
            onClick={() => handleReject(p)}
            title="Reject and suppress similar"
          >
            &#x2717;
          </button>
        </div>
      ))}
    </div>
  );
}
