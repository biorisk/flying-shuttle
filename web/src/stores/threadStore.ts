import { create } from "zustand";
import type { Thread } from "../types/model";
import { threads as api } from "../services/api";

interface ThreadState {
  threads: Thread[];
  selected: Thread | null;
  loading: boolean;
  error: string | null;
  fetchThreads: () => Promise<void>;
  selectThread: (id: string) => Promise<void>;
  createThread: (thread: Partial<Thread>) => Promise<Thread>;
  updateThread: (id: string, thread: Partial<Thread>) => Promise<void>;
  deleteThread: (id: string) => Promise<void>;
}

export const useThreadStore = create<ThreadState>((set, get) => ({
  threads: [],
  selected: null,
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

  selectThread: async (id: string) => {
    const cached = get().threads.find((t) => t.id === id);
    if (cached) {
      set({ selected: cached });
      return;
    }
    try {
      const thread = await api.get(id);
      set({ selected: thread });
    } catch (e) {
      set({ error: (e as Error).message });
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
    }));
  },
}));
