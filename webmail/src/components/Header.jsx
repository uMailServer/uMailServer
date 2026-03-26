import { useAuth } from '../contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Separator } from '@/components/ui/separator'
import { PenSquare, LogOut, Settings, User } from 'lucide-react'

function Header({ onCompose }) {
  const { user, logout } = useAuth()

  const getInitials = (email: string) => {
    return email?.split('@')[0]?.slice(0, 2).toUpperCase() || 'U'
  }

  return (
    <header className="sticky top-0 z-50 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="flex h-16 items-center px-4 gap-4">
        <div className="flex items-center gap-2 flex-1">
          <div className="bg-gradient-to-br from-violet-600 to-indigo-700 p-2 rounded-lg">
            <PenSquare className="h-5 w-5 text-white" />
          </div>
          <span className="font-semibold text-xl tracking-tight">uMailServer</span>
        </div>

        <div className="flex items-center gap-4">
          <Button onClick={onCompose} className="gap-2">
            <PenSquare className="h-4 w-4" />
            Compose
          </Button>

          <Separator orientation="vertical" className="h-8" />

          <div className="flex items-center gap-3">
            <Avatar className="h-8 w-8">
              <AvatarFallback className="bg-gradient-to-br from-violet-600 to-indigo-700 text-white text-xs">
                {getInitials(user?.email || '')}
              </AvatarFallback>
            </Avatar>
            <span className="text-sm text-muted-foreground hidden md:inline-block">
              {user?.email}
            </span>
          </div>

          <Button variant="ghost" size="icon" className="hidden md:flex">
            <Settings className="h-4 w-4" />
          </Button>

          <Button variant="ghost" size="icon" onClick={logout}>
            <LogOut className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </header>
  )
}

export default Header
