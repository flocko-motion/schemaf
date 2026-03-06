import { Api } from "../../frontend/src/api/generated/api.gen"

export async function testHealth(baseUrl: string) {
  const resp = await fetch(`${baseUrl}/api/health`)
  if (!resp.ok) throw new Error(`/api/health returned ${resp.status}`)
  const body = await resp.json()
  if (body.status !== "ok") throw new Error(`health status: ${body.status}, db: ${body.db}`)
}

// skip: requires clock docker service
export async function testClockTime(baseUrl: string) {
  const api = new Api({ baseUrl })
  const { data } = await api.api.getApiTime()
  const serverTime = new Date(data!.time).getTime()
  const now = Date.now()
  const diffSeconds = Math.abs(serverTime - now) / 1000
  if (diffSeconds > 5) throw new Error(`clock skew too large: ${diffSeconds}s`)
}
