import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Renders a queue entry's recipients; the API returns an array, which React would otherwise concatenate without separators. */
export function formatRecipients(to: string | string[] | undefined | null): string {
  if (Array.isArray(to)) return to.join(", ")
  return to ?? ""
}

/** The API serialises a never-logged-in account as Go's zero time ("0001-01-01T00:00:00Z"), which is truthy. */
export function hasLoggedIn(lastLogin: string | undefined | null): boolean {
  if (!lastLogin) return false
  const t = new Date(lastLogin).getTime()
  return !Number.isNaN(t) && t > 0
}
