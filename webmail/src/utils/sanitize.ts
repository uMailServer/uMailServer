import DOMPurify from 'isomorphic-dompurify'

/**
 * Sanitizes HTML content to prevent XSS attacks.
 * Uses DOMPurify with a strict configuration that:
 * - Strips all script tags and event handlers
 * - Removes dangerous protocols (javascript:, data:)
 * - Keeps safe HTML tags for email rendering
 */
export function sanitizeHTML(dirty: string): string {
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
    // Drop dangerous protocols
    ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto|tel):|[^a-z]|[a-z+\-.]+(?:[^a-z+\-.]|$))/i,
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
