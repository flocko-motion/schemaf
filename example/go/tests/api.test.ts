import { Api } from "../../frontend/src/api/generated/api.gen"

export async function testHealth(baseUrl: string) {
  const resp = await fetch(`${baseUrl}/api/health`)
  if (!resp.ok) throw new Error(`/api/health returned ${resp.status}`)
  const body = await resp.json()
  if (body.status !== "ok") throw new Error(`health status: ${body.status}, db: ${body.db}`)
}

// manual: Go wrapper in auth_test.go (requires TEST_TOKEN env var)
export async function testUserAuthFlow(baseUrl: string) {
  const token = process.env.TEST_TOKEN
  if (!token) throw new Error("TEST_TOKEN not set")

  // Unauthenticated request must be rejected.
  const unauth = await fetch(`${baseUrl}/api/user/me`)
  if (unauth.status !== 401) throw new Error(`expected 401 without token, got ${unauth.status}`)

  // Authenticated request — Api class configured with Bearer token for all requests.
  const _api = new Api({
    baseUrl,
    baseApiParams: { headers: { Authorization: `Bearer ${token}` } },
  })
  // /api/user/me is a framework built-in not in the generated client; call directly.
  const resp = await fetch(`${baseUrl}/api/user/me`, {
    headers: { Authorization: `Bearer ${token}` },
  })
  if (!resp.ok) throw new Error(`/api/user/me returned ${resp.status}`)
  const body = await resp.json()
  if (!body.id) throw new Error(`expected id in response, got: ${JSON.stringify(body)}`)
  if (body.id !== "test-user-uuid") throw new Error(`expected id "test-user-uuid", got "${body.id}"`)
}

export async function testClockTime(baseUrl: string) {
  const api = new Api({ baseUrl })
  const { data } = await api.api.getApiTime()
  const serverTime = new Date(data!.time).getTime()
  const now = Date.now()
  const diffSeconds = Math.abs(serverTime - now) / 1000
  if (diffSeconds > 5) throw new Error(`clock skew too large: ${diffSeconds}s`)
}
