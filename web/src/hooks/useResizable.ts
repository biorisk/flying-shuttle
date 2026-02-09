import { useCallback, useRef, useState } from "react";

interface UseResizableOptions {
  initialWidth: number;
  minWidth: number;
  maxWidth: number;
  direction: "left" | "right";
}

export function useResizable({
  initialWidth,
  minWidth,
  maxWidth,
  direction,
}: UseResizableOptions) {
  const [width, setWidth] = useState(initialWidth);
  const dragging = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(0);

  const onMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragging.current = true;
      startX.current = e.clientX;
      startWidth.current = width;

      const onMouseMove = (ev: MouseEvent) => {
        if (!dragging.current) return;
        const delta = ev.clientX - startX.current;
        const newWidth =
          direction === "left" ? startWidth.current + delta : startWidth.current - delta;
        setWidth(Math.max(minWidth, Math.min(maxWidth, newWidth)));
      };

      const onMouseUp = () => {
        dragging.current = false;
        document.removeEventListener("mousemove", onMouseMove);
        document.removeEventListener("mouseup", onMouseUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };

      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      document.addEventListener("mousemove", onMouseMove);
      document.addEventListener("mouseup", onMouseUp);
    },
    [width, minWidth, maxWidth, direction]
  );

  return { width, onMouseDown, setWidth };
}
