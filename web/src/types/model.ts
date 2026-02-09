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

// API envelope returned by all endpoints.
export interface ApiResponse<T> {
  data?: T;
  error?: string;
}
