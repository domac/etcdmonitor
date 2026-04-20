// util.js - Small client-side helpers shared across dashboard pages.
// Must be loaded BEFORE any business script that touches user/backend data.
// See SECURITY.md / security-hardening capability for XSS policy.

/**
 * Escape a string for safe interpolation into HTML.
 * Converts & < > " ' into entities. Returns the empty string for null/undefined.
 *
 * @param {*} s - value to escape (non-strings are String()-coerced)
 * @returns {string}
 */
function escapeHTML(s) {
    if (s === null || s === undefined) return '';
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

/**
 * Set the textContent of an element (null-safe, selector or element).
 * Prefer this over `el.innerHTML = str` whenever `str` contains user/backend data.
 *
 * @param {string|Element} target - element reference or CSS selector
 * @param {*} text - value to set as text
 */
function setText(target, text) {
    var el = (typeof target === 'string') ? document.querySelector(target) : target;
    if (!el) return;
    el.textContent = (text === null || text === undefined) ? '' : String(text);
}

// Export to global scope for use in inline scripts and legacy files.
if (typeof window !== 'undefined') {
    window.escapeHTML = escapeHTML;
    window.setText = setText;
}
