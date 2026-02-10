// Domain types mirroring the Go backend models.

export interface Node {
  id: string;
  type: NodeType;
  title: string;
  body: string;
  labels?: Record<string, string>;
  locked: boolean;
  version: number;
  created_at: string;
  updated_at: string;
}

export type NodeType = "outline" | "chunk_ref" | "synth";

export interface Edge {
  id: string;
  from_node: string;
  to_node: string;
  type: EdgeType;
  condition?: string;
  weight: number;
  created_at: string;
}

export type EdgeType = "linear" | "branch" | "jump";

export interface Thread {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface ThreadNode {
  thread_id: string;
  node_id: string;
  position: number;
}

export interface Chunk {
  id: string;
  source_file: string;
  content: string;
  start_offset: number;
  end_offset: number;
  speaker?: string;
  embedding_vec?: string;
  created_at: string;
}

export interface ValidationReport {
  valid: boolean;
  issues?: ValidationIssue[];
}

export interface ValidationIssue {
  type: string;
  message: string;
  id: string;
}

export type UploadStatus = "pending" | "transcribing" | "done" | "failed";

export interface Upload {
  id: string;
  filename: string;
  format: string;
  size_bytes: number;
  status: UploadStatus;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface TranscriptSegment {
  id: string;
  upload_id: string;
  speaker: string;
  text: string;
  start_ms: number;
  end_ms: number;
  created_at: string;
}

export interface SearchResult {
  chunk_id: string;
  score: number;
}

export interface ChunkSuggestion {
  chunk_id: string;
  label: string;
  score: number;
  confidence: number;
}

export interface ClusterSuggestion {
  label: string;
  chunk_ids: string[];
  chunk_count: number;
  confidence: number;
}

export type SpanType = "chunk" | "glue";

export interface StitchSpan {
  type: SpanType;
  chunk_index?: number;
  chunk_id?: string;
  text: string;
}

export interface StitchStats {
  chunk_chars: number;
  glue_chars: number;
  total_chars: number;
  glue_ratio: number;
}

export interface StitchResult {
  spans: StitchSpan[];
  text: string;
  stats: StitchStats;
}

export interface LinearizeResult {
  nodes: Node[];
  stitch: StitchResult;
}

// API envelope returned by all endpoints.
export interface ApiResponse<T> {
  data?: T;
  error?: string;
}
