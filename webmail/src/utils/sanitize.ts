import DOMPurify from 'isomorphic-dompurify'

// Hook to inject rel="noopener noreferrer" on all links with target="_blank"
// preventing tabnabbing attacks (CWE-1022)
let allowRemote = false

const UNSAFE_CSS = /url\s*\(|image-set\s*\(|expression|behavior|-moz-binding|@import|javascript:|\\/i
const OVERLAY_CSS = /^\s*position\s*:\s*(?:fixed|absolute|sticky)/i

/** Drops declarations that fetch remote resources or escape the message box. */
function cleanStyle(style: string): string {
  return style
    .split(';')
    .filter((d) => d.includes(':') && !UNSAFE_CSS.test(d) && !OVERLAY_CSS.test(d))
    .map((d) => d.trim())
    .join(';')
}

const REMOTE_URL = /^\s*(?:https?:)?\/\//i

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A' && node.getAttribute('target') === '_blank') {
    node.setAttribute('rel', 'noopener noreferrer')
  }
  if (node.hasAttribute && node.hasAttribute('style')) {
    const cleaned = cleanStyle(node.getAttribute('style') || '')
    if (cleaned) node.setAttribute('style', cleaned)
    else node.removeAttribute('style')
  }
  // Block remote content (tracking pixels) unless explicitly allowed.
  if (!allowRemote && node.tagName === 'IMG') {
    const src = node.getAttribute('src')
    if (src !== null && REMOTE_URL.test(src)) node.removeAttribute('src')
  }
})

/**
 * Sanitizes HTML content to prevent XSS attacks.
 * Uses DOMPurify with a strict configuration that:
 * - Strips all script tags and event handlers
 * - Removes dangerous protocols (javascript:, data:)
 * - Keeps safe HTML tags for email rendering
 * - Injects rel="noopener noreferrer" on target="_blank" links
 */
export function sanitizeHTML(dirty: string, opts: { allowRemoteContent?: boolean } = {}): string {
  allowRemote = opts.allowRemoteContent === true
  try {
    return sanitizeInner(dirty)
  } finally {
    allowRemote = false
  }
}

function sanitizeInner(dirty: string): string {
  return DOMPurify.sanitize(dirty, {
    ALLOWED_TAGS: [
      'html', 'body', 'head', 'style',
      'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'p', 'br', 'hr',
      'ul', 'ol', 'li', 'dl', 'dt', 'dd',
      'blockquote', 'pre', 'code',
      'a', 'img', 'figure', 'figcaption',
      'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td',
      'div', 'span', 'section', 'article', 'header', 'footer', 'nav', 'aside', 'main',
      'strong', 'b', 'em', 'i', 'u', 's', 'strike', 'del', 'ins',
      'sup', 'sub',
      'q', 'cite', 'abbr', 'acronym', 'mark',
      'details', 'summary',
    ],
    ALLOWED_ATTR: [
      'href', 'src', 'alt', 'title', 'class', 'id',
      'width', 'height', 'colspan', 'rowspan',
      'target', 'rel',
      'style',
    ],
    ALLOW_DATA_ATTR: false,
    ADD_ATTR: ['target'],
    FORBID_TAGS: ['script', 'style', 'iframe', 'form', 'input', 'button', 'object', 'embed'],
    FORBID_ATTR: ['onerror', 'onload', 'onclick', 'onmouseover', 'onfocus', 'onblur', 'onchange', 'onsubmit'],
    // Drop dangerous protocols. The final character class excludes ":" so an
    // UNLISTED scheme (javascript:, vbscript:, data:, ...) can never satisfy
    // the third alternative — same safeguard as DOMPurify's default regex.
    ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto|tel):|[^a-z]|[a-z+\-.]+(?:[^a-z+\-.:]|$))/i,
  })
}

/**
 * Sanitizes plain text for safe display (strips all HTML)
 */
export function sanitizeText(dirty: string): string {
  return DOMPurify.sanitize(dirty, {
    ALLOWED_TAGS: [],
    ALLOWED_ATTR: [],
  })
}
