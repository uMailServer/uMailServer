// Reply / forward helpers: subject prefixes and quoted bodies.

const REPLY_PREFIX = /^\s*(re|aw|sv|ref)\s*:\s*/i
const FWD_PREFIX = /^\s*(fwd?|wg|tr)\s*:\s*/i

/** "Re: x" - never stacks "Re: Re:". */
export function replySubject(subject: string): string {
  const s = subject || ""
  return REPLY_PREFIX.test(s) ? s.replace(REPLY_PREFIX, "Re: ") : "Re: " + s
}

/** "Fwd: x" - never stacks "Fwd: Fwd:". */
export function forwardSubject(subject: string): string {
  const s = subject || ""
  return FWD_PREFIX.test(s) ? s.replace(FWD_PREFIX, "Fwd: ") : "Fwd: " + s
}

function stripTags(s: string): string {
  return s.replace(/<(script|style)[\s\S]*?<\/\1>/gi, "").replace(/<br\s*\/?>|<\/(p|div|tr|li)>/gi, "\n").replace(/<[^>]+>/g, "")
}

/** Quote a (possibly HTML) body with "> " prefixes, preceded by an attribution line. */
export function quoteForReply(from: string, date: string, body: string): string {
  const text = (/<\s*(?:p|div|br|html|body|table)\b/i.test(body) ? stripTags(body) : body).replace(/\r\n/g, "\n").trim()
  const quoted = text.split("\n").map((l) => (l.startsWith(">") ? ">" + l : "> " + l)).join("\n")
  return `\n\nOn ${date}, ${from} wrote:\n${quoted}\n`
}

export function forwardBody(from: string, date: string, subject: string, to: string[], body: string): string {
  const text = (/<\s*(?:p|div|br|html|body|table)\b/i.test(body) ? stripTags(body) : body).replace(/\r\n/g, "\n").trim()
  return `\n\n---------- Forwarded message ----------\nFrom: ${from}\nDate: ${date}\nSubject: ${subject}\nTo: ${to.join(", ")}\n\n${text}\n`
}

/** Bare lower-cased-for-compare address from "Name <a@b>" or "a@b". */
function bareAddress(s: string): string {
  const m = /<([^<>]+)>/.exec(s)
  return (m ? m[1] : s).trim()
}

/**
 * Reply-all recipients: To = sender + original To, Cc = original Cc, with the
 * caller's own address removed and case-insensitive de-duplication
 * (a recipient already in To is not repeated in Cc).
 */
export function replyAllRecipients(
  from: string, to: string[] | undefined, cc: string[] | undefined, me: string | undefined,
): { to: string[]; cc: string[] } {
  const self = bareAddress(me || "").toLowerCase()
  const seen = new Set<string>()
  const pick = (list: string[]) => {
    const out: string[] = []
    for (const raw of list) {
      const a = bareAddress(raw || "")
      const k = a.toLowerCase()
      if (!a || !a.includes("@") || k === self || seen.has(k)) continue
      seen.add(k)
      out.push(a)
    }
    return out
  }
  const toOut = pick([from, ...(to || [])])
  return { to: toOut, cc: pick(cc || []) }
}
