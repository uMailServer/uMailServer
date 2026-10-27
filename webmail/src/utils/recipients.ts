// A contact address offered while typing a recipient.
export type Suggestion = { name: string; email: string }

const MAX_SUGGESTIONS = 8

// suggestionsFor returns the contacts whose name or address contains the
// word being typed, leaving out addresses already added.
export function suggestionsFor(all: Suggestion[], word: string, exclude: string[]): Suggestion[] {
  const q = word.trim().toLowerCase()
  if (!q) return []
  const taken = new Set(exclude.map((e) => e.toLowerCase()))
  return all
    .filter((s) => !taken.has(s.email.toLowerCase()) &&
      (s.email.toLowerCase().includes(q) || s.name.toLowerCase().includes(q)))
    .slice(0, MAX_SUGGESTIONS)
}
