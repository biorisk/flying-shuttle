import { useCallback, useEffect, useState } from "react";
import { useNodeStore } from "../stores/nodeStore";
import { uploads as uploadsApi } from "../services/api";
import type { Upload } from "../types/model";
import AudioUpload from "./AudioUpload";
import AudioRibbon from "./AudioRibbon";

export default function SourceVault() {
  const { nodes, loading, fetchNodes } = useNodeStore();
  const [recentUploads, setRecentUploads] = useState<Upload[]>([]);

  useEffect(() => {
    fetchNodes();
    uploadsApi.list().then(setRecentUploads).catch(() => {});
  }, [fetchNodes]);

  const onUploaded = useCallback((u: Upload) => {
    setRecentUploads((prev) => [u, ...prev]);
  }, []);

  if (loading) return <p className="pane-placeholder">Loading chunks...</p>;

  const chunkRefs = nodes.filter((n) => n.type === "chunk_ref");

  return (
    <div className="source-vault">
      <AudioUpload onUploaded={onUploaded} />
      <AudioRibbon />

      {recentUploads.length > 0 && (
        <div className="upload-list">
          <h4 className="section-heading">Uploads</h4>
          <ul className="source-list">
            {recentUploads.map((u) => (
              <li key={u.id} className="source-item upload-item">
                <span className="upload-filename">{u.filename}</span>
                <span className={`upload-badge upload-badge--${u.status}`}>
                  {u.status}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {chunkRefs.length > 0 && (
        <div className="chunk-list">
          <h4 className="section-heading">Source Chunks</h4>
          <ul className="source-list">
            {chunkRefs.map((n) => (
              <li key={n.id} className="source-item">
                {n.title || "(untitled chunk)"}
              </li>
            ))}
          </ul>
        </div>
      )}

      {chunkRefs.length === 0 && recentUploads.length === 0 && (
        <p className="pane-placeholder">No source material yet</p>
      )}
    </div>
  );
}
