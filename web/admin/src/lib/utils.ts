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

// Binary units, matching the backend's config.KB/MB/GB (1024-based).
const GIB = 1024 ** 3

/** Human-readable byte count ("1.5 KB"); 1 GB here is 1024^3 bytes like the server's config.GB. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), sizes.length - 1)
  return parseFloat((bytes / Math.pow(1024, i)).toFixed(2)) + " " + sizes[i]
}

/** Parses a GB form value into the API's quota_limit bytes; null when empty, negative or not a number. 0 = unlimited. */
export function gbToBytes(input: string): number | null {
  const s = input.trim()
  if (s === "" || !/^\d*\.?\d+$/.test(s)) return null
  const gb = Number(s)
  if (!Number.isFinite(gb)) return null
  return Math.round(gb * GIB)
}

/** Form-field text for a quota_limit in bytes ("2", "1.5"); 0 stays "0" (unlimited). */
export function bytesToGb(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0"
  return String(parseFloat((bytes / GIB).toFixed(2)))
}

// One explicit formatter for every date the admin UI shows: fixed locale and
// the viewer's timezone, so dates and times never mix en-US/de-DE styles.
const DATE_LOCALE = "en-GB"
const parse = (value: string | number | Date | undefined | null): Date | null => {
  if (value === undefined || value === null || value === "") return null
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

/** "12 Oct 2026, 14:03:09" for an API timestamp; "-" when missing or unparseable. */
export function formatDateTime(value: string | number | Date | undefined | null): string {
  const d = parse(value)
  return d ? d.toLocaleString(DATE_LOCALE, { dateStyle: "medium", timeStyle: "medium" }) : "-"
}

/** "12 Oct 2026"; "-" when missing or unparseable. */
export function formatDate(value: string | number | Date | undefined | null): string {
  const d = parse(value)
  return d ? d.toLocaleDateString(DATE_LOCALE, { dateStyle: "medium" }) : "-"
}

/** "14:03:09"; "-" when missing or unparseable. */
export function formatTime(value: string | number | Date | undefined | null): string {
  const d = parse(value)
  return d ? d.toLocaleTimeString(DATE_LOCALE, { timeStyle: "medium" }) : "-"
}
