import { create } from "zustand";
import type { Edge, Node } from "../types/model";
import { edges as edgesApi, nodes as nodesApi } from "../services/api";

export interface TreeNode {
  node: Node;
  children: TreeNode[];
  parentId: string | null;
  depth: number;
}

interface OutlineState {
  tree: TreeNode[];
  allNodes: Node[];
  allEdges: Edge[];
  collapsed: Record<string, boolean>;
  focusId: string | null;
  loading: boolean;
  error: string | null;

  fetchOutline: () => Promise<void>;
  addSibling: (afterId: string) => Promise<string | null>;
  addChild: (parentId: string) => Promise<string | null>;
  addRoot: () => Promise<string | null>;
  indent: (nodeId: string) => Promise<void>;
  unindent: (nodeId: string) => Promise<void>;
  updateTitle: (nodeId: string, title: string) => Promise<void>;
  removeNode: (nodeId: string) => Promise<void>;
  toggleCollapse: (nodeId: string) => void;
  setFocus: (nodeId: string | null) => void;
}

function buildTree(nodes: Node[], edges: Edge[]): TreeNode[] {
  const nodeMap = new Map<string, Node>();
  const childrenMap = new Map<string, { nodeId: string; weight: number }[]>();
  const hasParent = new Set<string>();

  for (const n of nodes) {
    if (n.type === "outline") nodeMap.set(n.id, n);
  }

  for (const e of edges) {
    if (!nodeMap.has(e.from_node) || !nodeMap.has(e.to_node)) continue;
    if (e.type !== "linear") continue;
    const children = childrenMap.get(e.from_node) ?? [];
    children.push({ nodeId: e.to_node, weight: e.weight });
    childrenMap.set(e.from_node, children);
    hasParent.add(e.to_node);
  }

  function buildSubtree(id: string, parentId: string | null, depth: number): TreeNode {
    const children = (childrenMap.get(id) ?? [])
      .sort((a, b) => a.weight - b.weight)
      .map((c) => buildSubtree(c.nodeId, id, depth + 1));
    return { node: nodeMap.get(id)!, children, parentId, depth };
  }

  // Roots are outline nodes with no incoming linear edges.
  const roots = Array.from(nodeMap.keys()).filter((id) => !hasParent.has(id));
  return roots.map((id) => buildSubtree(id, null, 0));
}

// Flatten tree to ordered list for keyboard navigation.
export function flattenTree(
  tree: TreeNode[],
  collapsed: Record<string, boolean>
): TreeNode[] {
  const result: TreeNode[] = [];
  function walk(nodes: TreeNode[]) {
    for (const tn of nodes) {
      result.push(tn);
      if (!collapsed[tn.node.id]) walk(tn.children);
    }
  }
  walk(tree);
  return result;
}

function findInTree(tree: TreeNode[], id: string): TreeNode | null {
  for (const tn of tree) {
    if (tn.node.id === id) return tn;
    const found = findInTree(tn.children, id);
    if (found) return found;
  }
  return null;
}

function findParentAndIndex(
  tree: TreeNode[],
  id: string
): { parent: TreeNode | null; siblings: TreeNode[]; index: number } | null {
  for (let i = 0; i < tree.length; i++) {
    if (tree[i].node.id === id) return { parent: null, siblings: tree, index: i };
    const found = findParentAndIndex(tree[i].children, id);
    if (found) {
      if (found.parent === null)
        return { parent: tree[i], siblings: found.siblings, index: found.index };
      return found;
    }
  }
  return null;
}

export const useOutlineStore = create<OutlineState>((set, get) => ({
  tree: [],
  allNodes: [],
  allEdges: [],
  collapsed: {},
  focusId: null,
  loading: false,
  error: null,

  fetchOutline: async () => {
    set({ loading: true, error: null });
    try {
      const [nodes, edges] = await Promise.all([nodesApi.list(), edgesApi.list()]);
      const tree = buildTree(nodes, edges);
      set({ allNodes: nodes, allEdges: edges, tree, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  addRoot: async () => {
    try {
      const node = await nodesApi.create({ title: "", type: "outline" });
      set({ focusId: node.id });
      await get().fetchOutline();
      return node.id;
    } catch (e) {
      set({ error: (e as Error).message });
      return null;
    }
  },

  addSibling: async (afterId: string) => {
    const { tree } = get();
    const info = findParentAndIndex(tree, afterId);
    if (!info) return null;

    try {
      const node = await nodesApi.create({ title: "", type: "outline" });

      if (info.parent) {
        // Add edge from parent to new node, with weight after the sibling.
        const weight = info.index + 1;
        await edgesApi.create({
          from_node: info.parent.node.id,
          to_node: node.id,
          type: "linear",
          weight,
        });
        // Re-weight subsequent siblings.
        for (let i = info.index + 1; i < info.siblings.length; i++) {
          const sibEdge = get().allEdges.find(
            (e) =>
              e.from_node === info.parent!.node.id &&
              e.to_node === info.siblings[i].node.id &&
              e.type === "linear"
          );
          if (sibEdge) {
            await edgesApi.delete(sibEdge.id);
            await edgesApi.create({
              from_node: info.parent!.node.id,
              to_node: info.siblings[i].node.id,
              type: "linear",
              weight: i + 2,
            });
          }
        }
      }
      // If root level, no edge needed — it's another root.

      set({ focusId: node.id });
      await get().fetchOutline();
      return node.id;
    } catch (e) {
      set({ error: (e as Error).message });
      return null;
    }
  },

  addChild: async (parentId: string) => {
    try {
      const node = await nodesApi.create({ title: "", type: "outline" });
      const parentNode = findInTree(get().tree, parentId);
      const weight = parentNode ? parentNode.children.length : 0;
      await edgesApi.create({
        from_node: parentId,
        to_node: node.id,
        type: "linear",
        weight,
      });
      set({ focusId: node.id });
      await get().fetchOutline();
      return node.id;
    } catch (e) {
      set({ error: (e as Error).message });
      return null;
    }
  },

  indent: async (nodeId: string) => {
    const { tree, allEdges } = get();
    const info = findParentAndIndex(tree, nodeId);
    if (!info || info.index === 0) return; // Can't indent first sibling.

    const newParent = info.siblings[info.index - 1];
    try {
      // Remove old edge from current parent.
      if (info.parent) {
        const oldEdge = allEdges.find(
          (e) =>
            e.from_node === info.parent!.node.id &&
            e.to_node === nodeId &&
            e.type === "linear"
        );
        if (oldEdge) await edgesApi.delete(oldEdge.id);
      }

      // Add edge from new parent (sibling above) to this node.
      await edgesApi.create({
        from_node: newParent.node.id,
        to_node: nodeId,
        type: "linear",
        weight: newParent.children.length,
      });

      // Expand new parent so the indented node is visible.
      set((s) => ({
        collapsed: { ...s.collapsed, [newParent.node.id]: false },
      }));
      await get().fetchOutline();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  unindent: async (nodeId: string) => {
    const { tree, allEdges } = get();
    const info = findParentAndIndex(tree, nodeId);
    if (!info || !info.parent) return; // Already root, can't unindent.

    const grandparentInfo = findParentAndIndex(tree, info.parent.node.id);
    try {
      // Remove edge from current parent.
      const oldEdge = allEdges.find(
        (e) =>
          e.from_node === info.parent!.node.id &&
          e.to_node === nodeId &&
          e.type === "linear"
      );
      if (oldEdge) await edgesApi.delete(oldEdge.id);

      // Add edge from grandparent (if exists) to this node.
      if (grandparentInfo?.parent) {
        await edgesApi.create({
          from_node: grandparentInfo.parent.node.id,
          to_node: nodeId,
          type: "linear",
          weight: grandparentInfo.index + 1,
        });
      }
      // If grandparent is root level, node becomes root (no edge needed).

      await get().fetchOutline();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  updateTitle: async (nodeId: string, title: string) => {
    const node = get().allNodes.find((n) => n.id === nodeId);
    if (!node) return;
    try {
      await nodesApi.update(nodeId, { ...node, title });
      await get().fetchOutline();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  removeNode: async (nodeId: string) => {
    const { tree } = get();
    const flat = flattenTree(tree, {});
    const idx = flat.findIndex((tn) => tn.node.id === nodeId);

    try {
      await nodesApi.delete(nodeId);
      // Focus previous node or next node.
      const newFocus = idx > 0 ? flat[idx - 1].node.id : (flat[idx + 1]?.node.id ?? null);
      set({ focusId: newFocus });
      await get().fetchOutline();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  toggleCollapse: (nodeId: string) => {
    set((s) => ({
      collapsed: { ...s.collapsed, [nodeId]: !s.collapsed[nodeId] },
    }));
  },

  setFocus: (nodeId: string | null) => {
    set({ focusId: nodeId });
  },
}));
