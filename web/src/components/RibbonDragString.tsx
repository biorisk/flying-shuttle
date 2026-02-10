import { useEffect, useState } from "react";

interface RibbonDragStringProps {
  origin: DOMRect;
  color: string | null;
}

/**
 * Renders an SVG line from the ribbon segment origin to the current pointer
 * position during a chunk drag. Creates the "string" visual described in
 * the Audio Ribbon spec.
 */
export default function RibbonDragString({ origin, color }: RibbonDragStringProps) {
  const [pointer, setPointer] = useState<{ x: number; y: number }>({
    x: origin.left + origin.width / 2,
    y: origin.top + origin.height / 2,
  });

  useEffect(() => {
    function onMove(e: PointerEvent) {
      setPointer({ x: e.clientX, y: e.clientY });
    }
    window.addEventListener("pointermove", onMove);
    return () => window.removeEventListener("pointermove", onMove);
  }, []);

  const ox = origin.left + origin.width / 2;
  const oy = origin.top + origin.height / 2;
  const strokeColor = color ?? "rgba(255,255,255,0.4)";

  return (
    <svg className="ribbon-drag-string">
      <line
        x1={ox}
        y1={oy}
        x2={pointer.x}
        y2={pointer.y}
        stroke={strokeColor}
        strokeWidth={2}
        strokeDasharray="6 4"
        opacity={0.7}
      />
      <circle cx={ox} cy={oy} r={4} fill={strokeColor} opacity={0.9} />
    </svg>
  );
}
