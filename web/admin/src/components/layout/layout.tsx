import { useState } from "react";
import { Sidebar } from "./sidebar";
import { Header } from "./header";
import { cn } from "@/lib/utils";
import type { User } from "@/types";

interface LayoutProps {
  children: React.ReactNode;
  user: User | null;
  onLogout: () => void;
  isConnected?: boolean;
}

export function Layout({ children, user, onLogout, isConnected = false }: LayoutProps) {
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState(false);

  return (
    <div className="min-h-screen bg-background">
      <Sidebar
        isCollapsed={isSidebarCollapsed}
        onToggle={() => setIsSidebarCollapsed(!isSidebarCollapsed)}
        user={user}
        onLogout={onLogout}
      />

      <div
        className={cn(
          "transition-all duration-300 ease-in-out",
          isSidebarCollapsed ? "ml-16" : "ml-64"
        )}
      >
        <Header isConnected={isConnected} />

        <main className="p-6">
          <div className="mx-auto max-w-7xl">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
