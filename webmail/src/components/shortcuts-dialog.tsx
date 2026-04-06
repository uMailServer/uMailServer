import { useState, useEffect } from "react"
import { Keyboard } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { shortcuts } from "@/hooks/useKeyboardShortcuts"
import { Separator } from "@/components/ui/separator"

export function ShortcutsDialog() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const handleToggle = () => setOpen((prev) => !prev)
    const handleClose = () => setOpen(false)

    document.addEventListener("toggle-shortcuts", handleToggle)
    document.addEventListener("close-dialogs", handleClose)

    return () => {
      document.removeEventListener("toggle-shortcuts", handleToggle)
      document.removeEventListener("close-dialogs", handleClose)
    }
  }, [])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Keyboard className="h-5 w-5" />
            Keyboard Shortcuts
          </DialogTitle>
        </DialogHeader>
        <div className="flex-1 overflow-y-auto">
          <div className="grid gap-6 md:grid-cols-2">
            {shortcuts.map((section) => (
              <div key={section.category}>
                <h3 className="text-sm font-semibold text-muted-foreground mb-3">
                  {section.category}
                </h3>
                <div className="space-y-2">
                  {section.items.map((item, index) => (
                    <div
                      key={index}
                      className="flex items-center justify-between py-1"
                    >
                      <span className="text-sm">{item.description}</span>
                      <div className="flex items-center gap-1">
                        {item.keys.map((key, keyIndex) => (
                          <span key={keyIndex}>
                            <kbd className="inline-flex items-center justify-center rounded border bg-muted px-2 py-0.5 text-xs font-mono font-medium shadow-sm min-w-[1.5rem]">
                              {key}
                            </kbd>
                            {keyIndex < item.keys.length - 1 && (
                              <span className="mx-0.5 text-muted-foreground" />
                            )}
                          </span>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </div>
        <Separator className="my-4" />
        <div className="text-xs text-muted-foreground text-center">
          Press <kbd className="inline-flex items-center justify-center rounded border bg-muted px-1.5 py-0.5 text-xs font-mono">?</kbd> to toggle this dialog
        </div>
      </DialogContent>
    </Dialog>
  )
}
