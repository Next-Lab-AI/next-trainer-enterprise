import { apiRequest } from "./client"
import type { AccessTokenInfo, Dataset, Job, JobEvent, Node, Pool, User } from "./types"

export const nodesApi = {
  list: () => apiRequest<Node[]>("/api/nodes"),
  get: (id: string) => apiRequest<Node>(`/api/nodes/${encodeURIComponent(id)}`),
  update: (id: string, description: string, labels: Record<string, string>) =>
    apiRequest(`/api/nodes/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify({ description, labels }),
    }),
  revoke: (id: string) => apiRequest(`/api/nodes/${encodeURIComponent(id)}/revoke`, { method: "POST", body: "{}" }),
  envGc: (id: string, olderThanDays?: number) =>
    apiRequest<{ command_id: string }>(`/api/nodes/${encodeURIComponent(id)}/env_gc`, {
      method: "POST",
      body: JSON.stringify({ older_than_days: olderThanDays ?? 30 }),
    }),
  provision: (id: string, req: ProvisionRequest) =>
    apiRequest<{ command_id: string }>(`/api/nodes/${encodeURIComponent(id)}/provision`, {
      method: "POST",
      body: JSON.stringify(req),
    }),
}

export interface ProvisionRequest {
  engine: string
  version: string
  package: { uri: string; sha256: string; size?: number }
  constraints?: string
  env_mode?: string
  base_python?: string
  download_sources?: {
    pip_index?: string
    pytorch_index?: string
    hf_endpoint?: string
    github_prefix?: string
  }
}

export const poolsApi = {
  list: () => apiRequest<Pool[]>("/api/pools"),
  create: (name: string) => apiRequest<Pool>("/api/pools", { method: "POST", body: JSON.stringify({ name }) }),
  remove: (id: string) => apiRequest(`/api/pools/${encodeURIComponent(id)}`, { method: "DELETE" }),
  assignNode: (poolId: string, nodeId: string, assign: boolean) =>
    apiRequest(`/api/pools/${encodeURIComponent(poolId)}/nodes`, {
      method: "POST",
      body: JSON.stringify({ node_id: nodeId, assign }),
    }),
  grantUser: (poolId: string, userId: string, grant: boolean) =>
    apiRequest(`/api/pools/${encodeURIComponent(poolId)}/users`, {
      method: "POST",
      body: JSON.stringify({ user_id: userId, grant }),
    }),
  grantDataset: (poolId: string, datasetId: string, grant: boolean) =>
    apiRequest(`/api/pools/${encodeURIComponent(poolId)}/datasets`, {
      method: "POST",
      body: JSON.stringify({ dataset_id: datasetId, grant }),
    }),
}

export interface SubmitJobRequest {
  pool_id: string
  engine: string
  engine_version?: string
  params: Record<string, unknown>
  dataset_ids: string[]
  gpus: number
  gpu_model?: string
}

export const jobsApi = {
  list: (poolId?: string) => apiRequest<Job[]>(`/api/jobs${poolId ? `?pool_id=${encodeURIComponent(poolId)}` : ""}`),
  get: (id: string) => apiRequest<Job>(`/api/jobs/${encodeURIComponent(id)}`),
  submit: (req: SubmitJobRequest) => apiRequest<Job>("/api/jobs", { method: "POST", body: JSON.stringify(req) }),
  stop: (id: string, preempt = false) =>
    apiRequest(`/api/jobs/${encodeURIComponent(id)}/stop`, { method: "POST", body: JSON.stringify({ preempt }) }),
  kill: (id: string) => apiRequest(`/api/jobs/${encodeURIComponent(id)}/kill`, { method: "POST", body: "{}" }),
  events: (id: string, afterSeq = 0) =>
    apiRequest<JobEvent[]>(`/api/jobs/${encodeURIComponent(id)}/events?after_seq=${afterSeq}`),
  logsUrl: (id: string) => `/api/jobs/${encodeURIComponent(id)}/logs/stream`,
}

export const datasetsApi = {
  list: () => apiRequest<Dataset[]>("/api/datasets"),
  upload: (file: File, name: string, kind: string, version?: string) => {
    const form = new FormData()
    form.append("name", name)
    form.append("kind", kind)
    if (version) form.append("version", version)
    form.append("file", file)
    return apiRequest<Dataset>("/api/datasets", { method: "POST", body: form })
  },
  remove: (id: string) => apiRequest(`/api/datasets/${encodeURIComponent(id)}`, { method: "DELETE" }),
}

export const adminApi = {
  listUsers: () => apiRequest<User[]>("/api/users"),
  createUser: (username: string, password: string, role: string) =>
    apiRequest<User>("/api/users", { method: "POST", body: JSON.stringify({ username, password, role }) }),
  updateUser: (id: string, patch: { role?: string; disabled?: boolean; password?: string }) =>
    apiRequest<User>(`/api/users/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(patch) }),
  listTokens: () => apiRequest<AccessTokenInfo[]>("/api/tokens"),
  createToken: (label: string, expiresAt?: number) =>
    apiRequest<{ token: string; id: string }>("/api/tokens", {
      method: "POST",
      body: JSON.stringify({ label, expires_at: expiresAt ?? null }),
    }),
  revokeToken: (id: string) => apiRequest(`/api/tokens/${encodeURIComponent(id)}/revoke`, { method: "POST", body: "{}" }),
  getRegistration: () => apiRequest<{ open: boolean }>("/api/settings/registration"),
  setRegistration: (open: boolean) =>
    apiRequest<{ open: boolean }>("/api/settings/registration", { method: "PUT", body: JSON.stringify({ open }) }),
}
