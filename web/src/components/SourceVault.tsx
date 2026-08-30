import { useCallback, useEffect, useState } from "react";
import { useNodeStore } from "../stores/nodeStore";
import { uploads as uploadsApi } from "../services/api";
import type { Upload } from "../types/model";
import AudioUpload from "./AudioUpload";
import AudioRibbon from "./AudioRibbon";
import SearchBox from "./SearchBox";

const UPLOADS_PAGE = 100;

export default function SourceVault() {
  const { nodes, loading, fetchNodes } = useNodeStore();
  const [recentUploads, setRecentUploads] = useState<Upload[]>([]);
  const [uploadTotal, setUploadTotal] = useState(0);

  useEffect(() => {
    fetchNodes();
    uploadsApi
      .list({ limit: UPLOADS_PAGE })
      .then((page) => {
        setRecentUploads(page.items);
        setUploadTotal(page.total);
      })
      .catch(() => {});
  }, [fetchNodes]);

  const onUploaded = useCallback((u: Upload) => {
    setRecentUploads((prev) => [u, ...prev]);
    setUploadTotal((n) => n + 1);
  }, []);

  if (loading) return <p className="pane-placeholder">Loading chunks...</p>;

  const chunkRefs = nodes.filter((n) => n.type === "chunk_ref");

  return (
    <div className="source-vault">
      <AudioUpload onUploaded={onUploaded} />
      <SearchBox />
      <AudioRibbon />

      {recentUploads.length > 0 && (
        <div className="upload-list">
          <h4 className="section-heading">
            Uploads
            <span className="section-count">
              {uploadTotal > recentUploads.length
                ? `${recentUploads.length} / ${uploadTotal}`
                : recentUploads.length}
            </span>
          </h4>
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
