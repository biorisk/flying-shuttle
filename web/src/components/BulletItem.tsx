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
    toggleCollapse,
    setFocus,
  } = useOutlineStore();
  const { selectNode } = useNodeStore();
  const fetchProposals = useGhostStore((s) => s.fetchProposals);

  const isCollapsed = collapsed[node.id] ?? false;
  const hasChildren = children.length > 0;
  const isFocused = focusId === node.id;

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

  return (
    <div className="bullet-tree-node">
      <div
        className={`bullet-row ${isFocused ? "focused" : ""}`}
        style={{ paddingLeft: depth * 24 }}
      >
        {hasChildren ? (
          <button
            className="bullet-toggle"
            onClick={() => toggleCollapse(node.id)}
            tabIndex={-1}
          >
            {isCollapsed ? "+" : "-"}
          </button>
        ) : (
          <span className="bullet-dot" />
        )}
        <input
          ref={inputRef}
          className="bullet-input"
          type="text"
          defaultValue={node.title}
          placeholder="Type here..."
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          onClick={handleClick}
        />
        <button
          className="ghost-trigger"
          onClick={handleTriggerGhosts}
          title="Get suggestions (Cmd+J)"
          tabIndex={-1}
        >
          +
        </button>
      </div>
      <GhostSubList nodeId={node.id} depth={depth} />
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
