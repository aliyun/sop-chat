/**
 * Clipboard utilities with robust fallbacks.
 *
 * Why:
 * - `navigator.clipboard` is unavailable in some browsers or non-secure contexts (http/file).
 * - When it's undefined, calling `navigator.clipboard.writeText` throws synchronously and
 *   won't be caught by Promise `.catch()`.
 */

export async function copyToClipboard(text) {
  if (typeof text !== 'string') {
    text = String(text ?? '');
  }

  // Prefer modern Clipboard API when available and in a secure context.
  // Note: Some browsers require https (or localhost) for navigator.clipboard.
  try {
    if (typeof window !== 'undefined' && window.isSecureContext && navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch (_) {
    // Fall through to legacy fallback
  }

  // Legacy fallback: use a hidden textarea + execCommand('copy')
  try {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.top = '-1000px';
    textarea.style.left = '-1000px';
    textarea.style.opacity = '0';

    document.body.appendChild(textarea);
    textarea.focus();
    textarea.select();

    const ok = document.execCommand && document.execCommand('copy');
    document.body.removeChild(textarea);
    return !!ok;
  } catch (_) {
    return false;
  }
}

