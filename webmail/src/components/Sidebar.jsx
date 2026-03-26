import { useEmail } from '../contexts/EmailContext'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Inbox,
  Send,
  FileText,
  Trash2,
  AlertTriangle,
  DraftingCompass
} from 'lucide-react'

const folderIcons: Record<string, React.ReactNode> = {
  'Inbox': <Inbox className="h-4 w-4" />,
  'Sent': <Send className="h-4 w-4" />,
  'Drafts': <DraftingCompass className="h-4 w-4" />,
  'Trash': <Trash2 className="h-4 w-4" />,
  'Junk': <AlertTriangle className="h-4 w-4" />,
}

function Sidebar() {
  const { folders, currentFolder, changeFolder, getUnreadCount } = useEmail()

  return (
    <aside className="w-64 border-r bg-muted/40 flex flex-col">
      <ScrollArea className="flex-1 py-4">
        <div className="px-3 space-y-1">
          {folders.map((folder) => {
            const isActive = folder === currentFolder
            const count = getUnreadCount(folder)

            return (
              <Button
                key={folder}
                variant={isActive ? 'secondary' : 'ghost'}
                className="w-full justify-start gap-3 h-11"
                onClick={() => changeFolder(folder)}
              >
                {folderIcons[folder] || <FileText className="h-4 w-4" />}
                <span className="flex-1 text-left">{folder}</span>
                {count > 0 && (
                  <Badge variant="secondary" className="ml-auto">
                    {count}
                  </Badge>
                )}
              </Button>
            )
          })}
        </div>
      </ScrollArea>
    </aside>
  )
}

export default Sidebar
