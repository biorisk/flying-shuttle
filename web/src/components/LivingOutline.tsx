import { useCallback, useEffect, useRef, useState } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { useOutlineStore, type TreeNode } from "../stores/outlineStore";
import BulletItem from "./BulletItem";

export interface DropTarget {
  nodeId: string;
  zone: "before" | "after" | "child";
}

export default function LivingOutline() {
  const { tree, loading, fetchOutline, addRoot, moveNode } =
    useOutlineStore();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [dropTarget, setDropTarget] = useState<DropTarget | null>(null);
  const lastOverRef = useRef<DragOverEvent | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } })
  );

  useEffect(() => {
    fetchOutline();
  }, [fetchOutline]);

  const findTreeNode = useCallback(
    (id: string): TreeNode | null => {
      function search(nodes: TreeNode[]): TreeNode | null {
        for (const tn of nodes) {
          if (tn.node.id === id) return tn;
          const found = search(tn.children);
          if (found) return found;
        }
        return null;
      }
      return search(tree);
    },
    [tree]
  );

  const computeDropZone = useCallback(
    (overEvent: DragOverEvent): DropTarget | null => {
      const overId = overEvent.over?.id as string | undefined;
      if (!overId || !activeId || overId === activeId) return null;

      const overNode = findTreeNode(overId);
      if (!overNode) return null;

      // Use pointer Y relative to the over element to decide before/after/child.
      const overRect = overEvent.over?.rect;
      const pointerY = (overEvent.activatorEvent as PointerEvent)?.clientY;
      const delta = overEvent.delta;

      if (!overRect || pointerY == null) {
        return { nodeId: overId, zone: "after" };
      }

      const currentY = pointerY + (delta?.y ?? 0);
      const top = overRect.top;
      const height = overRect.height;
      const relY = (currentY - top) / height;

      if (relY < 0.25) return { nodeId: overId, zone: "before" };
      if (relY > 0.75) return { nodeId: overId, zone: "after" };
      return { nodeId: overId, zone: "child" };
    },
    [activeId, findTreeNode]
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    setActiveId(event.active.id as string);
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      lastOverRef.current = event;
      setDropTarget(computeDropZone(event));
    },
    [computeDropZone]
  );

  const handleDragEnd = useCallback(
    async (event: DragEndEvent) => {
      const draggedId = event.active.id as string;
      const target = dropTarget;

      setActiveId(null);
      setDropTarget(null);
      lastOverRef.current = null;

      if (!target || draggedId === target.nodeId) return;

      const targetNode = findTreeNode(target.nodeId);
      if (!targetNode) return;

      if (target.zone === "child") {
        // Drop as first child of the target node.
        await moveNode(draggedId, target.nodeId, 0);
      } else {
        // Drop before or after the target node among its siblings.
        const parentId = targetNode.parentId;

        // Find the target's position among siblings.
        const parentNode = parentId ? findTreeNode(parentId) : null;
        const siblings = parentNode ? parentNode.children : tree;
        const targetIdx = siblings.findIndex(
          (tn) => tn.node.id === target.nodeId
        );

        const position =
          target.zone === "before" ? targetIdx : targetIdx + 1;
        await moveNode(draggedId, parentId, position);
      }
    },
    [dropTarget, findTreeNode, moveNode, tree]
  );

  const handleDragCancel = useCallback(() => {
    setActiveId(null);
    setDropTarget(null);
    lastOverRef.current = null;
  }, []);

  const activeNode = activeId ? findTreeNode(activeId) : null;

  if (loading) return <p className="pane-placeholder">Loading outline...</p>;

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
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
              <BulletItem
                key={tn.node.id}
                treeNode={tn}
                activeId={activeId}
                dropTarget={dropTarget}
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
      <DragOverlay dropAnimation={null}>
        {activeNode && (
          <div className="drag-overlay">
            <span className="bullet-dot" />
            <span className="drag-overlay-label">
              {activeNode.node.title || "(untitled)"}
            </span>
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}
