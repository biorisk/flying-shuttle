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
