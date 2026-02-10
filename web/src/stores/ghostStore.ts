import { create } from "zustand";
import type { GhostProposal } from "../types/ghost";
import { nodes as nodesApi } from "../services/api";

// Generate proposals via the backend suggest endpoint. Falls back to stub
// proposals when the backend returns empty results (e.g. no indexed chunks).
async function generateProposals(
  nodeId: string,
  nodeTitle: string,
): Promise<GhostProposal[]> {
  if (!nodeTitle || nodeTitle.trim().length < 3) return [];

  // Try clustered suggestions first (groups chunks into sub-themes).
  try {
    const clusters = await nodesApi.suggestClusters(nodeId, 10);
    if (clusters && clusters.length > 0) {
      return clusters.map((c, i) => ({
        id: `cluster-${nodeId}-${i}`,
        label: c.label,
        chunkCount: c.chunk_count,
        confidence: c.confidence,
        chunkIds: c.chunk_ids,
      }));
    }
  } catch {
    // Fall through to individual suggestions.
  }

  // Fallback to individual chunk suggestions.
  try {
    const suggestions = await nodesApi.suggest(nodeId, 5);
    if (suggestions && suggestions.length > 0) {
      return suggestions.map((s, i) => ({
        id: `suggest-${nodeId}-${i}`,
        label: s.label,
        chunkCount: 1,
        confidence: s.confidence,
        chunkIds: [s.chunk_id],
      }));
    }
  } catch {
    // Backend not available — fall through to stub.
  }

  // Stub fallback for when the index is empty or backend is unavailable.
  await new Promise((r) => setTimeout(r, 300));
  const hash = nodeTitle.length + nodeTitle.charCodeAt(0);
  return [
    {
      id: `ghost-${hash}-1`,
      label: `${nodeTitle} — emotional context`,
      chunkCount: Math.max(2, (hash % 5) + 1),
      confidence: 0.85,
      chunkIds: [],
    },
    {
      id: `ghost-${hash}-2`,
      label: `${nodeTitle} — key evidence`,
      chunkCount: Math.max(1, (hash % 3) + 1),
      confidence: 0.72,
      chunkIds: [],
    },
    {
      id: `ghost-${hash}-3`,
      label: `${nodeTitle} — counterpoint`,
      chunkCount: Math.max(1, (hash % 4) + 1),
      confidence: 0.61,
      chunkIds: [],
    },
  ].filter((p) => p.confidence > 0.5);
}

interface GhostState {
  proposals: Record<string, GhostProposal[]>;
  loading: Record<string, boolean>;
  dismissed: Record<string, Set<string>>;
  // Rejected labels used to suppress similar future proposals.
  rejectedLabels: string[];

  fetchProposals: (nodeId: string, nodeTitle: string) => Promise<void>;
  dismissProposal: (nodeId: string, proposalId: string) => void;
  rejectProposal: (nodeId: string, proposalId: string, label: string) => void;
  clearProposals: (nodeId: string) => void;
}

function isSuppressed(label: string, rejectedLabels: string[]): boolean {
  const lower = label.toLowerCase();
  return rejectedLabels.some((rejected) => {
    const rejLower = rejected.toLowerCase();
    // Suppress if the proposal label contains the rejected label's core theme.
    // Extract the part after the dash (the theme descriptor).
    const dashIdx = rejLower.lastIndexOf("—");
    const theme = dashIdx >= 0 ? rejLower.slice(dashIdx + 1).trim() : rejLower;
    return theme.length > 3 && lower.includes(theme);
  });
}

export const useGhostStore = create<GhostState>((set, get) => ({
  proposals: {},
  loading: {},
  dismissed: {},
  rejectedLabels: [],

  fetchProposals: async (nodeId: string, nodeTitle: string) => {
    set((s) => ({ loading: { ...s.loading, [nodeId]: true } }));
    try {
      const proposals = await generateProposals(nodeId, nodeTitle);
      const { dismissed, rejectedLabels } = get();
      const dismissedSet = dismissed[nodeId] ?? new Set();
      const filtered = proposals.filter(
        (p) => !dismissedSet.has(p.id) && !isSuppressed(p.label, rejectedLabels),
      );
      set((s) => ({
        proposals: { ...s.proposals, [nodeId]: filtered },
        loading: { ...s.loading, [nodeId]: false },
      }));
    } catch {
      set((s) => ({ loading: { ...s.loading, [nodeId]: false } }));
    }
  },

  dismissProposal: (nodeId: string, proposalId: string) => {
    set((s) => {
      const dismissed = new Set(s.dismissed[nodeId] ?? []);
      dismissed.add(proposalId);
      const proposals = (s.proposals[nodeId] ?? []).filter((p) => p.id !== proposalId);
      return {
        dismissed: { ...s.dismissed, [nodeId]: dismissed },
        proposals: { ...s.proposals, [nodeId]: proposals },
      };
    });
  },

  rejectProposal: (nodeId: string, proposalId: string, label: string) => {
    set((s) => {
      const dismissed = new Set(s.dismissed[nodeId] ?? []);
      dismissed.add(proposalId);
      const proposals = (s.proposals[nodeId] ?? []).filter((p) => p.id !== proposalId);
      return {
        dismissed: { ...s.dismissed, [nodeId]: dismissed },
        proposals: { ...s.proposals, [nodeId]: proposals },
        rejectedLabels: [...s.rejectedLabels, label],
      };
    });
  },

  clearProposals: (nodeId: string) => {
    set((s) => ({
      proposals: { ...s.proposals, [nodeId]: [] },
    }));
  },
}));
