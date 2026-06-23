import { Bell } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";

export function Alarms() {
  return (
    <>
      <TopBar title="Alarms" />
      <div className="flex-1 overflow-auto">
        <EmptyState
          icon={Bell}
          title="No alarms yet"
          hint="Motion events and alerts will appear here once motion detection is enabled (coming in the next phase)."
        />
      </div>
    </>
  );
}
