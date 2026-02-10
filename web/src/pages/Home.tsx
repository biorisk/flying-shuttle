import { useCallback, useRef, useState } from "react";
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
import TriFoldLayout from "../components/TriFoldLayout";
import SourceVault from "../components/SourceVault";
import LivingOutline from "../components/LivingOutline";
import EvidenceDrawer from "../components/EvidenceDrawer";
import StitchView from "../components/StitchView";
import RibbonDragString from "../components/RibbonDragString";
import { useOutlineStore, type TreeNode } from "../stores/outlineStore";
import { nodes as nodesApi, edges as edgesApi } from "../services/api";
import type { Chunk } from "../types/model";
import type { DropTarget } from "../components/LivingOutline";

type CenterView = "outline" | "stitch";

function isChunkDrag(active: DragStartEvent["active"]): boolean {
  return active.data.current?.type === "chunk";
}

export default function Home() {
  const [centerView, setCenterView] = useState<CenterView>("outline");

  // Shared DnD state for both outline reordering and chunk-to-outline drops.
  const [activeId, setActiveId] = useState<string | null>(null);
  const [dragType, setDragType] = useState<"outline" | "chunk" | null>(null);
  const [dropTarget, setDropTarget] = useState<DropTarget | null>(null);
  const [chunkDragOrigin, setChunkDragOrigin] = useState<DOMRect | null>(null);
  const [chunkDragColor, setChunkDragColor] = useState<string | null>(null);
  const [chunkDragLabel, setChunkDragLabel] = useState<string>("");
  const lastOverRef = useRef<DragOverEvent | null>(null);

  const { tree, moveNode, fetchOutline } = useOutlineStore();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

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
    [tree],
  );

  const computeDropZone = useCallback(
    (overEvent: DragOverEvent): DropTarget | null => {
      const overId = overEvent.over?.id as string | undefined;
      if (!overId || !activeId || overId === activeId) return null;

      const overNode = findTreeNode(overId);
      if (!overNode) return null;

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
    [activeId, findTreeNode],
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const id = event.active.id as string;
    setActiveId(id);

    if (isChunkDrag(event.active)) {
      setDragType("chunk");
      const data = event.active.data.current!;
      setChunkDragColor(data.color ?? null);
      const chunk = data.chunk as Chunk;
      setChunkDragLabel(
        chunk.content.length > 60
          ? chunk.content.slice(0, 60) + "..."
          : chunk.content,
      );
      // Capture the originating ribbon element's position for the string.
      const el = document.querySelector(
        `[data-rbd-draggable-id="${id}"], [id="${id}"]`,
      ) as HTMLElement | null;
      if (event.active.rect.current.initial) {
        setChunkDragOrigin(event.active.rect.current.initial);
      } else if (el) {
        setChunkDragOrigin(el.getBoundingClientRect());
      }
    } else {
      setDragType("outline");
    }
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      lastOverRef.current = event;
      setDropTarget(computeDropZone(event));
    },
    [computeDropZone],
  );

  const handleDragEnd = useCallback(
    async (event: DragEndEvent) => {
      const draggedId = event.active.id as string;
      const target = dropTarget;
      const type = dragType;

      // Reset all drag state.
      setActiveId(null);
      setDragType(null);
      setDropTarget(null);
      setChunkDragOrigin(null);
      setChunkDragColor(null);
      setChunkDragLabel("");
      lastOverRef.current = null;

      if (!target) return;

      if (type === "chunk") {
        // Chunk dropped onto an outline node — create a chunk_ref node.
        const chunk = event.active.data.current?.chunk as Chunk | undefined;
        if (!chunk) return;

        const targetNode = findTreeNode(target.nodeId);
        if (!targetNode) return;

        try {
          // Create a chunk_ref node with a title preview.
          const preview =
            chunk.content.length > 80
              ? chunk.content.slice(0, 80) + "..."
              : chunk.content;
          const node = await nodesApi.create({
            title: preview,
            type: "chunk_ref",
            locked: true,
            labels: { _chunkCount: "1" },
          });

          // Link the chunk to the new node.
          await nodesApi.setChunks(node.id, [chunk.id]);

          // Place it in the outline relative to the drop target.
          if (target.zone === "child") {
            await edgesApi.create({
              from_node: target.nodeId,
              to_node: node.id,
              type: "linear",
              weight: targetNode.children.length,
            });
          } else {
            const parentId = targetNode.parentId;
            const parentNode = parentId ? findTreeNode(parentId) : null;
            const siblings = parentNode ? parentNode.children : tree;
            const targetIdx = siblings.findIndex(
              (tn) => tn.node.id === target.nodeId,
            );
            const position =
              target.zone === "before" ? targetIdx : targetIdx + 1;

            if (parentId) {
              await edgesApi.create({
                from_node: parentId,
                to_node: node.id,
                type: "linear",
                weight: position,
              });
            }
            // If root level, no edge needed.
          }

          await fetchOutline();
        } catch {
          // Silently handle errors.
        }
        return;
      }

      // Outline node reordering.
      if (draggedId === target.nodeId) return;

      const targetNode = findTreeNode(target.nodeId);
      if (!targetNode) return;

      if (target.zone === "child") {
        await moveNode(draggedId, target.nodeId, 0);
      } else {
        const parentId = targetNode.parentId;
        const parentNode = parentId ? findTreeNode(parentId) : null;
        const siblings = parentNode ? parentNode.children : tree;
        const targetIdx = siblings.findIndex(
          (tn) => tn.node.id === target.nodeId,
        );
        const position =
          target.zone === "before" ? targetIdx : targetIdx + 1;
        await moveNode(draggedId, parentId, position);
      }
    },
    [dropTarget, dragType, findTreeNode, moveNode, fetchOutline, tree],
  );

  const handleDragCancel = useCallback(() => {
    setActiveId(null);
    setDragType(null);
    setDropTarget(null);
    setChunkDragOrigin(null);
    setChunkDragColor(null);
    setChunkDragLabel("");
    lastOverRef.current = null;
  }, []);

  // Resolve the active outline node for the drag overlay.
  const activeOutlineNode = dragType === "outline" && activeId ? findTreeNode(activeId) : null;

  const centerTitle = (
    <span className="center-view-tabs">
      <button
        className={`center-tab ${centerView === "outline" ? "active" : ""}`}
        onClick={() => setCenterView("outline")}
      >
        Outline
      </button>
      <button
        className={`center-tab ${centerView === "stitch" ? "active" : ""}`}
        onClick={() => setCenterView("stitch")}
      >
        Preview
      </button>
    </span>
  );

  return (
    <DndContext
      sensors={sensors}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <TriFoldLayout
        left={<SourceVault />}
        center={
          centerView === "outline" ? (
            <LivingOutline activeId={activeId} dropTarget={dropTarget} />
          ) : (
            <StitchView />
          )
        }
        right={<EvidenceDrawer />}
        centerTitle={centerTitle}
      />

      {/* Drag overlay for outline nodes */}
      <DragOverlay dropAnimation={null}>
        {activeOutlineNode && (
          <div className="drag-overlay">
            <span className="bullet-dot" />
            <span className="drag-overlay-label">
              {activeOutlineNode.node.title || "(untitled)"}
            </span>
          </div>
        )}
        {dragType === "chunk" && (
          <div
            className="drag-overlay drag-overlay--chunk"
            style={{ borderLeftColor: chunkDragColor ?? undefined }}
          >
            <span className="drag-overlay-label">{chunkDragLabel}</span>
          </div>
        )}
      </DragOverlay>

      {/* String connecting dragged chunk back to its ribbon position */}
      {dragType === "chunk" && chunkDragOrigin && (
        <RibbonDragString origin={chunkDragOrigin} color={chunkDragColor} />
      )}
    </DndContext>
  );
}
