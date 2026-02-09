import { useCallback, useEffect, useRef } from "react";
import type { TreeNode } from "../stores/outlineStore";
import { flattenTree, useOutlineStore } from "../stores/outlineStore";
import { useNodeStore } from "../stores/nodeStore";
import { useGhostStore } from "../stores/ghostStore";
import GhostSubList from "./GhostSubList";

interface BulletItemProps {
  treeNode: TreeNode;
}

export default function BulletItem({ treeNode }: BulletItemProps) {
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
  } = useOutlineStore();
  const { selectNode } = useNodeStore();
  const fetchProposals = useGhostStore((s) => s.fetchProposals);

  const isCollapsed = collapsed[node.id] ?? false;
  const hasChildren = children.length > 0;
  const isFocused = focusId === node.id;
  const isLocked = node.locked;
  const chunkCount = node.labels?._chunkCount ? parseInt(node.labels._chunkCount, 10) : 0;

  // Auto-focus when this bullet becomes the focus target.
  useEffect(() => {
    if (isFocused && inputRef.current) {
      inputRef.current.focus();
      const len = inputRef.current.value.length;
      inputRef.current.setSelectionRange(len, len);
    }
  }, [isFocused]);

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
    selectNode(node.id);
    setFocus(node.id);
  }, [node.id, selectNode, setFocus]);

  const handleTriggerGhosts = useCallback(() => {
    const value = inputRef.current?.value ?? node.title;
    fetchProposals(node.id, value);
    setFocus(node.id);
  }, [node.id, node.title, fetchProposals, setFocus]);

  const bulletRowClass = [
    "bullet-row",
    isFocused ? "focused" : "",
    isLocked ? "locked" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="bullet-tree-node">
      <div className={bulletRowClass} style={{ paddingLeft: depth * 24 }}>
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
      </div>
      {!isLocked && <GhostSubList nodeId={node.id} depth={depth} />}
      {hasChildren && !isCollapsed && (
        <div className="bullet-children">
          {children.map((child) => (
            <BulletItem key={child.node.id} treeNode={child} />
          ))}
        </div>
      )}
    </div>
  );
}
