import { Suspense, lazy } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./lib/auth";
import { usePrefs } from "./lib/prefs";
import { AppShell } from "./components/AppShell";
import { DeviceNotifications } from "./components/DeviceNotifications";
import { ChangeFeedListener } from "./components/ChangeFeedListener";
import { Login } from "./screens/Login";
import { Setup } from "./screens/Setup";
import { FirstRun } from "./screens/FirstRun";
import { Dashboard } from "./screens/Dashboard";
import { Cameras } from "./screens/Cameras";
import { Alarms } from "./screens/Alarms";
import { Settings } from "./screens/Settings";
import { ExtensionsPage } from "./ext/ExtensionsPage";
import { Spinner } from "./components/ui/Spinner";

// Playback pulls in hls.js; load it only when visited.
const Playback = lazy(() =>
  import("./screens/Playback").then((m) => ({ default: m.Playback })),
);

export default function App() {
  const { user, loading, needsSetup } = useAuth();
  const { prefs } = usePrefs();

  if (loading) {
    return (
      <div className="h-full grid place-items-center bg-ink-900">
        <Spinner label="Loading Aperture" />
      </div>
    );
  }

  if (needsSetup) return <Setup />;
  if (!user) return <Login />;
  if (user.mustChangePassword) return <FirstRun />;

  return (
    <AppShell>
      <DeviceNotifications />
      <ChangeFeedListener />
      <Suspense
        fallback={
          <div className="flex-1 grid place-items-center">
            <Spinner />
          </div>
        }
      >
        <Routes>
          <Route path="/" element={<Navigate to={`/${prefs.landing}`} replace />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/live" element={<Navigate to="/dashboard" replace />} />
          <Route path="/cameras" element={<Cameras />} />
          <Route path="/playback" element={<Playback />} />
          <Route path="/recordings" element={<Navigate to="/playback" replace />} />
          <Route path="/alarms" element={<Alarms />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="/extensions" element={<ExtensionsPage />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </Suspense>
    </AppShell>
  );
}
