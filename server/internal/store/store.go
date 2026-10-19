package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

const (
	NodeStatusOnline  = "online"
	NodeStatusOffline = "offline"
	NodeStatusRevoked = "revoked"
)

const (
	DatasetKindDataset  = "dataset"
	DatasetKindArtifact = "artifact"
	DatasetKindEngine   = "engine"
)

const (
	BackendLocal = "local"
	BackendS3    = "s3"
)

const (
	JobStatusQueued     = "QUEUED"
	JobStatusCreated    = "CREATED"
	JobStatusWaiting    = "WAITING"
	JobStatusRunning    = "RUNNING"
	JobStatusFinished   = "FINISHED"
	JobStatusFailed     = "FAILED"
	JobStatusTerminated = "TERMINATED"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAt    int64
	UpdatedAt    int64
}

type AccessToken struct {
	ID        string
	TokenHash string
	Label     string
	CreatedBy string
	CreatedAt int64
	ExpiresAt *int64
	RevokedAt *int64
}

type Node struct {
	NodeID          string
	CredentialHash  string
	Hostname        string
	Description     string
	AgentVersion    string
	ProtocolVersion string
	Labels          []byte
	GPUs            []byte
	Status          string
	FirstSeen       int64
	LastSeen        int64
}

type Pool struct {
	PoolID      string
	Name        string
	AutoCreated bool
	CreatedAt   int64
}

type Dataset struct {
	DatasetID string
	Name      string
	Version   string
	Kind      string
	Backend   string
	URI       string
	SHA256    string
	SizeBytes int64
	CreatedBy string
	CreatedAt int64
	JobID     string
	DeletedAt *int64
	EncAlgo   string
	EncKey    string
}

type Job struct {
	JobID         string
	UserID        string
	PoolID        string
	NodeID        string
	Engine        string
	EngineVersion string
	ConfigTOML    string
	Assets        []byte
	GPUs          []byte
	Status        string
	ExitCode      *int64
	Request       []byte
	CreatedAt     int64
	StartedAt     *int64
	EndedAt       *int64
}

type Event struct {
	Seq      int64
	NodeID   string
	JobID    string
	Type     string
	Payload  []byte
	ServerTS int64
	AgentSeq *int64
	ElapsedS *int64
	Gap      bool
}

type Command struct {
	CommandID string
	NodeID    string
	Type      string
	Payload   []byte
	IssuedBy  string
	IssuedAt  int64
	AckedAt   *int64
}

type JobFilter struct {
	UserID string
	PoolID string
	Status []string
	Limit  int
	Offset int
}

type Users interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Update(ctx context.Context, u *User) error
	Count(ctx context.Context) (int64, error)
}

type AccessTokens interface {
	Create(ctx context.Context, t *AccessToken) error
	GetByHash(ctx context.Context, hash string) (*AccessToken, error)
	List(ctx context.Context) ([]*AccessToken, error)
	Revoke(ctx context.Context, id string, revokedAt int64) error
}

type Nodes interface {
	Create(ctx context.Context, n *Node) error
	Get(ctx context.Context, nodeID string) (*Node, error)
	List(ctx context.Context) ([]*Node, error)
	UpdateHeartbeat(ctx context.Context, nodeID string, lastSeen int64, status string) error
	UpdateFacts(ctx context.Context, nodeID, hostname, agentVersion, protocolVersion string, labels, gpus []byte) error
	UpdateMeta(ctx context.Context, nodeID, description string, labels []byte) error
	SetStatus(ctx context.Context, nodeID, status string) error
	MarkOffline(ctx context.Context, nodeID string) error
}

type Pools interface {
	Create(ctx context.Context, p *Pool) error
	Get(ctx context.Context, poolID string) (*Pool, error)
	GetByName(ctx context.Context, name string) (*Pool, error)
	List(ctx context.Context) ([]*Pool, error)
	Delete(ctx context.Context, poolID string) error
	AssignNode(ctx context.Context, nodeID, poolID string) error
	UnassignNode(ctx context.Context, nodeID, poolID string) error
	NodesOf(ctx context.Context, poolID string) ([]string, error)
	PoolsOfNode(ctx context.Context, nodeID string) ([]string, error)
	GrantUser(ctx context.Context, userID, poolID string) error
	RevokeUser(ctx context.Context, userID, poolID string) error
	PoolsOfUser(ctx context.Context, userID string) ([]string, error)
	UsersOf(ctx context.Context, poolID string) ([]string, error)
	GrantDataset(ctx context.Context, datasetID, poolID string) error
	RevokeDataset(ctx context.Context, datasetID, poolID string) error
	PoolsOfDataset(ctx context.Context, datasetID string) ([]string, error)
}

type Datasets interface {
	Create(ctx context.Context, d *Dataset) error
	Get(ctx context.Context, datasetID string) (*Dataset, error)
	List(ctx context.Context, includeDeleted bool) ([]*Dataset, error)
	ListByJob(ctx context.Context, jobID string) ([]*Dataset, error)
	SoftDelete(ctx context.Context, datasetID string, deletedAt int64) error
}

type Jobs interface {
	CreateJob(ctx context.Context, j *Job) error
	GetJob(ctx context.Context, jobID string) (*Job, error)
	ListJobs(ctx context.Context, f JobFilter) ([]*Job, error)
	AssignJob(ctx context.Context, jobID, nodeID string, gpus []byte) error
	UpdateJobStatus(ctx context.Context, jobID, status string, exitCode *int64, startedAt, endedAt *int64) error
	AppendEvents(ctx context.Context, events []*Event) error
	ListEvents(ctx context.Context, jobID string, afterSeq int64, limit int) ([]*Event, error)
	CreateCommand(ctx context.Context, c *Command) error
	AckCommand(ctx context.Context, commandID string, ackedAt int64) error
	ListCommands(ctx context.Context, nodeID string, limit int) ([]*Command, error)
}

type Settings interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

type Store interface {
	Users() Users
	AccessTokens() AccessTokens
	Nodes() Nodes
	Pools() Pools
	Datasets() Datasets
	Jobs() Jobs
	Settings() Settings
	Tx(ctx context.Context, fn func(s Store) error) error
	Close() error
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
