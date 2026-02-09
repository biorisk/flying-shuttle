// Ghost Proposal types for ambient RAG suggestions.

export interface GhostProposal {
  id: string;
  label: string;
  chunkCount: number;
  confidence: number;
  chunkIds: string[];
}
