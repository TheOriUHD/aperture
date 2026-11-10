import { useParams } from "react-router-dom";
import { Puzzle } from "lucide-react";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { TopBar } from "../components/TopBar";
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
      <>
        <TopBar title="Extensions" />
        <div className="flex-1 overflow-auto p-4">
          {isLoading ? (
            <div className="grid place-items-center py-20">
              <Spinner label="Loading extension" />
            </div>
          ) : (
            <EmptyState
              icon={Puzzle}
              title="Extension not available"
              hint="This extension isn't installed or enabled. Manage extensions in Settings → Extensions."
            />
          )}
        </div>
      </>
    );
  }

  return (
    <>
      <TopBar title={panel.title || panel.extName} />
      <div className="flex-1 overflow-auto px-4 py-4 lg:px-6">
        <h1 className="mb-3 text-[15px] font-semibold text-fg lg:hidden">{panel.title || panel.extName}</h1>
        <ExtensionPanel panel={panel} />
      </div>
    </>
  );
}
