export class ApiError extends Error {
  constructor(
    message: string,
    public readonly httpStatus?: number,
  ) {
    super(message)
    this.name = "ApiError"
  }
}

const TOKEN_KEY = "nt_token"

let authToken: string | null = localStorage.getItem(TOKEN_KEY)
let onUnauthorized: (() => void) | null = null

export function setAuthToken(token: string | null) {
  authToken = token
  if (token) {
    localStorage.setItem(TOKEN_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_KEY)
  }
}

export function getAuthToken() {
  return authToken
}

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export interface ApiRequestOptions {
  method?: string
  body?: BodyInit | null
  signal?: AbortSignal
}

export async function apiRequest<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (!(options.body instanceof FormData)) {
    headers["Content-Type"] = "application/json"
  }
  if (authToken) {
    headers["Authorization"] = `Bearer ${authToken}`
  }
  let resp: Response
  try {
    resp = await fetch(path, { method: options.method ?? "GET", headers, body: options.body, signal: options.signal })
  } catch (err) {
    throw new ApiError(err instanceof Error ? err.message : "network error")
  }
  if (resp.status === 401) {
    setAuthToken(null)
    onUnauthorized?.()
    throw new ApiError("unauthorized", 401)
  }
  const text = await resp.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!resp.ok) {
    const msg =
      data && typeof data === "object" && "error" in data && typeof (data as { error: unknown }).error === "string"
        ? (data as { error: string }).error
        : `HTTP ${resp.status}`
    throw new ApiError(msg, resp.status)
  }
  return data as T
}
