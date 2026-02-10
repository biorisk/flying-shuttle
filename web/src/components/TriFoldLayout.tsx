import { useState, type ReactNode } from "react";
import { useResizable } from "../hooks/useResizable";

interface TriFoldLayoutProps {
  left: ReactNode;
  center: ReactNode;
  right: ReactNode;
  leftTitle?: string;
  centerTitle?: ReactNode;
  rightTitle?: string;
}

const LEFT_DEFAULT = 280;
const RIGHT_DEFAULT = 300;
const MIN_PANE = 180;
const MAX_PANE = 500;

export default function TriFoldLayout({
  left,
  center,
  right,
  leftTitle = "Source Vault",
  centerTitle = "Living Outline",
  rightTitle = "Evidence Drawer",
}: TriFoldLayoutProps) {
  const [leftCollapsed, setLeftCollapsed] = useState(false);
  const [rightCollapsed, setRightCollapsed] = useState(false);

  const leftPane = useResizable({
    initialWidth: LEFT_DEFAULT,
    minWidth: MIN_PANE,
    maxWidth: MAX_PANE,
    direction: "left",
  });

  const rightPane = useResizable({
    initialWidth: RIGHT_DEFAULT,
    minWidth: MIN_PANE,
    maxWidth: MAX_PANE,
    direction: "right",
  });

  return (
    <div className="trifold">
      {/* Left pane */}
      {!leftCollapsed && (
        <>
          <aside className="trifold-pane trifold-left" style={{ width: leftPane.width }}>
            <div className="trifold-pane-header">
              <span className="trifold-pane-title">{leftTitle}</span>
              <button
                className="trifold-collapse-btn"
                onClick={() => setLeftCollapsed(true)}
                title="Collapse"
              >
                ‹
              </button>
            </div>
            <div className="trifold-pane-content">{left}</div>
          </aside>
          <div className="trifold-handle" onMouseDown={leftPane.onMouseDown} />
        </>
      )}

      {/* Center pane */}
      <main className="trifold-pane trifold-center">
        <div className="trifold-pane-header">
          {leftCollapsed && (
            <button
              className="trifold-collapse-btn"
              onClick={() => setLeftCollapsed(false)}
              title="Expand left pane"
            >
              ›
            </button>
          )}
          <span className="trifold-pane-title">{centerTitle}</span>
          {rightCollapsed && (
            <button
              className="trifold-collapse-btn"
              onClick={() => setRightCollapsed(false)}
              title="Expand right pane"
            >
              ‹
            </button>
          )}
        </div>
        <div className="trifold-pane-content">{center}</div>
      </main>

      {/* Right pane */}
      {!rightCollapsed && (
        <>
          <div className="trifold-handle" onMouseDown={rightPane.onMouseDown} />
          <aside
            className="trifold-pane trifold-right"
            style={{ width: rightPane.width }}
          >
            <div className="trifold-pane-header">
              <span className="trifold-pane-title">{rightTitle}</span>
              <button
                className="trifold-collapse-btn"
                onClick={() => setRightCollapsed(true)}
                title="Collapse"
              >
                ›
              </button>
            </div>
            <div className="trifold-pane-content">{right}</div>
          </aside>
        </>
      )}
    </div>
  );
}
