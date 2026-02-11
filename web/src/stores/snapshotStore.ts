import { create } from "zustand";
import type { SnapshotSummary } from "../types/model";
import { snapshots as api } from "../services/api";

interface SnapshotState {
  snapshots: SnapshotSummary[];
  saving: boolean;
  restoring: boolean;
  error: string | null;

  fetchSnapshots: () => Promise<void>;
  createSnapshot: (label: string) => Promise<SnapshotSummary | null>;
  deleteSnapshot: (id: string) => Promise<void>;
  restoreSnapshot: (id: string) => Promise<boolean>;
}

export const useSnapshotStore = create<SnapshotState>((set) => ({
  snapshots: [],
  saving: false,
  restoring: false,
  error: null,

  fetchSnapshots: async () => {
    try {
      const snapshots = await api.list();
      set({ snapshots, error: null });
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  createSnapshot: async (label: string) => {
    set({ saving: true, error: null });
    try {
      const summary = await api.create(label);
      set((s) => ({
        snapshots: [summary, ...s.snapshots],
        saving: false,
      }));
      return summary;
    } catch (e) {
      set({ error: (e as Error).message, saving: false });
      return null;
    }
  },

  deleteSnapshot: async (id: string) => {
    try {
      await api.delete(id);
      set((s) => ({
        snapshots: s.snapshots.filter((snap) => snap.id !== id),
        error: null,
      }));
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  restoreSnapshot: async (id: string) => {
    set({ restoring: true, error: null });
    try {
      await api.restore(id);
      set({ restoring: false });
      return true;
    } catch (e) {
      set({ error: (e as Error).message, restoring: false });
      return false;
    }
  },
}));
