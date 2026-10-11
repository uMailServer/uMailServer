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
