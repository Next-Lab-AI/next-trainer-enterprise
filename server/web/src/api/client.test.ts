// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"

import { ApiError, apiRequest, getAuthToken, setAuthToken } from "./client"

afterEach(() => {
  vi.unstubAllGlobals()
  setAuthToken(null)
})

describe("apiRequest", () => {
  it("parses json response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 })))
    const data = await apiRequest<{ ok: boolean }>("/api/x")
    expect(data.ok).toBe(true)
  })

  it("throws ApiError with server message on non-2xx", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: "forbidden" }), { status: 403 })))
    await expect(apiRequest("/api/x")).rejects.toMatchObject({ message: "forbidden", httpStatus: 403 })
  })

  it("clears token and calls handler on 401", async () => {
    setAuthToken("abc")
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 401 })))
    await expect(apiRequest("/api/x")).rejects.toBeInstanceOf(ApiError)
    expect(getAuthToken()).toBeNull()
  })

  it("sends bearer token when set", async () => {
    setAuthToken("tok")
    const spy = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response("{}", { status: 200 }))
    vi.stubGlobal("fetch", spy)
    await apiRequest("/api/x")
    const init = spy.mock.calls[0][1]
    expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer tok")
  })
})
