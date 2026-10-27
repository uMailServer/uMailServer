// A contact address offered while typing a recipient.
export type Suggestion = { name: string; email: string }

const MAX_SUGGESTIONS = 8

// suggestionsFor returns the contacts whose name or address contains the
// word being typed, leaving out addresses already added and duplicates.
export function suggestionsFor(all: Suggestion[], word: string, exclude: string[]): Suggestion[] {
  const q = word.trim().toLowerCase()
  if (!q) return []
  // Addresses already added, and ones offered once already: the same
  // address often appears in several contacts or address books.
  const seen = new Set(exclude.map((e) => e.toLowerCase()))
  const out: Suggestion[] = []
  for (const s of all) {
    const key = s.email.toLowerCase()
    if (seen.has(key) || !(key.includes(q) || s.name.toLowerCase().includes(q))) continue
    seen.add(key)
    out.push(s)
    if (out.length === MAX_SUGGESTIONS) break
  }
  return out
}
