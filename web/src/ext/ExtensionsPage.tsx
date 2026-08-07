import { useParams } from "react-router-dom";
import { Puzzle } from "lucide-react";
import { EmptyState } from "../components/ui/EmptyState";
import { ExtensionPanel } from "./ExtensionSlot";
import { panelsForSlot } from "./panels";
import { useExtPanels } from "./useExtPanels";

// ExtensionsPage hosts a pinned extension's full-page (nav-panel) view at
// /extensions/:id. The rail entry that links here is user-granted (see NavRail), so
// with nothing pinned this route is simply never linked. /extensions with no id
// falls back to the first nav-panel, if any.
export function ExtensionsPage() {
  const { id } = useParams();
  const { data: panels = [], isLoading } = useExtPanels();
  const navPanels = panelsForSlot(panels, "nav-panel");
  const panel = id ? navPanels.find((p) => p.extId === id) : navPanels[0];

  if (!panel) {
    return (
      <div className="flex-1 overflow-auto p-4">
        {isLoading ? null : (
          <EmptyState
            icon={Puzzle}
            title="Extension not available"
            hint="This extension isn't installed or enabled. Manage extensions in Settings → Extensions."
          />
        )}
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-auto p-4">
      <h1 className="mb-3 text-lg font-medium text-fg">{panel.title || panel.extName}</h1>
      <ExtensionPanel panel={panel} />
    </div>
  );
}
