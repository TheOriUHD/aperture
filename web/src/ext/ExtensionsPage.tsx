import { ExtensionSlot } from "./ExtensionSlot";

// ExtensionsPage hosts full-page (nav-panel) extension panels at /extensions. The
// nav entry linking here is rendered only when a nav-panel extension is installed
// (see NavRail), so with zero extensions the nav — and this route's reachability —
// are unchanged.
export function ExtensionsPage() {
  return (
    <div className="flex-1 overflow-auto p-4">
      <h1 className="mb-3 text-lg font-medium text-fg">Extensions</h1>
      <div className="space-y-3">
        <ExtensionSlot slot="nav-panel" />
      </div>
    </div>
  );
}
