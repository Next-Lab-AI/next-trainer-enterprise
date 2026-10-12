export interface User {
  id: string
  username: string
  role: "admin" | "user"
  disabled: boolean
  created_at: number
}

export interface Node {
  node_id: string
  credential_hash?: string
  hostname: string
  description: string
  agent_version: string
  protocol_version: string
  labels: string
  gpus: string
  status: string
  first_seen: number
  last_seen: number
  online: boolean
  pools: string[]
  base_envs?: string
}

export interface GpuInfo {
  index: number
  uuid: string
  model: string
  vendor: string
  mem_total_mb: number
}

export interface BaseEnv {
  python: string
  torch?: string
  cuda?: string
}

export interface Pool {
  pool_id: string
  name: string
  auto_created: boolean
  created_at: number
}

export interface Job {
  job_id: string
  user_id: string
  pool_id: string
  node_id: string
  engine: string
  engine_version: string
  config_toml: string
  assets: string
  gpus: string
  status: string
  exit_code: number | null
  request: string
  created_at: number
  started_at: number | null
  ended_at: number | null
}

export interface JobEvent {
  seq: number
  node_id: string
  job_id: string
  type: string
  payload: string
  server_ts: number
  agent_seq: number | null
  elapsed_s: number | null
  gap: boolean
}

export interface Dataset {
  dataset_id: string
  name: string
  version: string
  kind: string
  backend: string
  uri: string
  sha256: string
  size_bytes: number
  created_by: string
  created_at: number
  job_id: string
  deleted_at: number | null
}

export interface AccessTokenInfo {
  id: string
  label: string
  created_by: string
  created_at: number
  expires_at: number | null
  revoked_at: number | null
}
