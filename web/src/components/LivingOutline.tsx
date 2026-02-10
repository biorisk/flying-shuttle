import { useEffect } from "react";
import { useOutlineStore, type TreeNode } from "../stores/outlineStore";
import { useThreadStore } from "../stores/threadStore";
import BulletItem from "./BulletItem";
import ThreadSelector from "./ThreadSelector";
import SnapshotBar from "./SnapshotBar";

export interface DropTarget {
  nodeId: string;
  zone: "before" | "after" | "child";
}

interface LivingOutlineProps {
  activeId: string | null;
  dropTarget: DropTarget | null;
}

export default function LivingOutline({ activeId, dropTarget }: LivingOutlineProps) {
  const { tree, loading, fetchOutline, addRoot } = useOutlineStore();
  const diffActive = useOutlineStore((s) => s.diffActive);
  const diffNodeStatus = useOutlineStore((s) => s.diffNodeStatus);
  const diffGhostNodes = useOutlineStore((s) => s.diffGhostNodes);
  const rescueNode = useOutlineStore((s) => s.rescueNode);
  const threadSelected = useThreadStore((s) => s.selected);
  const threadNodeIds = useThreadStore((s) => s.threadNodeIds);

  useEffect(() => {
    fetchOutline();
  }, [fetchOutline]);

  if (loading) return <p className="pane-placeholder">Loading outline...</p>;

  return (
    <div className="living-outline">
      <ThreadSelector />
      <SnapshotBar />
      {tree.length === 0 ? (
        <div className="outline-empty">
          <p className="pane-placeholder">Start writing your outline.</p>
          <button className="outline-add-root" onClick={addRoot}>
            + Add first bullet
          </button>
        </div>
      ) : (
        <div className="bullet-tree">
          {tree.map((tn) => (
            <BulletItem
              key={tn.node.id}
              treeNode={tn}
              activeId={activeId}
              dropTarget={dropTarget}
              threadActive={threadSelected ? threadNodeIds.has(tn.node.id) : null}
              diffStatus={diffActive ? diffNodeStatus.get(tn.node.id) ?? null : null}
            />
          ))}
          {diffActive &&
            diffGhostNodes
              .filter((g) => g.originalParentId === null)
              .map((ghost) => (
                <BulletItem
                  key={`ghost-${ghost.node.id}`}
                  treeNode={{ ...ghost, depth: 0 }}
                  activeId={null}
                  dropTarget={null}
                  threadActive={null}
                  isGhost
                  onRescue={() => rescueNode(ghost)}
                />
              ))}
          <button
            className="outline-add-root outline-add-root--subtle"
            onClick={addRoot}
          >
            +
          </button>
        </div>
      )}
    </div>
  );
}
