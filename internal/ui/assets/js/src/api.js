// Shared requests distinguish session expiry from transient network failures.
let expired = false;
export function sessionExpired() { return expired; }
export class SessionExpiredError extends Error {}
async function parseResponse(res) {
    if (res.status === 401 || (res.redirected && new URL(res.url).pathname === '/login')) {
        expired = true;
        window.dispatchEvent(new Event('servo-session-expired'));
        window.location.assign('/login');
        throw new SessionExpiredError('Session expired; sign in again');
    }
    const text = await res.text();
    let value = text;
    try { value = text ? JSON.parse(text) : null; } catch { /* plain error response */ }
    if (!res.ok) throw new Error(value?.error || value?.message || text || `HTTP ${res.status}`);
    return value;
}
export async function requestJSON(endpoint, { method = 'GET', body, signal } = {}) {
    if (expired) throw new SessionExpiredError('Session expired');
    return parseResponse(await fetch(endpoint, {
        method, signal,
        headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
        body: body === undefined ? undefined : JSON.stringify(body),
    }));
}
export async function uploadForm(endpoint, body) {
    if (expired) throw new SessionExpiredError('Session expired');
    return parseResponse(await fetch(endpoint, { method: 'POST', headers: { Accept: 'application/json' }, body }));
}
export const getJSON = (url, signal) => requestJSON(url, { signal });
export const postJSON = (url, body, signal) => requestJSON(url, { method: 'POST', body, signal });
export const patchJSON = (url, body, signal) => requestJSON(url, { method: 'PATCH', body, signal });
export const putJSON = (url, body, signal) => requestJSON(url, { method: 'PUT', body, signal });
export const deleteJSON = (url, body, signal) => requestJSON(url, { method: 'DELETE', body, signal });
