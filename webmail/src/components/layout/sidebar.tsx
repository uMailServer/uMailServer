import { useState } from "react"
import { NavLink, useLocation } from "react-router-dom"
import {
  Inbox,
  Send,
  FileText,
  Trash2,
  Archive,
  Star,
  AlertCircle,
  Settings,
  ChevronLeft,
  ChevronRight,
  PenSquare,
  FolderOpen,
  Tag,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"

interface SidebarProps {
  collapsed: boolean
  onToggle: () => void
  unreadCount?: number
}

interface NavItem {
  icon: React.ElementType
  label: string
  path: string
  count?: number
  color?: string
}

const mainNavItems: NavItem[] = [
  { icon: Inbox, label: "Gelen Kutusu", path: "/inbox" },
  { icon: Star, label: "Yıldızlı", path: "/starred" },
  { icon: Send, label: "Gönderilenler", path: "/sent" },
  { icon: FileText, label: "Taslaklar", path: "/drafts", count: 2 },
  { icon: Archive, label: "Arşiv", path: "/archive" },
  { icon: Trash2, label: "Çöp Kutusu", path: "/trash" },
]

const folderItems: NavItem[] = [
  { icon: AlertCircle, label: "Spam", path: "/spam", count: 5, color: "text-red-500" },
  { icon: FolderOpen, label: "İş", path: "/folder/work" },
  { icon: FolderOpen, label: "Kişisel", path: "/folder/personal" },
  { icon: Tag, label: "Önemli", path: "/tag/important", color: "text-amber-500" },
]

export function Sidebar({ collapsed, onToggle, unreadCount = 0 }: SidebarProps) {
  const location = useLocation()
  const [hovered, setHovered] = useState(false)

  const isExpanded = !collapsed || hovered

  return (
    <aside
      className={cn(
        "fixed left-0 top-0 z-40 h-screen border-r bg-card transition-all duration-300 ease-in-out",
        isExpanded ? "w-64" : "w-16"
      )}
      onMouseEnter={() => collapsed && setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {/* Logo Area */}
      <div className="flex h-16 items-center justify-between border-b px-4">
        <div className={cn("flex items-center gap-3", !isExpanded && "justify-center w-full")}>
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-primary to-primary/80 shadow-lg shadow-primary/25">
            <svg
              viewBox="0 0 24 24"
              className="h-5 w-5 text-primary-foreground"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
          </div>
          {isExpanded && (
            <span className="font-semibold text-lg tracking-tight">uMail</span>
          )}
        </div>
        {isExpanded && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={onToggle}
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
        )}
      </div>

      {/* Compose Button */}
      <div className="p-3">
        <Button
          className={cn(
            "w-full bg-gradient-to-r from-primary to-primary/90 hover:from-primary/90 hover:to-primary shadow-lg shadow-primary/25 transition-all",
            !isExpanded && "px-0 justify-center"
          )}
          size={isExpanded ? "default" : "icon"}
        >
          <PenSquare className="h-4 w-4" />
          {isExpanded && <span className="ml-2">Yeni E-posta</span>}
        </Button>
      </div>

      {/* Main Navigation */}
      <nav className="flex-1 space-y-1 px-2 py-2 overflow-y-auto">
        {mainNavItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-200 group relative",
                isActive
                  ? "bg-primary/10 text-primary shadow-sm"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon
              className={cn(
                "h-5 w-5 shrink-0 transition-colors",
                location.pathname === item.path ? "text-primary" : "text-muted-foreground group-hover:text-foreground"
              )}
            />
            {isExpanded && (
              <>
                <span className="flex-1">{item.label}</span>
                {(item.count !== undefined || (item.path === "/inbox" && unreadCount > 0)) && (
                  <Badge
                    variant={location.pathname === item.path ? "default" : "secondary"}
                    className="h-5 min-w-[20px] px-1.5 text-xs"
                  >
                    {item.path === "/inbox" ? unreadCount : item.count}
                  </Badge>
                )}
              </>
            )}
            {!isExpanded && (item.count !== undefined || (item.path === "/inbox" && unreadCount > 0)) && (
              <Badge
                variant="default"
                className="absolute -right-1 -top-1 h-4 w-4 p-0 flex items-center justify-center text-[10px]"
              >
                {item.path === "/inbox" ? unreadCount : item.count}
              </Badge>
            )}
          </NavLink>
        ))}

        <Separator className="my-3" />

        {isExpanded && (
          <p className="px-3 pb-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
            Klasörler
          </p>
        )}

        {folderItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-200 group relative",
                isActive
                  ? "bg-primary/10 text-primary shadow-sm"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon
              className={cn(
                "h-5 w-5 shrink-0 transition-colors",
                item.color || (location.pathname === item.path ? "text-primary" : "text-muted-foreground group-hover:text-foreground")
              )}
            />
            {isExpanded && (
              <>
                <span className="flex-1">{item.label}</span>
                {item.count !== undefined && (
                  <Badge
                    variant={location.pathname === item.path ? "default" : "secondary"}
                    className="h-5 min-w-[20px] px-1.5 text-xs"
                  >
                    {item.count}
                  </Badge>
                )}
              </>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Bottom Actions */}
      <div className="border-t p-2">
        <NavLink
          to="/settings"
          className={({ isActive }) =>
            cn(
              "flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-200 group",
              isActive
                ? "bg-primary/10 text-primary"
                : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            )
          }
        >
          <Settings className="h-5 w-5 shrink-0" />
          {isExpanded && <span>Ayarlar</span>}
        </NavLink>
      </div>

      {/* Collapse Toggle (when collapsed) */}
      {!isExpanded && (
        <Button
          variant="ghost"
          size="icon"
          className="absolute -right-3 top-20 h-6 w-6 rounded-full border bg-background shadow-md"
          onClick={onToggle}
        >
          <ChevronRight className="h-3 w-3" />
        </Button>
      )}
    </aside>
  )
}
