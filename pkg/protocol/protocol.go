package protocol

const Version = 1

const (
	TypeHello       = "hello"
	TypeWelcome     = "welcome"
	TypeHeartbeat   = "heartbeat"
	TypeProvision   = "provision"
	TypeEnvGC       = "env.gc"
	TypeJobStart    = "job.start"
	TypeJobStop     = "job.stop"
	TypeJobKill     = "job.kill"
	TypeEvent       = "event"
	TypeMetrics     = "metrics"
	TypeLog         = "log"
	TypeAssetReady  = "asset.ready"
	TypeAssetFailed = "asset.failed"
	TypeFile        = "file"
)

const (
	EventJobCreated      = "job.created"
	EventJobWaiting      = "job.waiting"
	EventJobStarting     = "job.starting"
	EventJobProgress     = "job.progress"
	EventJobFinished     = "job.finished"
	EventJobFailed       = "job.failed"
	EventJobTerminated   = "job.terminated"
	EventGap             = "gap"
	EventProvisionReady  = "provision.ready"
	EventProvisionBroken = "provision.broken"
	EventEnvGCDone       = "env.gc.done"
	EventNodeEnvs        = "node.envs"
)

const (
	CloseBadToken            = "bad_token"
	CloseRevoked             = "revoked"
	CloseWindowClosed        = "window_closed"
	CloseVersionIncompatible = "version_incompatible"
)

const (
	LaneCompute     = "compute"
	LaneMaintenance = "maintenance"
)
