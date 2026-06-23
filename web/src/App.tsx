import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./lib/auth";
import { AppShell } from "./components/AppShell";
import { Login } from "./screens/Login";
import { Setup } from "./screens/Setup";
import { Dashboard } from "./screens/Dashboard";
import { Live } from "./screens/Live";
import { Cameras } from "./screens/Cameras";
import { Recordings } from "./screens/Recordings";
import { Alarms } from "./screens/Alarms";
import { Settings } from "./screens/Settings";
import { Spinner } from "./components/ui/Spinner";

export default function App() {
  const { user, loading, needsSetup } = useAuth();

  if (loading) {
    return (
      <div className="h-full grid place-items-center bg-ink-900">
        <Spinner label="Loading Aperture" />
      </div>
    );
  }

  if (needsSetup) return <Setup />;
  if (!user) return <Login />;

  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<Navigate to="/live" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/live" element={<Live />} />
        <Route path="/cameras" element={<Cameras />} />
        <Route path="/recordings" element={<Recordings />} />
        <Route path="/alarms" element={<Alarms />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/live" replace />} />
      </Routes>
    </AppShell>
  );
}
