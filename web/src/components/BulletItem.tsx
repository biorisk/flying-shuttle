import { useCallback, useEffect, useRef } from "react";
import type { TreeNode } from "../stores/outlineStore";
import { flattenTree, useOutlineStore } from "../stores/outlineStore";
import { useNodeStore } from "../stores/nodeStore";

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

  const isCollapsed = collapsed[node.id] ?? false;
  const hasChildren = children.length > 0;

  // Auto-focus when this bullet becomes the focus target.
  useEffect(() => {
    if (focusId === node.id && inputRef.current) {
      inputRef.current.focus();
      // Place cursor at end.
      const len = inputRef.current.value.length;
      inputRef.current.setSelectionRange(len, len);
    }
  }, [focusId, node.id]);

  const handleKeyDown = useCallback(
    async (e: React.KeyboardEvent<HTMLInputElement>) => {
      const value = inputRef.current?.value ?? "";

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

      // Arrow keys for navigation between bullets.
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
    [node.id, tree, collapsed, addSibling, indent, unindent, removeNode, setFocus]
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

  return (
    <div className="bullet-tree-node">
      <div
        className={`bullet-row ${focusId === node.id ? "focused" : ""}`}
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
      </div>
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
