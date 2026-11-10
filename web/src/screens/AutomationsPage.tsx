import { TopBar } from "../components/TopBar";
import { Automations } from "../components/Automations";

// Automations: rules that turn events into actions (notify, webhook, Home
// Assistant), as a page of its own like the alarm manager it is modelled on.
export function AutomationsPage() {
  return (
    <>
      <TopBar title="Automations" />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="px-4 py-5 lg:px-6 lg:py-6">
          <Automations />
        </div>
      </div>
    </>
  );
}
