// Thin JSON fetch wrapper for same-origin /api (AGENTS: no CORS — everything
// goes through Traefik's console.localhost router). credentials: 'include' so
// the session cookie rides along; the CSRF token is whatever login or
// /auth/me last handed us (plan §7 double-submit).

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

let csrfToken: string | null = null;

export function setCsrfToken(token: string | null): void {
  csrfToken = token;
}

/** For callers that bypass fetch (XHR uploads need upload-progress events). */
export function getCsrfToken(): string | null {
  return csrfToken;
}

export async function api<T>(
  path: string,
  init: { json?: unknown; method?: string } = {},
): Promise<T> {
  const headers = new Headers();
  let body: BodyInit | undefined;
  if (init.json !== undefined) {
    headers.set('content-type', 'application/json');
    body = JSON.stringify(init.json);
  }
  if (csrfToken) headers.set('x-csrf-token', csrfToken);

  const res = await fetch(`/api${path}`, {
    method: init.method ?? (init.json !== undefined ? 'POST' : 'GET'),
    headers,
    body,
    credentials: 'include',
  });
  if (!res.ok) {
    let message = res.statusText;
    try {
      const payload = (await res.json()) as { message?: string };
      message = payload.message ?? message;
    } catch {
      // non-JSON error body (proxy/HTML) — keep statusText
    }
    throw new ApiError(res.status, Array.isArray(message) ? message.join(', ') : String(message));
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
