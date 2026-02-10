import { useCallback, useEffect, useRef } from "react";
import { useDraggable, useDroppable } from "@dnd-kit/core";
import type { TreeNode, DiffStatus, DiffGhostNode } from "../stores/outlineStore";
import { flattenTree, useOutlineStore } from "../stores/outlineStore";
import { useNodeStore } from "../stores/nodeStore";
import { useGhostStore } from "../stores/ghostStore";
import { useThreadStore } from "../stores/threadStore";
import GhostSubList from "./GhostSubList";
import ExitWidget from "./ExitWidget";
import type { DropTarget } from "./LivingOutline";

interface BulletItemProps {
  treeNode: TreeNode;
  activeId: string | null;
  dropTarget: DropTarget | null;
  threadActive: boolean | null; // null = no thread selected, true = in thread, false = dimmed
  diffStatus?: DiffStatus | null;
  isGhost?: boolean;
  onRescue?: () => void;
}

export default function BulletItem({ treeNode, activeId, dropTarget, threadActive, diffStatus, isGhost, onRescue }: BulletItemProps) {
  const { node, children, depth } = treeNode;
  const inputRef = useRef<HTMLInputElement>(null);

  const {
    collapsed,
    focusId,
    tree,
    addSibling,
    indent,
    unindent,
    updateTitle,
    removeNode,
    toggleLock,
    toggleCollapse,
    setFocus,
    contextWarnings,
    clearContextWarning,
  } = useOutlineStore();
  const diffActive = useOutlineStore((s) => s.diffActive);
  const diffNodeStatus = useOutlineStore((s) => s.diffNodeStatus);
  const diffGhostNodes = useOutlineStore((s) => s.diffGhostNodes);
  const rescueNode = useOutlineStore((s) => s.rescueNode);
  const { selectNode } = useNodeStore();
  const fetchProposals = useGhostStore((s) => s.fetchProposals);
  const toggleNodeInThread = useThreadStore((s) => s.toggleNodeInThread);
  const threadSelected = useThreadStore((s) => s.selected);
  const threadNodeIds = useThreadStore((s) => s.threadNodeIds);
  const brushMode = useThreadStore((s) => s.brushMode);
  const brushOrder = useThreadStore((s) => s.brushOrder);
  const brushPaint = useThreadStore((s) => s.brushPaint);

  const isCollapsed = collapsed[node.id] ?? false;
  const hasChildren = children.length > 0;
  const isFocused = focusId === node.id;
  const isLocked = node.locked;
  const chunkCount = node.labels?._chunkCount ? parseInt(node.labels._chunkCount, 10) : 0;
  const isDragging = activeId === node.id;
  const contextWarning = contextWarnings[node.id] ?? null;
  const brushIndex = brushMode ? brushOrder.indexOf(node.id) : -1;
  const isPainted = brushIndex >= 0;

  // dnd-kit hooks
  const {
    attributes,
    listeners,
    setNodeRef: setDragRef,
    transform,
  } = useDraggable({ id: node.id });

  const { setNodeRef: setDropRef } = useDroppable({ id: node.id });

  // Combine refs for the bullet row.
  const rowRef = useCallback(
    (el: HTMLDivElement | null) => {
      setDragRef(el);
      setDropRef(el);
    },
    [setDragRef, setDropRef]
  );

  // Determine which drop indicator to show.
  const showDropBefore =
    dropTarget?.nodeId === node.id && dropTarget.zone === "before";
  const showDropAfter =
    dropTarget?.nodeId === node.id && dropTarget.zone === "after";
  const showDropChild =
    dropTarget?.nodeId === node.id && dropTarget.zone === "child";

  // Auto-focus when this bullet becomes the focus target.
  useEffect(() => {
    if (isFocused && inputRef.current && !isDragging) {
      inputRef.current.focus();
      const len = inputRef.current.value.length;
      inputRef.current.setSelectionRange(len, len);
    }
  }, [isFocused, isDragging]);

  // Ambient fetch: load proposals when bullet is focused and has content.
  useEffect(() => {
    if (isFocused && node.title.trim().length >= 3) {
      const timer = setTimeout(() => {
        fetchProposals(node.id, node.title);
      }, 600);
      return () => clearTimeout(timer);
    }
  }, [isFocused, node.id, node.title, fetchProposals]);

  const handleKeyDown = useCallback(
    async (e: React.KeyboardEvent<HTMLInputElement>) => {
      const value = inputRef.current?.value ?? "";

      // Cmd+J / Ctrl+J — explicitly trigger ghost proposals.
      if (e.key === "j" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        fetchProposals(node.id, value);
        return;
      }

      // Cmd+L / Ctrl+L — toggle lock.
      if (e.key === "l" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        await toggleLock(node.id);
        return;
      }

      if (e.key === "Enter") {
        e.preventDefault();
        await addSibling(node.id);
        return;
      }

      if (e.key === "Tab" && !e.shiftKey) {
        e.preventDefault();
        await indent(node.id);
        setFocus(node.id);
        return;
      }

      if (e.key === "Tab" && e.shiftKey) {
        e.preventDefault();
        await unindent(node.id);
        setFocus(node.id);
        return;
      }

      if (e.key === "Backspace" && value === "") {
        e.preventDefault();
        await removeNode(node.id);
        return;
      }

      if (e.key === "ArrowUp" || e.key === "ArrowDown") {
        const flat = flattenTree(tree, collapsed);
        const idx = flat.findIndex((tn) => tn.node.id === node.id);
        const targetIdx = e.key === "ArrowUp" ? idx - 1 : idx + 1;
        if (targetIdx >= 0 && targetIdx < flat.length) {
          e.preventDefault();
          setFocus(flat[targetIdx].node.id);
        }
      }
    },
    [
      node.id,
      tree,
      collapsed,
      addSibling,
      indent,
      unindent,
      removeNode,
      toggleLock,
      setFocus,
      fetchProposals,
    ]
  );

  const handleBlur = useCallback(() => {
    const value = inputRef.current?.value ?? "";
    if (value !== node.title) {
      updateTitle(node.id, value);
    }
  }, [node.id, node.title, updateTitle]);

  const handleClick = useCallback(() => {
    if (brushMode && threadSelected) {
      brushPaint(node.id);
      return;
    }
    selectNode(node.id);
    setFocus(node.id);
  }, [node.id, selectNode, setFocus, brushMode, threadSelected, brushPaint]);

  const handleTriggerGhosts = useCallback(() => {
    const value = inputRef.current?.value ?? node.title;
    fetchProposals(node.id, value);
    setFocus(node.id);
  }, [node.id, node.title, fetchProposals, setFocus]);

  const bulletRowClass = [
    "bullet-row",
    isFocused ? "focused" : "",
    isLocked ? "locked" : "",
    isDragging ? "dragging" : "",
    showDropChild ? "drop-child" : "",
    threadActive === false ? "thread-dimmed" : "",
    threadActive === true ? "thread-active" : "",
    brushMode && isPainted ? "brush-painted" : "",
    brushMode ? "brush-mode" : "",
    diffStatus === "added" ? "diff-added" : "",
    diffStatus === "changed" ? "diff-changed" : "",
    diffStatus === "removed" ? "diff-removed" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const dragStyle = transform
    ? { transform: `translate3d(${transform.x}px, ${transform.y}px, 0)` }
    : undefined;

  // Ghost node: simplified read-only row
  if (isGhost) {
    return (
      <div className="bullet-tree-node">
        <div
          className="bullet-row diff-ghost"
          style={{ paddingLeft: depth * 24 }}
        >
          <span className="bullet-dot" />
          <span className="diff-ghost-title">{node.title || "Untitled"}</span>
          {onRescue && (
            <button className="diff-rescue-btn" onClick={onRescue}>
              Rescue
            </button>
          )}
        </div>
      </div>
    );
  }

  // Compute ghost children for this node
  const ghostChildren = diffActive
    ? diffGhostNodes.filter((g) => g.originalParentId === node.id)
    : [];

  return (
    <div className="bullet-tree-node">
      {showDropBefore && (
        <div
          className="drop-indicator"
          style={{ marginLeft: depth * 24 + 24 }}
        />
      )}
      <div
        ref={rowRef}
        className={bulletRowClass}
        style={{ paddingLeft: depth * 24, ...dragStyle }}
        {...attributes}
      >
        <span className="drag-handle" {...listeners} title="Drag to reorder">
          &#x2630;
        </span>
        {hasChildren ? (
          <button
            className="bullet-toggle"
            onClick={() => toggleCollapse(node.id)}
            tabIndex={-1}
          >
            {isCollapsed ? "+" : "-"}
          </button>
        ) : (
          <span className={`bullet-dot ${isLocked ? "locked" : ""}`} />
        )}
        {brushMode && isPainted && (
          <span className="brush-order-badge">{brushIndex + 1}</span>
        )}
        <input
          ref={inputRef}
          className={`bullet-input ${isLocked ? "locked" : ""}`}
          type="text"
          defaultValue={node.title}
          placeholder="Type here..."
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          onClick={handleClick}
          readOnly={isLocked}
        />
        {isLocked && chunkCount > 0 && (
          <span className="source-badge" title={`Backed by ${chunkCount} chunks`}>
            {chunkCount}
          </span>
        )}
        {isLocked && (
          <button
            className="lock-indicator"
            onClick={() => toggleLock(node.id)}
            title="Unlock bullet (Cmd+L)"
            tabIndex={-1}
          >
            &#x1F512;
          </button>
        )}
        {!isLocked && (
          <>
            <button
              className="lock-btn"
              onClick={() => toggleLock(node.id)}
              title="Lock bullet (Cmd+L)"
              tabIndex={-1}
            >
              &#x1F513;
            </button>
            <button
              className="ghost-trigger"
              onClick={handleTriggerGhosts}
              title="Get suggestions (Cmd+J)"
              tabIndex={-1}
            >
              +
            </button>
          </>
        )}
        {contextWarning && (
          <span
            className="context-warning"
            title={`${contextWarning} — click to dismiss`}
            onClick={() => clearContextWarning(node.id)}
          >
            {contextWarning}
          </span>
        )}
        {threadSelected && (
          <button
            className={`thread-toggle ${threadActive ? "in-thread" : ""}`}
            onClick={() => toggleNodeInThread(node.id)}
            title={threadActive ? "Remove from thread" : "Add to thread"}
            tabIndex={-1}
          >
            {threadActive ? "&#x25C9;" : "&#x25CB;"}
          </button>
        )}
      </div>
      {showDropAfter && (
        <div
          className="drop-indicator"
          style={{ marginLeft: depth * 24 + 24 }}
        />
      )}
      {!isLocked && <GhostSubList nodeId={node.id} depth={depth} />}
      {isLocked && <ExitWidget nodeId={node.id} depth={depth} />}
      {hasChildren && !isCollapsed && (
        <div className="bullet-children">
          {children.map((child) => (
            <BulletItem
              key={child.node.id}
              treeNode={child}
              activeId={activeId}
              dropTarget={dropTarget}
              threadActive={threadSelected ? threadNodeIds.has(child.node.id) : null}
              diffStatus={diffActive ? diffNodeStatus.get(child.node.id) ?? null : null}
            />
          ))}
        </div>
      )}
      {ghostChildren.map((ghost) => (
        <BulletItem
          key={`ghost-${ghost.node.id}`}
          treeNode={{ ...ghost, depth: depth + 1 }}
          activeId={null}
          dropTarget={null}
          threadActive={null}
          isGhost
          onRescue={() => rescueNode(ghost)}
        />
      ))}
    </div>
  );
}
