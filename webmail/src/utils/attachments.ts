// Compose attachment helpers. Caps mirror internal/api/mail_send_attach.go.
export const MAX_ATTACHMENTS = 20
export const MAX_ATTACHMENT_BYTES = 10 << 20
export const MAX_ATTACHMENT_TOTAL = 15 << 20

export interface ComposeAttachment {
  filename: string
  contentType: string
  data: string // base64
  size: number
}

export function readFileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onerror = () => reject(fr.error ?? new Error('Could not read file'))
    fr.onload = () => {
      const s = String(fr.result)
      resolve(s.slice(s.indexOf(',') + 1))
    }
    fr.readAsDataURL(file)
  })
}

// Returns an error message when adding `file` to `current` would break a cap.
export function attachmentError(current: { size: number }[], file: { name: string; size: number }): string | null {
  if (current.length >= MAX_ATTACHMENTS) return `At most ${MAX_ATTACHMENTS} attachments`
  if (file.size > MAX_ATTACHMENT_BYTES) return `${file.name} is larger than ${MAX_ATTACHMENT_BYTES >> 20} MB`
  if (current.reduce((n, a) => n + a.size, 0) + file.size > MAX_ATTACHMENT_TOTAL) return `Attachments exceed ${MAX_ATTACHMENT_TOTAL >> 20} MB in total`
  return null
}
