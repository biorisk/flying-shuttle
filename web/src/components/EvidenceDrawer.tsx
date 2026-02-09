import { useNodeStore } from "../stores/nodeStore";

export default function EvidenceDrawer() {
  const { selected } = useNodeStore();

  if (!selected) {
    return (
      <p className="pane-placeholder">Select a node to view its evidence and chunks.</p>
    );
  }

  return (
    <div className="evidence-drawer">
      <h3>{selected.title}</h3>
      <dl className="evidence-meta">
        <dt>Type</dt>
        <dd>{selected.type}</dd>
        <dt>Version</dt>
        <dd>{selected.version}</dd>
        <dt>Locked</dt>
        <dd>{selected.locked ? "Yes" : "No"}</dd>
      </dl>
      {selected.body && (
        <section className="evidence-body">
          <h4>Body</h4>
          <p>{selected.body}</p>
        </section>
      )}
      {selected.labels && Object.keys(selected.labels).length > 0 && (
        <section className="evidence-labels">
          <h4>Labels</h4>
          <ul>
            {Object.entries(selected.labels).map(([k, v]) => (
              <li key={k}>
                <code>{k}</code>: {v}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
