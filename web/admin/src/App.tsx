import { useState, useEffect } from "react";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
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

// Placeholder pages for routes not yet implemented
function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-center h-96">
      <div className="text-center">
        <h2 className="text-2xl font-bold mb-2">{title}</h2>
        <p className="text-muted-foreground">This page is under development</p>
      </div>
    </div>
  );
}

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

  const handleLogin = (newToken: string, userData: User) => {
    localStorage.setItem("adminToken", newToken);
    setToken(newToken);
    setIsAuthenticated(true);
    setUser(userData);
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
