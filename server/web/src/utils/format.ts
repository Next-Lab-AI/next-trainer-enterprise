export function formatTime(ts: number | null | undefined): string {
  if (!ts) return "-"
  return new Date(ts * 1000).toLocaleString()
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ["KiB", "MiB", "GiB", "TiB"]
  let v = n
  let i = -1
  do {
    v /= 1024
    i++
  } while (v >= 1024 && i < units.length - 1)
  return `${v.toFixed(1)} ${units[i]}`
}

export function shortId(id: string): string {
  return id.length > 10 ? `${id.slice(0, 10)}…` : id
}

export function statusTagType(status: string): "success" | "warning" | "danger" | "info" | "primary" {
  switch (status) {
    case "FINISHED":
      return "success"
    case "RUNNING":
      return "primary"
    case "QUEUED":
    case "WAITING":
    case "CREATED":
      return "warning"
    case "FAILED":
      return "danger"
    case "TERMINATED":
      return "info"
    default:
      return "info"
  }
}
