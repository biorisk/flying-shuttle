import { create } from "zustand";
import type { Thread, ThreadNode } from "../types/model";
import { threads as api } from "../services/api";

interface ThreadState {
  threads: Thread[];
  selected: Thread | null;
  threadNodeIds: Set<string>; // node IDs belonging to the selected thread
  loading: boolean;
  error: string | null;

  fetchThreads: () => Promise<void>;
  selectThread: (id: string | null) => Promise<void>;
  createThread: (thread: Partial<Thread>) => Promise<Thread>;
  updateThread: (id: string, thread: Partial<Thread>) => Promise<void>;
  deleteThread: (id: string) => Promise<void>;
  toggleNodeInThread: (nodeId: string) => Promise<void>;
}

export const useThreadStore = create<ThreadState>((set, get) => ({
  threads: [],
  selected: null,
  threadNodeIds: new Set(),
  loading: false,
  error: null,

  fetchThreads: async () => {
    set({ loading: true, error: null });
    try {
      const threads = await api.list();
      set({ threads, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  selectThread: async (id: string | null) => {
    if (!id) {
      set({ selected: null, threadNodeIds: new Set() });
      return;
    }
    const cached = get().threads.find((t) => t.id === id);
    const thread = cached ?? (await api.get(id));
    if (!cached) {
      set((s) => ({ threads: [...s.threads, thread] }));
    }
    set({ selected: thread, loading: true });
    try {
      const threadNodes = await api.getNodes(id);
      set({
        threadNodeIds: new Set(threadNodes.map((tn: ThreadNode) => tn.node_id)),
        loading: false,
      });
    } catch {
      set({ threadNodeIds: new Set(), loading: false });
    }
  },

  createThread: async (thread: Partial<Thread>) => {
    const created = await api.create(thread);
    set((s) => ({ threads: [...s.threads, created] }));
    return created;
  },

  updateThread: async (id: string, thread: Partial<Thread>) => {
    const updated = await api.update(id, thread);
    set((s) => ({
      threads: s.threads.map((t) => (t.id === id ? updated : t)),
      selected: s.selected?.id === id ? updated : s.selected,
    }));
  },

  deleteThread: async (id: string) => {
    await api.delete(id);
    set((s) => ({
      threads: s.threads.filter((t) => t.id !== id),
      selected: s.selected?.id === id ? null : s.selected,
      threadNodeIds: s.selected?.id === id ? new Set() : s.threadNodeIds,
    }));
  },

  toggleNodeInThread: async (nodeId: string) => {
    const { selected, threadNodeIds } = get();
    if (!selected) return;

    const newIds = new Set(threadNodeIds);
    if (newIds.has(nodeId)) {
      newIds.delete(nodeId);
    } else {
      newIds.add(nodeId);
    }

    const threadNodes: ThreadNode[] = Array.from(newIds).map((nid, i) => ({
      thread_id: selected.id,
      node_id: nid,
      position: i,
    }));

    try {
      await api.setNodes(selected.id, threadNodes);
      set({ threadNodeIds: newIds });
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },
}));
