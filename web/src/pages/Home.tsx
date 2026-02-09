import { useEffect } from "react";
import { useNodeStore } from "../stores/nodeStore";
import { useThreadStore } from "../stores/threadStore";

export default function Home() {
  const { nodes, loading: nodesLoading, fetchNodes } = useNodeStore();
  const { threads, loading: threadsLoading, fetchThreads } = useThreadStore();

  useEffect(() => {
    fetchNodes();
    fetchThreads();
  }, [fetchNodes, fetchThreads]);

  if (nodesLoading || threadsLoading) return <p>Loading...</p>;

  return (
    <div>
      <h1>Flying Shuttle</h1>
      <section>
        <h2>Nodes ({nodes.length})</h2>
        <ul>
          {nodes.map((n) => (
            <li key={n.id}>
              <strong>{n.title || "(untitled)"}</strong> <code>{n.type}</code>
            </li>
          ))}
        </ul>
      </section>
      <section>
        <h2>Threads ({threads.length})</h2>
        <ul>
          {threads.map((t) => (
            <li key={t.id}>{t.name}</li>
          ))}
        </ul>
      </section>
    </div>
  );
}
