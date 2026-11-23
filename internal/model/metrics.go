package model

import "time"

// Snapshot represents a point-in-time collection of all metrics.
type Snapshot struct {
	CollectedAt time.Time
	ServerInfo  ServerInfo
	Overview    Overview
	Activities  []Activity
	Queries     []QueryStat
	Tables      []TableStat
	Indexes     []IndexStat
	Locks       []LockInfo
	Replication []ReplicationStat
	Settings    []Setting
	Clients     []ClientStat

	// Additional monitoring data from PG catalog views.
	WALStats          *WALStats
	CheckpointerStats *CheckpointerStats
	BGWriterStats     *BGWriterStats
	ArchiverStats     *ArchiverStats
	IOStats           []IOStat
	ConflictStats     *ConflictStats
	TableIOStats      []TableIOStat
	VacuumProgress    []VacuumProgress
}

// ServerInfo holds PostgreSQL server metadata.
type ServerInfo struct {
	Version             string
	VersionNum          int
	StartTime           time.Time
	Uptime              time.Duration
	MaxConnections      int
	DataDirectory       string
	Extensions          []string // detected extensions
	HasStatStatements   bool
	HasStatIO           bool // PG 16+
	HasStatWAL          bool // PG 14+
	HasStatCheckpointer bool // PG 17+ (separated from bgwriter)
	IsStandby           bool
	TrackIOTiming       bool

	// Catalog lists the statistics views/columns present on this server.
	// Excluded from snapshot exports; it is static per connection.
	Catalog Catalog `json:"-"`
}

// Overview holds the high-level dashboard metrics.
type Overview struct {
	// Connections
	ActiveConnections   int
	IdleConnections     int
	IdleInTxConnections int
	TotalConnections    int
	MaxConnections      int

	// Throughput (per second, computed as deltas)
	TxCommitRate   float64
	TxRollbackRate float64
	TupInsertRate  float64
	TupUpdateRate  float64
	TupDeleteRate  float64
	TupFetchRate   float64

	// Cache
	CacheHitRatio float64

	// Checkpoints
	CheckpointsReq      int64
	CheckpointsTimed    int64
	CheckpointWriteTime float64
	CheckpointSyncTime  float64

	// Conflicts
	Deadlocks int64
	Conflicts int64
	TempFiles int64
	TempBytes int64

	// Database size
	DatabaseSize int64
	DatabaseName string

	// Session stats (PG 14+).
	SessionTime       float64
	ActiveTime        float64
	IdleInTxTime      float64
	Sessions          int64
	SessionsAbandoned int64
	SessionsFatal     int64
	SessionsKilled    int64

	// Transaction ID wraparound.
	OldestXactAge    int64
	OldestXactDB     string
	DataFrozenXIDAge int64

	// Rollback ratio.
	RollbackRatio float64
}

// Activity represents a row from pg_stat_activity.
type Activity struct {
	PID             int
	DatabaseName    string
	UserName        string
	ApplicationName string
	ClientAddr      string
	State           string
	WaitEventType   string
	WaitEvent       string
	Query           string
	QueryStart      time.Time
	Duration        time.Duration
	BackendStart    time.Time
	XactStart       time.Time
	StateChange     time.Time
	LeaderPID       int
	BackendType     string
	BackendXID      string
	BackendXmin     string
}

// QueryStat represents a row from pg_stat_statements.
type QueryStat struct {
	QueryID         int64
	Query           string
	DatabaseName    string
	UserName        string
	Calls           int64
	TotalExecTime   float64
	MeanExecTime    float64
	MinExecTime     float64
	MaxExecTime     float64
	Rows            int64
	SharedBlksHit   int64
	SharedBlksRead  int64
	CacheHitRatio   float64
	TotalPlanTime   float64
	MeanPlanTime    float64
	StddevExecTime  float64
	TempBlksRead    int64
	TempBlksWritten int64
	LocalBlksHit    int64
	LocalBlksRead   int64
	WALRecords      int64
	WALBytes        int64
	WALFPI          int64
	JITFunctions    int64
	JITTime         float64
}

// TableStat represents stats about a single table.
type TableStat struct {
	SchemaName       string
	TableName        string
	SeqScan          int64
	SeqTupRead       int64
	IdxScan          int64
	IdxTupFetch      int64
	InsertCount      int64
	UpdateCount      int64
	DeleteCount      int64
	HotUpdateCount   int64
	LiveTuples       int64
	DeadTuples       int64
	TableSize        int64
	TotalSize        int64
	LastVacuum       *time.Time
	LastAutoVacuum   *time.Time
	LastAnalyze      *time.Time
	LastAutoAnalyze  *time.Time
	VacuumCount      int64
	AutoVacuumCount  int64
	AnalyzeCount     int64
	AutoAnalyzeCount int64
	ModsSinceAnalyze int64
	InsSinceVacuum   int64
	DeadTupleRatio   float64
	SeqScanRatio     float64
}

// TableIOStat represents I/O stats from pg_statio_user_tables.
type TableIOStat struct {
	SchemaName    string
	TableName     string
	HeapBlksRead  int64
	HeapBlksHit   int64
	IdxBlksRead   int64
	IdxBlksHit    int64
	ToastBlksRead int64
	ToastBlksHit  int64
	TidxBlksRead  int64
	TidxBlksHit   int64
	CacheHitRatio float64
}

// IndexStat represents stats about a single index.
type IndexStat struct {
	SchemaName string
	TableName  string
	IndexName  string
	IndexScan  int64
	TupRead    int64
	TupFetch   int64
	IndexSize  int64
	IsUnique   bool
	IsValid    bool
}

// LockInfo represents a lock with blocking information.
type LockInfo struct {
	PID           int
	LockType      string
	DatabaseName  string
	RelationName  string
	Mode          string
	Granted       bool
	WaitStart     *time.Time
	BlockedByPIDs []int
	Query         string
	UserName      string
	Duration      time.Duration
}

// ReplicationStat represents a replication connection.
type ReplicationStat struct {
	PID             int
	UserName        string
	ApplicationName string
	ClientAddr      string
	State           string
	SentLSN         string
	WriteLSN        string
	FlushLSN        string
	ReplayLSN       string
	WriteLag        *time.Duration
	FlushLag        *time.Duration
	ReplayLag       *time.Duration
	SyncState       string
	SyncPriority    int
	ReplyTime       *time.Time
}

// Setting represents a PostgreSQL configuration parameter.
type Setting struct {
	Name        string
	Value       string
	Unit        string
	Category    string
	Description string
	Source      string
	Context     string // pg_settings.context; "postmaster" means a restart is needed to change it
}

// ClientStat holds aggregated metrics for a single client address.
type ClientStat struct {
	ClientAddr          string
	TotalConnections    int
	ActiveConnections   int
	IdleConnections     int
	IdleInTxConnections int
	WaitingConnections  int
	LongestQueryDur     time.Duration
	LongestTxDur        time.Duration
	Databases           []string
	Users               []string
	Applications        []string
	Backends            []BackendThread
}

// BackendThread holds per-backend-process (per-thread) metrics.
type BackendThread struct {
	PID             int
	ClientAddr      string
	DatabaseName    string
	UserName        string
	ApplicationName string
	BackendType     string
	State           string
	WaitEventType   string
	WaitEvent       string
	Query           string
	QueryStart      time.Time
	QueryDuration   time.Duration
	XactStart       time.Time
	TxDuration      time.Duration
	BackendStart    time.Time
	Uptime          time.Duration
	StateChange     time.Time
	LeaderPID       int
	BackendMemoryKB int64
}

// WALStats from pg_stat_wal (PG 14+).
type WALStats struct {
	WALRecords     int64
	WALFPI         int64
	WALBytes       int64
	WALBuffersFull int64
	WALWrite       int64
	WALSync        int64
	WALWriteTime   float64
	WALSyncTime    float64
	StatsReset     *time.Time
}

// CheckpointerStats from pg_stat_checkpointer (PG 17+).
type CheckpointerStats struct {
	NumTimed           int64
	NumRequested       int64
	NumDone            int64
	RestartpointsTimed int64
	RestartpointsReq   int64
	RestartpointsDone  int64
	WriteTime          float64
	SyncTime           float64
	BuffersWritten     int64
	StatsReset         *time.Time
}

// BGWriterStats from pg_stat_bgwriter.
type BGWriterStats struct {
	BuffersClean    int64
	MaxWrittenClean int64
	BuffersAlloc    int64
	StatsReset      *time.Time
}

// ArchiverStats from pg_stat_archiver.
type ArchiverStats struct {
	ArchivedCount    int64
	LastArchivedWAL  string
	LastArchivedTime *time.Time
	FailedCount      int64
	LastFailedWAL    string
	LastFailedTime   *time.Time
	StatsReset       *time.Time
}

// IOStat from pg_stat_io (PG 16+).
type IOStat struct {
	BackendType string
	Object      string
	Context     string
	Reads       int64
	ReadTime    float64
	Writes      int64
	WriteTime   float64
	Extends     int64
	ExtendTime  float64
	Hits        int64
	Evictions   int64
	Reuses      int64
	Fsyncs      int64
	FsyncTime   float64
	StatsReset  *time.Time
}

// ConflictStats from pg_stat_database_conflicts.
type ConflictStats struct {
	ConflTablespace  int64
	ConflLock        int64
	ConflSnapshot    int64
	ConflBufferpin   int64
	ConflDeadlock    int64
	ConflLogicalSlot int64
}

// VacuumProgress from pg_stat_progress_vacuum.
type VacuumProgress struct {
	PID              int
	DatabaseName     string
	TableSchema      string
	TableName        string
	Phase            string
	HeapBlksTotal    int64
	HeapBlksScanned  int64
	HeapBlksVacuumed int64
	IndexVacuumCount int64
	MaxDeadTuples    int64
	NumDeadTuples    int64
}
