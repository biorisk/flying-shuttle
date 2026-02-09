// API client for the Flying Shuttle Go backend.

import type {
  ApiResponse,
  Chunk,
  ChunkSuggestion,
  Edge,
  Node,
  SearchResult,
  Thread,
  ThreadNode,
  TranscriptSegment,
  Upload,
  ValidationReport,
} from "../types/model";

const BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (res.status === 204) return undefined as T;
  const body: ApiResponse<T> = await res.json();
  if (body.error) throw new Error(body.error);
  return body.data as T;
}

// --- Nodes ---

export const nodes = {
  list: () => request<Node[]>("/nodes"),
  get: (id: string) => request<Node>(`/nodes/${id}`),
  create: (node: Partial<Node>) =>
    request<Node>("/nodes", { method: "POST", body: JSON.stringify(node) }),
  update: (id: string, node: Partial<Node>) =>
    request<Node>(`/nodes/${id}`, { method: "PUT", body: JSON.stringify(node) }),
  delete: (id: string) => request<void>(`/nodes/${id}`, { method: "DELETE" }),
  getChunks: (id: string) => request<Chunk[]>(`/nodes/${id}/chunks`),
  setChunks: (id: string, chunkIds: string[]) =>
    request<void>(`/nodes/${id}/chunks`, {
      method: "PUT",
      body: JSON.stringify({ chunk_ids: chunkIds }),
    }),
  getEdges: (id: string) => request<Edge[]>(`/nodes/${id}/edges`),
  suggest: (id: string, limit = 5) =>
    request<ChunkSuggestion[]>(
      `/nodes/${id}/suggest?limit=${limit}`,
    ),
  move: (id: string, parentId: string | null, position: number) =>
    request<void>(`/nodes/${id}/move`, {
      method: "POST",
      body: JSON.stringify({ parent_id: parentId ?? "", position }),
    }),
};

// --- Edges ---

export const edges = {
  list: () => request<Edge[]>("/edges"),
  get: (id: string) => request<Edge>(`/edges/${id}`),
  create: (edge: Partial<Edge>) =>
    request<Edge>("/edges", { method: "POST", body: JSON.stringify(edge) }),
  delete: (id: string) => request<void>(`/edges/${id}`, { method: "DELETE" }),
};

// --- Threads ---

export const threads = {
  list: () => request<Thread[]>("/threads"),
  get: (id: string) => request<Thread>(`/threads/${id}`),
  create: (thread: Partial<Thread>) =>
    request<Thread>("/threads", {
      method: "POST",
      body: JSON.stringify(thread),
    }),
  update: (id: string, thread: Partial<Thread>) =>
    request<Thread>(`/threads/${id}`, {
      method: "PUT",
      body: JSON.stringify(thread),
    }),
  delete: (id: string) => request<void>(`/threads/${id}`, { method: "DELETE" }),
  getNodes: (id: string) => request<ThreadNode[]>(`/threads/${id}/nodes`),
  setNodes: (id: string, nodes: ThreadNode[]) =>
    request<void>(`/threads/${id}/nodes`, {
      method: "PUT",
      body: JSON.stringify({ nodes }),
    }),
  render: (id: string) => request<Node[]>(`/threads/${id}/render`),
};

// --- Chunks ---

export const chunks = {
  list: () => request<Chunk[]>("/chunks"),
  get: (id: string) => request<Chunk>(`/chunks/${id}`),
  create: (chunk: Partial<Chunk>) =>
    request<Chunk>("/chunks", { method: "POST", body: JSON.stringify(chunk) }),
};

// --- Uploads ---

export const uploads = {
  list: () => request<Upload[]>("/uploads"),
  get: (id: string) => request<Upload>(`/uploads/${id}`),
  create: async (file: File): Promise<Upload> => {
    const form = new FormData();
    form.append("file", file);
    const res = await fetch(`${BASE}/uploads`, { method: "POST", body: form });
    const body: ApiResponse<Upload> = await res.json();
    if (body.error) throw new Error(body.error);
    return body.data as Upload;
  },
  segments: (id: string) =>
    request<TranscriptSegment[]>(`/uploads/${id}/segments`),
};

// --- Search ---

export const search = {
  query: (q: string, limit = 10) =>
    request<SearchResult[]>(`/search?q=${encodeURIComponent(q)}&limit=${limit}`),
};

// --- DAG ---

export const dag = {
  validate: () => request<ValidationReport>("/dag/validate"),
  roots: () => request<Node[]>("/dag/roots"),
};
