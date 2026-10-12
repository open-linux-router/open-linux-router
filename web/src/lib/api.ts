// The one place the UI talks to olrd.
//
// The SPA is a pure client of the HTTP API (design.md §6.3) and has no
// privileged path of its own — anything reachable from here is equally
// reachable from `olr` or an agent over MCP. Keeping every request in this file
// is what makes that easy to verify.

/** One addressed complaint about a request, mirroring core.Problem. */
export interface Problem {
  path?: string
  message: string
}

/** The error envelope every failing endpoint returns, mirroring core.ErrorBody. */
export interface ErrorBody {
  message: string
  problems?: Problem[]
}

/** Where the API token is kept. Only ever read and written here. */
const TOKEN_KEY = 'olr.token'

// Enumerable symbols survive an editor's object spread and are omitted by
// JSON.stringify. Each draft carries the revision it was actually based on.
const CONFIG_REVISION = Symbol('config revision')

export function getToken(): string | null {
  try {
    localStorage.removeItem(TOKEN_KEY)
    return sessionStorage.getItem(TOKEN_KEY)
  } catch {
    // Private browsing modes can throw on access rather than return null.
    return null
  }
}

export function setToken(token: string | null) {
  try {
    localStorage.removeItem(TOKEN_KEY)
    if (token) sessionStorage.setItem(TOKEN_KEY, token)
    else sessionStorage.removeItem(TOKEN_KEY)
  } catch {
    /* ignore — the request will simply 401 and the gate will ask again */
  }
}

export interface AccessView {
  claimed: boolean
  password_set: boolean
  authenticated: boolean
  token_required: boolean
}

/**
 * An API failure, carrying the structured problems so a form can attach each
 * one to the field that caused it rather than dumping a paragraph.
 */
export class ApiError extends Error {
  readonly status: number
  readonly problems: Problem[]

  /**
   * The whole decoded response body, when there was one.
   *
   * Kept because a failed apply is not just an error: design.md §5.3.2 has no
   * rollback, so the response carries the steps that *did* land and the caller
   * has to be able to show them. Reducing a 500 to its message would throw
   * away the only record of what actually changed on the box.
   */
  readonly body: unknown

  constructor(status: number, body: ErrorBody, raw?: unknown) {
    super(body.message)
    this.name = 'ApiError'
    this.status = status
    this.problems = body.problems ?? []
    this.body = raw
  }

  /** True when the token is missing or wrong, which the app handles specially. */
  get unauthorized() {
    return this.status === 401
  }
}

async function request<T>(method: string, path: string, body?: unknown, extraHeaders?: Record<string, string>, encode = JSON.stringify, decode: (text: string) => unknown = JSON.parse): Promise<T> {
  const headers: Record<string, string> = { ...extraHeaders }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if ((method === 'PUT' || method === 'PATCH') && body && typeof body === 'object') {
    const revision = (body as { [CONFIG_REVISION]?: string })[CONFIG_REVISION]
    if (revision && !headers['If-Match']) headers['If-Match'] = revision
  }

  const response = await fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : encode(body),
  })

  if (response.status === 204) return undefined as T

  const text = await response.text()
  let parsed: unknown = undefined
  if (text) {
    try {
      parsed = decode(text)
    } catch {
      // A non-JSON body from an API that always speaks JSON means something
      // upstream answered instead — a proxy, or the SPA fallback. Say so,
      // rather than reporting a confusing parse error.
      throw new ApiError(response.status, {
        message: `unexpected non-JSON response (${response.status})`,
      })
    }
  }

  if (!response.ok) {
    if (response.status === 401 && !path.endsWith('/access/login') && typeof window !== 'undefined') {
      window.dispatchEvent(new Event('olr:unauthorized'))
    }
    const envelope = (parsed as { error?: ErrorBody } | undefined)?.error
    throw new ApiError(
      response.status,
      envelope ?? { message: `request failed (${response.status})` },
      parsed,
    )
  }

  const revision = response.headers.get('ETag')
  if (method === 'GET' && revision && /\/config(?:\?|$)/.test(path) && parsed && typeof parsed === 'object') {
    Object.assign(parsed, { [CONFIG_REVISION]: revision })
  }

  return parsed as T
}

export const api = {
  get: <T>(path: string, decode?: (text: string) => unknown) => request<T>('GET', path, undefined, undefined, undefined, decode),
  put: <T>(path: string, body: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body: unknown) => request<T>('PATCH', path, body),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),

  /**
   * One request whose method is decided by the caller.
   *
   * For a surface that describes a change as data rather than as a call — the
   * gateway screen builds `{method, path, body}` values so that a change can be
   * held, shown to the operator, and then sent again with `confirm=true`
   * unchanged. Replaying a change is only simple if the change is a value.
   */
  send: <T>(method: string, path: string, body?: unknown, headers?: Record<string, string>, encode?: (body: unknown) => string) => request<T>(method, path, body, headers, encode),
}
