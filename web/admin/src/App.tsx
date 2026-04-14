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

  // Check for existing session on mount
  // Token is now stored in HttpOnly cookie (more secure against XSS)
  useEffect(() => {
    // Check if user is already authenticated via cookie
    // The server will validate the cookie on API requests
    fetch('/api/v1/accounts', {
      credentials: 'include'
    }).then(res => {
      if (res.ok) {
        setIsAuthenticated(true);
        setUser({ email: "admin@example.com", isAdmin: true });
      }
    }).catch(() => {
      // Not authenticated
    });
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
    // Token is stored in HttpOnly cookie by the server
    // No need to store in localStorage (more secure against XSS)
    setToken(newToken);
    setIsAuthenticated(true);
    setUser({ email: userData.email, isAdmin: true });
  };

  const handleLogout = () => {
    // Server will clear the HttpOnly cookie
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
