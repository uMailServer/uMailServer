import { Link, useLocation } from "react-router-dom";
import {
  LayoutDashboard,
  Globe,
  Users,
  Mail,
  Settings,
  Server,
  ChevronLeft,
  ChevronRight,
  LogOut,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { User } from "@/types";

interface SidebarProps {
  isCollapsed: boolean;
  onToggle: () => void;
  user: User | null;
  onLogout: () => void;
}

const menuItems = [
  { path: "/", icon: LayoutDashboard, label: "Dashboard" },
  { path: "/domains", icon: Globe, label: "Domains" },
  { path: "/accounts", icon: Users, label: "Accounts" },
  { path: "/queue", icon: Mail, label: "Queue" },
  { path: "/settings", icon: Settings, label: "Settings" },
];

export function Sidebar({ isCollapsed, onToggle, user, onLogout }: SidebarProps) {
  const location = useLocation();

  return (
    <TooltipProvider delay={0}>
      <aside
        className={cn(
          "fixed left-0 top-0 z-40 h-screen bg-card border-r border-border transition-all duration-300 ease-in-out",
          isCollapsed ? "w-16" : "w-64"
        )}
      >
        {/* Header */}
        <div className="flex h-16 items-center justify-between px-4 border-b border-border">
          <div className="flex items-center gap-2 overflow-hidden">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary flex-shrink-0">
              <Server className="h-4 w-4 text-primary-foreground" />
            </div>
            {!isCollapsed && (
              <span className="font-semibold text-lg truncate">uMail Admin</span>
            )}
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={onToggle}
            className="h-8 w-8 flex-shrink-0"
          >
            {isCollapsed ? (
              <ChevronRight className="h-4 w-4" />
            ) : (
              <ChevronLeft className="h-4 w-4" />
            )}
          </Button>
        </div>

        {/* Navigation */}
        <nav className="flex-1 p-2 space-y-1">
          {menuItems.map((item) => {
            const isActive = location.pathname === item.path;
            const content = (
              <Link
                to={item.path}
                className={cn(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                  isActive
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                )}
              >
                <item.icon className={cn("h-5 w-5 flex-shrink-0", isActive && "text-primary")} />
                {!isCollapsed && <span className="truncate">{item.label}</span>}
              </Link>
            );

            if (isCollapsed) {
              return (
                <Tooltip key={item.path}>
                  {/* @ts-expect-error asChild prop not typed in Base UI but works at runtime */}
                  <TooltipTrigger asChild>{content}</TooltipTrigger>
                  <TooltipContent side="right">{item.label}</TooltipContent>
                </Tooltip>
              );
            }

            return <div key={item.path}>{content}</div>;
          })}
        </nav>

        {/* Footer */}
        <div className="absolute bottom-0 left-0 right-0 p-2 border-t border-border">
          <div className={cn("flex items-center gap-3 px-3 py-2", isCollapsed && "justify-center")}>
            {!isCollapsed && (
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{user?.email}</p>
                <p className="text-xs text-muted-foreground">Administrator</p>
              </div>
            )}
            <Tooltip>
              {/* @ts-expect-error asChild prop not typed in Base UI but works at runtime */}
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={onLogout}
                  className="h-8 w-8 text-muted-foreground hover:text-destructive"
                >
                  <LogOut className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side={isCollapsed ? "right" : "top"}>Logout</TooltipContent>
            </Tooltip>
          </div>
        </div>
      </aside>
    </TooltipProvider>
  );
}
