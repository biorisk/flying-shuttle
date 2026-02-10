import { useState } from "react";
import TriFoldLayout from "../components/TriFoldLayout";
import SourceVault from "../components/SourceVault";
import LivingOutline from "../components/LivingOutline";
import EvidenceDrawer from "../components/EvidenceDrawer";
import StitchView from "../components/StitchView";

type CenterView = "outline" | "stitch";

export default function Home() {
  const [centerView, setCenterView] = useState<CenterView>("outline");

  const centerTitle = (
    <span className="center-view-tabs">
      <button
        className={`center-tab ${centerView === "outline" ? "active" : ""}`}
        onClick={() => setCenterView("outline")}
      >
        Outline
      </button>
      <button
        className={`center-tab ${centerView === "stitch" ? "active" : ""}`}
        onClick={() => setCenterView("stitch")}
      >
        Preview
      </button>
    </span>
  );

  return (
    <TriFoldLayout
      left={<SourceVault />}
      center={centerView === "outline" ? <LivingOutline /> : <StitchView />}
      right={<EvidenceDrawer />}
      centerTitle={centerTitle}
    />
  );
}
