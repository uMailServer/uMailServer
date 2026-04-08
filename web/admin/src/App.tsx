import { useState, useEffect } from "react";
import { Routes, Route, Navigate } from "react-router-dom";
import { ThemeProvider } from "@/components/theme-provider";
import { Layout } from "@/components/layout";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { Login } from "@/pages/Login";
import { Dashboard } from "@/pages/Dashboard";
import { Domains } from "@/pages/Domains";
import { Accounts } from "@/pages/Accounts";
import { Queue } from "@/pages/Queue";
import { SettingsPage } from "@/pages/Settings";
import { useWebSocket } from "@/hooks/useWebSocket";
import type { User, Activity, RealtimeMetrics } from "@/types";

function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [activities, setActivities] = useState<Activity[]>([]);
  const [metrics, setMetrics] = useState<RealtimeMetrics | undefined>();

  // Check for existing token on mount
  useEffect(() => {
    const storedToken = localStorage.getItem("adminToken");
    if (storedToken) {
      setToken(storedToken);
      setIsAuthenticated(true);
      // TODO: Validate token and get user info
      setUser({ email: "admin@example.com", isAdmin: true });
    }
  }, []);

  // WebSocket connection for realtime updates
  const { isConnected } = useWebSocket(token, {
    onMetrics: (newMetrics) => {
      setMetrics(newMetrics);
    },
    onActivity: (activity) => {
      setActivities((prev) => [activity, ...prev].slice(0, 50));
    },
  });

  const handleLogin = (newToken: string, userData: { email: string }) => {
    localStorage.setItem("adminToken", newToken);
    setToken(newToken);
    setIsAuthenticated(true);
    setUser({ email: userData.email, isAdmin: true });
  };

  const handleLogout = () => {
    localStorage.removeItem("adminToken");
    setToken(null);
    setIsAuthenticated(false);
    setUser(null);
    setActivities([]);
    setMetrics(undefined);
  };

  if (!isAuthenticated) {
    return (
      <ThemeProvider defaultTheme="system" storageKey="umail-admin-theme">
        <TooltipProvider>
          <Login onLogin={handleLogin} />
        </TooltipProvider>
      </ThemeProvider>
    );
  }

  return (
    <ThemeProvider defaultTheme="system" storageKey="umail-admin-theme">
      <TooltipProvider>
        <Layout user={user} onLogout={handleLogout} isConnected={isConnected}>
          <Routes>
            <Route
              path="/"
              element={
                <Dashboard
                  isConnected={isConnected}
                  metrics={metrics}
                  activities={activities}
                />
              }
            />
            <Route path="/domains" element={<Domains />} />
            <Route path="/accounts" element={<Accounts />} />
            <Route path="/queue" element={<Queue />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Layout>
        <Toaster />
      </TooltipProvider>
    </ThemeProvider>
  );
}

export default App;
