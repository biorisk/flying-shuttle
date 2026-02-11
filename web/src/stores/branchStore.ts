import { create } from "zustand";
import type { BranchSummary, SnapshotData } from "../types/model";
import { branches as api } from "../services/api";

interface BranchState {
  branches: BranchSummary[];
  activeBranch: BranchSummary | null;
  switching: boolean;
  creating: boolean;
  comparing: boolean;
  compareData: SnapshotData | null;
  compareBranchId: string | null;
  error: string | null;

  fetchBranches: () => Promise<void>;
  createBranch: (name: string) => Promise<BranchSummary | null>;
  switchBranch: (id: string) => Promise<boolean>;
  renameBranch: (id: string, name: string) => Promise<void>;
  deleteBranch: (id: string) => Promise<boolean>;
  loadBranchForCompare: (id: string) => Promise<SnapshotData | null>;
  clearCompare: () => void;
}

export const useBranchStore = create<BranchState>((set, get) => ({
  branches: [],
  activeBranch: null,
  switching: false,
  creating: false,
  comparing: false,
  compareData: null,
  compareBranchId: null,
  error: null,

  fetchBranches: async () => {
    try {
      const branches = await api.list();
      const active = branches.find((b) => b.active) ?? null;
      set({ branches, activeBranch: active, error: null });
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  createBranch: async (name: string) => {
    set({ creating: true, error: null });
    try {
      const summary = await api.create(name);
      await get().fetchBranches();
      set({ creating: false });
      return summary;
    } catch (e) {
      set({ error: (e as Error).message, creating: false });
      return null;
    }
  },

  switchBranch: async (id: string) => {
    set({ switching: true, error: null });
    try {
      await api.switchTo(id);
      await get().fetchBranches();
      set({ switching: false });
      return true;
    } catch (e) {
      set({ error: (e as Error).message, switching: false });
      return false;
    }
  },

  renameBranch: async (id: string, name: string) => {
    try {
      await api.update(id, name);
      await get().fetchBranches();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },

  deleteBranch: async (id: string) => {
    try {
      await api.delete(id);
      await get().fetchBranches();
      return true;
    } catch (e) {
      set({ error: (e as Error).message });
      return false;
    }
  },

  loadBranchForCompare: async (id: string) => {
    set({ comparing: true, error: null });
    try {
      const branch = await api.get(id);
      set({ comparing: false, compareData: branch.data, compareBranchId: id });
      return branch.data;
    } catch (e) {
      set({ error: (e as Error).message, comparing: false });
      return null;
    }
  },

  clearCompare: () => {
    set({ compareData: null, compareBranchId: null });
  },
}));
