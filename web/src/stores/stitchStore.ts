import { create } from "zustand";
import type { LinearizeResult } from "../types/model";
import { threads as threadsApi, dag as dagApi } from "../services/api";

type ViewMode = "thread" | "manuscript";

interface StitchState {
  result: LinearizeResult | null;
  viewMode: ViewMode;
  threadId: string | null;
  glueLevel: number;
  loading: boolean;
  error: string | null;

  setViewMode: (mode: ViewMode) => void;
  setThreadId: (id: string | null) => void;
  setGlueLevel: (level: number) => void;
  fetchStitch: () => Promise<void>;
}

export const useStitchStore = create<StitchState>((set, get) => ({
  result: null,
  viewMode: "manuscript",
  threadId: null,
  glueLevel: 50,
  loading: false,
  error: null,

  setViewMode: (mode: ViewMode) => {
    set({ viewMode: mode });
  },

  setThreadId: (id: string | null) => {
    set({ threadId: id, viewMode: id ? "thread" : "manuscript" });
  },

  setGlueLevel: (level: number) => {
    set({ glueLevel: Math.max(0, Math.min(100, level)) });
  },

  fetchStitch: async () => {
    const { viewMode, threadId, glueLevel } = get();
    set({ loading: true, error: null });
    try {
      let result: LinearizeResult;
      if (viewMode === "thread" && threadId) {
        result = await threadsApi.linearize(threadId, glueLevel);
      } else {
        result = await dagApi.linearize(glueLevel);
      }
      set({ result, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },
}));
