import { useEffect, useCallback } from "react"
import { useNavigate } from "react-router-dom"

export function useKeyboardShortcuts() {
  const navigate = useNavigate()

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    // Ignore if typing in an input
    if (
      e.target instanceof HTMLInputElement ||
      e.target instanceof HTMLTextAreaElement
    ) {
      return
    }

    const key = e.key.toLowerCase()
    const ctrl = e.ctrlKey || e.metaKey
    const shift = e.shiftKey

    // Navigation shortcuts (g + letter)
    if (key === "g" && !ctrl) {
      // Wait for next key
      return
    }

    // Global shortcuts
    if (ctrl && key === "n") {
      e.preventDefault()
      navigate("/compose")
      return
    }

    if (ctrl && shift && key === "i") {
      e.preventDefault()
      navigate("/inbox")
      return
    }

    if (key === "/" && !ctrl) {
      e.preventDefault()
      navigate("/search")
      return
    }

    if (ctrl && key === "1") {
      e.preventDefault()
      navigate("/inbox")
      return
    }

    if (ctrl && key === "2") {
      e.preventDefault()
      navigate("/sent")
      return
    }

    if (ctrl && key === "3") {
      e.preventDefault()
      navigate("/drafts")
      return
    }

    if (ctrl && key === "4") {
      e.preventDefault()
      navigate("/trash")
      return
    }

    if (ctrl && key === "k") {
      e.preventDefault()
      navigate("/search")
      return
    }

    if (key === "?" && shift) {
      e.preventDefault()
      // Toggle shortcuts dialog
      document.dispatchEvent(new CustomEvent("toggle-shortcuts"))
      return
    }

    if (key === "escape") {
      document.dispatchEvent(new CustomEvent("close-dialogs"))
      return
    }
  }, [navigate])

  useEffect(() => {
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [handleKeyDown])
}

export const shortcuts = [
  { category: "Navigation", items: [
    { keys: ["⌘", "1"], description: "Go to Inbox" },
    { keys: ["⌘", "2"], description: "Go to Sent" },
    { keys: ["⌘", "3"], description: "Go to Drafts" },
    { keys: ["⌘", "4"], description: "Go to Trash" },
    { keys: ["⌘", "K"], description: "Search" },
    { keys: ["/"], description: "Search (when not in input)" },
    { keys: ["?"], description: "Show keyboard shortcuts" },
    { keys: ["Esc"], description: "Close dialog" },
  ]},
  { category: "Actions", items: [
    { keys: ["⌘", "N"], description: "Compose new email" },
    { keys: ["⌘", "Shift", "I"], description: "Go to Inbox" },
    { keys: ["R"], description: "Reply to email" },
    { keys: ["A"], description: "Reply all" },
    { keys: ["F"], description: "Forward email" },
    { keys: ["E"], description: "Archive email" },
    { keys: ["#"], description: "Delete email" },
    { keys: ["S"], description: "Toggle star" },
    { keys: ["U"], description: "Mark as unread" },
  ]},
  { category: "Selection", items: [
    { keys: ["X"], description: "Select email" },
    { keys: ["Shift", "↓"], description: "Select next email" },
    { keys: ["Shift", "↑"], description: "Select previous email" },
    { keys: ["*", "A"], description: "Select all" },
    { keys: ["*", "N"], description: "Deselect all" },
  ]},
  { category: "Navigation in list", items: [
    { keys: ["J"], description: "Next email" },
    { keys: ["K"], description: "Previous email" },
    { keys: ["Enter"], description: "Open email" },
    { keys: ["←"], description: "Go back" },
  ]},
]
