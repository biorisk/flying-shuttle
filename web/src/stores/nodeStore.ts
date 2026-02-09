import { create } from "zustand";
import type { Node } from "../types/model";
import { nodes as api } from "../services/api";

interface NodeState {
  nodes: Node[];
  selected: Node | null;
  loading: boolean;
  error: string | null;
  fetchNodes: () => Promise<void>;
  selectNode: (id: string) => Promise<void>;
  createNode: (node: Partial<Node>) => Promise<Node>;
  updateNode: (id: string, node: Partial<Node>) => Promise<void>;
  deleteNode: (id: string) => Promise<void>;
}

export const useNodeStore = create<NodeState>((set, get) => ({
  nodes: [],
  selected: null,
  loading: false,
  error: null,

  fetchNodes: async () => {
    set({ loading: true, error: null });
    try {
      const nodes = await api.list();
      set({ nodes, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  selectNode: async (id: string) => {
    const cached = get().nodes.find((n) => n.id === id);
    if (cached) {
      set({ selected: cached });
      return;
    }
    try {
      const node = await api.get(id);
      set({ selected: node });
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  createNode: async (node: Partial<Node>) => {
    const created = await api.create(node);
    set((s) => ({ nodes: [...s.nodes, created] }));
    return created;
  },

  updateNode: async (id: string, node: Partial<Node>) => {
    const updated = await api.update(id, node);
    set((s) => ({
      nodes: s.nodes.map((n) => (n.id === id ? updated : n)),
      selected: s.selected?.id === id ? updated : s.selected,
    }));
  },

  deleteNode: async (id: string) => {
    await api.delete(id);
    set((s) => ({
      nodes: s.nodes.filter((n) => n.id !== id),
      selected: s.selected?.id === id ? null : s.selected,
    }));
  },
}));
