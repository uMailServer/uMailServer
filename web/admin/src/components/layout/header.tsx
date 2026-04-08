import { useLocation } from "react-router-dom";
import { Sun, Moon, Monitor } from "lucide-react";
import { useTheme } from "@/components/theme-provider";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

interface HeaderProps {
  isConnected: boolean;
}

const breadcrumbMap: Record<string, string> = {
  "/": "Dashboard",
  "/domains": "Domains",
  "/accounts": "Accounts",
  "/queue": "Queue",
  "/settings": "Settings",
};

export function Header({ isConnected }: HeaderProps) {
  const { setTheme, resolvedTheme } = useTheme();
  const location = useLocation();

  const pageTitle = breadcrumbMap[location.pathname] || "Dashboard";

  return (
    <header className="sticky top-0 z-30 h-16 bg-card border-b border-border px-6 flex items-center justify-between">
      {/* Left side - Breadcrumb */}
      <div className="flex items-center gap-4">
        <h1 className="text-xl font-semibold">{pageTitle}</h1>
      </div>

      {/* Right side - Actions */}
      <div className="flex items-center gap-2">
        {/* Realtime Status */}
        <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-muted mr-2">
          <div
            className={cn(
              "w-2 h-2 rounded-full animate-pulse",
              isConnected ? "bg-green-500" : "bg-red-500"
            )}
          />
          <span className="text-xs font-medium text-muted-foreground">
            {isConnected ? "Live" : "Offline"}
          </span>
        </div>

        {/* Theme Toggle */}
        <DropdownMenu>
          {/* @ts-expect-error asChild prop not typed in Base UI but works at runtime */}
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-9 w-9">
              {resolvedTheme === "dark" ? (
                <Moon className="h-4 w-4" />
              ) : (
                <Sun className="h-4 w-4" />
              )}
              <span className="sr-only">Toggle theme</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => setTheme("light")}>
              <Sun className="mr-2 h-4 w-4" />
              Light
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setTheme("dark")}>
              <Moon className="mr-2 h-4 w-4" />
              Dark
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setTheme("system")}>
              <Monitor className="mr-2 h-4 w-4" />
              System
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
