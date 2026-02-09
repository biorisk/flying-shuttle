import TriFoldLayout from "../components/TriFoldLayout";
import SourceVault from "../components/SourceVault";
import LivingOutline from "../components/LivingOutline";
import EvidenceDrawer from "../components/EvidenceDrawer";

export default function Home() {
  return (
    <TriFoldLayout
      left={<SourceVault />}
      center={<LivingOutline />}
      right={<EvidenceDrawer />}
    />
  );
}
