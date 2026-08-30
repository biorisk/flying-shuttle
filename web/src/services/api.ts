// API client for the Flying Shuttle Go backend.

import { useUiStore } from "../stores/uiStore";
import type {
  ApiResponse,
  Branch,
  BranchSummary,
  Chunk,
  ChunkSuggestion,
  ClusterSuggestion,
  ContextCheck,
  Edge,
  ExportResult,
  LinearizeResult,
  Node,
  SearchResult,
  Snapshot,
  SnapshotSummary,
  StitchResult,
  Thread,
  ThreadNode,
  TranscriptSegment,
  Upload,
  ValidationReport,
} from "../types/model";

const BASE = "/api/v1";

// Page mirrors the { data, meta } envelope returned by list endpoints.
export interface Page<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

function isMutation(init?: RequestInit): boolean {
  const m = (init?.method ?? "GET").toUpperCase();
  return m !== "GET" && m !== "HEAD";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      headers: { "Content-Type": "application/json" },
      ...init,
    });
  } catch {
    // Network / server-down failures have no response body.
    if (isMutation(init)) {
      useUiStore.getState().notify("Network error — the server may be down");
    }
    throw new Error("network error");
  }
  if (res.status === 204) return undefined as T;
  const body: ApiResponse<T> = await res.json().catch(() => ({}) as ApiResponse<T>);
  if (!res.ok || body.error) {
    const msg = body.error || `${res.status} ${res.statusText}`;
    // Surface failed writes; silent GETs are handled per-pane.
    if (isMutation(init)) useUiStore.getState().notify(msg);
    throw new Error(msg);
  }
  return body.data as T;
}

// requestPage reads a paginated list response ({ data, meta }).
async function requestPage<T>(path: string): Promise<Page<T>> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
  });
  const body = (await res.json().catch(() => ({}))) as ApiResponse<T[]> & {
    meta?: { total: number; limit: number; offset: number };
  };
  if (!res.ok || body.error) {
    throw new Error(body.error || `${res.status} ${res.statusText}`);
  }
  const items = (body.data ?? []) as T[];
  return {
    items,
    total: body.meta?.total ?? items.length,
    limit: body.meta?.limit ?? items.length,
    offset: body.meta?.offset ?? 0,
  };
}

function pageQuery(opts?: { limit?: number; offset?: number }): string {
  const p = new URLSearchParams();
  if (opts?.limit != null) p.set("limit", String(opts.limit));
  if (opts?.offset != null) p.set("offset", String(opts.offset));
  const s = p.toString();
  return s ? `?${s}` : "";
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
  suggestClusters: (id: string, limit = 10) =>
    request<ClusterSuggestion[]>(
      `/nodes/${id}/suggest-clusters?limit=${limit}`,
    ),
  move: (id: string, parentId: string | null, position: number) =>
    request<void>(`/nodes/${id}/move`, {
      method: "POST",
      body: JSON.stringify({ parent_id: parentId ?? "", position }),
    }),
  checkContext: (id: string, parentId: string) =>
    request<ContextCheck>(`/nodes/${id}/check-context`, {
      method: "POST",
      body: JSON.stringify({ parent_id: parentId }),
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
  linearize: (id: string, glueLevel = 50) =>
    request<LinearizeResult>(
      `/threads/${id}/linearize?glue_level=${glueLevel}`,
    ),
};

// --- Chunks ---

export const chunks = {
  list: (opts?: { limit?: number; offset?: number }) =>
    requestPage<Chunk>(`/chunks${pageQuery(opts)}`),
  get: (id: string) => request<Chunk>(`/chunks/${id}`),
  create: (chunk: Partial<Chunk>) =>
    request<Chunk>("/chunks", { method: "POST", body: JSON.stringify(chunk) }),
};

// --- Uploads ---

export const uploads = {
  list: (opts?: { limit?: number; offset?: number }) =>
    requestPage<Upload>(`/uploads${pageQuery(opts)}`),
  get: (id: string) => request<Upload>(`/uploads/${id}`),
  create: async (
    file: File,
    opts?: { defer?: boolean },
  ): Promise<Upload> => {
    const form = new FormData();
    form.append("file", file);
    if (opts?.defer) form.append("defer", "1");
    let res: Response;
    try {
      res = await fetch(`${BASE}/uploads`, { method: "POST", body: form });
    } catch {
      useUiStore.getState().notify(`Upload failed: ${file.name}`);
      throw new Error("network error");
    }
    const body = (await res.json().catch(() => ({}))) as ApiResponse<Upload>;
    if (!res.ok || body.error) {
      const msg = body.error || `Upload failed: ${file.name}`;
      useUiStore.getState().notify(msg);
      throw new Error(msg);
    }
    return body.data as Upload;
  },
  // Start processing uploads created with { defer: true }. Omit ids to
  // process every pending upload.
  process: (ids?: string[]) =>
    request<{ started: number }>("/uploads/process", {
      method: "POST",
      body: JSON.stringify({ ids: ids ?? [] }),
    }),
  segments: (id: string) =>
    request<TranscriptSegment[]>(`/uploads/${id}/segments`),
};

// --- Search ---

export const search = {
  query: (q: string, limit = 10) =>
    request<SearchResult[]>(`/search?q=${encodeURIComponent(q)}&limit=${limit}`),
};

// --- Stitch ---

export const stitching = {
  stitch: (chunkIds: string[], glueLevel = 50) =>
    request<StitchResult>("/stitch", {
      method: "POST",
      body: JSON.stringify({ chunk_ids: chunkIds, glue_level: glueLevel }),
    }),
};

// --- Export ---

export const exporting = {
  markdown: (threadId?: string, glueLevel = 50, title = "Manuscript") =>
    request<ExportResult>("/export/markdown", {
      method: "POST",
      body: JSON.stringify({
        thread_id: threadId ?? "",
        glue_level: glueLevel,
        title,
      }),
    }),
  downloadUrl: (threadId?: string, title = "Manuscript") =>
    `${BASE}/export/markdown/download?thread_id=${encodeURIComponent(threadId ?? "")}&title=${encodeURIComponent(title)}`,
};

// --- Snapshots ---

export const snapshots = {
  list: () => request<SnapshotSummary[]>("/snapshots"),
  get: (id: string) => request<Snapshot>(`/snapshots/${id}`),
  create: (label: string) =>
    request<SnapshotSummary>("/snapshots", {
      method: "POST",
      body: JSON.stringify({ label }),
    }),
  delete: (id: string) =>
    request<void>(`/snapshots/${id}`, { method: "DELETE" }),
  restore: (id: string) =>
    request<void>(`/snapshots/${id}/restore`, { method: "POST" }),
};

// --- Branches ---

export const branches = {
  list: () => request<BranchSummary[]>("/branches"),
  get: (id: string) => request<Branch>(`/branches/${id}`),
  create: (name: string) =>
    request<BranchSummary>("/branches", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  update: (id: string, name: string) =>
    request<BranchSummary>(`/branches/${id}`, {
      method: "PUT",
      body: JSON.stringify({ name }),
    }),
  delete: (id: string) =>
    request<void>(`/branches/${id}`, { method: "DELETE" }),
  switchTo: (id: string) =>
    request<void>(`/branches/${id}/switch`, { method: "POST" }),
  active: () => request<BranchSummary | null>("/branches/active"),
};

// --- DAG ---

export const dag = {
  validate: () => request<ValidationReport>("/dag/validate"),
  roots: () => request<Node[]>("/dag/roots"),
  linearize: (glueLevel = 50) =>
    request<LinearizeResult>(`/dag/linearize?glue_level=${glueLevel}`),
};
