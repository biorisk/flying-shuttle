import { useEffect } from "react";
import { useOutlineStore } from "../stores/outlineStore";
import BulletItem from "./BulletItem";

export default function LivingOutline() {
  const { tree, loading, fetchOutline, addRoot } = useOutlineStore();

  useEffect(() => {
    fetchOutline();
  }, [fetchOutline]);

  if (loading) return <p className="pane-placeholder">Loading outline...</p>;

  return (
    <div className="living-outline">
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
            <BulletItem key={tn.node.id} treeNode={tn} />
          ))}
          <button className="outline-add-root outline-add-root--subtle" onClick={addRoot}>
            +
          </button>
        </div>
      )}
    </div>
  );
}
