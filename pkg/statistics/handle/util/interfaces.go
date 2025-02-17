// Copyright 2023 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"context"
	"time"

	"github.com/pingcap/tidb/pkg/infoschema"
	"github.com/pingcap/tidb/pkg/parser/model"
	"github.com/pingcap/tidb/pkg/sessionctx"
	"github.com/pingcap/tidb/pkg/statistics"
	"github.com/pingcap/tidb/pkg/types"
	"github.com/pingcap/tidb/pkg/util/sqlexec"
	"github.com/tiancaiamao/gp"
)

// StatsGC is used to GC unnecessary stats.
type StatsGC interface {
	// GCStats will garbage collect the useless stats' info.
	// For dropped tables, we will first update their version
	// so that other tidb could know that table is deleted.
	GCStats(is infoschema.InfoSchema, ddlLease time.Duration) (err error)

	// ClearOutdatedHistoryStats clear outdated historical stats.
	// Only for test.
	ClearOutdatedHistoryStats() error

	// DeleteTableStatsFromKV deletes table statistics from kv.
	// A statsID refers to statistic of a table or a partition.
	DeleteTableStatsFromKV(statsIDs []int64) (err error)
}

// ColStatsTimeInfo records usage information of this column stats.
type ColStatsTimeInfo struct {
	LastUsedAt     *types.Time // last time the column is used
	LastAnalyzedAt *types.Time // last time the column is analyzed
}

// StatsUsage is used to track the usage of column / index statistics.
type StatsUsage interface {
	// Below methods are for predicated columns.

	// LoadColumnStatsUsage returns all columns' usage information.
	LoadColumnStatsUsage(loc *time.Location) (map[model.TableItemID]ColStatsTimeInfo, error)

	// GetPredicateColumns returns IDs of predicate columns, which are the columns whose stats are used(needed) when generating query plans.
	GetPredicateColumns(tableID int64) ([]int64, error)

	// CollectColumnsInExtendedStats returns IDs of the columns involved in extended stats.
	CollectColumnsInExtendedStats(tableID int64) ([]int64, error)

	// Below methods are for index usage.

	// NewSessionIndexUsageCollector creates a new IndexUsageCollector on the list.
	// The returned value's type should be *usage.SessionIndexUsageCollector, use interface{} to avoid cycle import now.
	// TODO: use *usage.SessionIndexUsageCollector instead of interface{}.
	NewSessionIndexUsageCollector() interface{}

	// DumpIndexUsageToKV dumps all collected index usage info to storage.
	DumpIndexUsageToKV() error

	// GCIndexUsage removes unnecessary index usage data.
	GCIndexUsage() error

	// Blow methods are for table delta and stats usage.

	// NewSessionStatsItem allocates a stats collector for a session.
	// TODO: use interface{} to avoid cycle import, remove this interface{}.
	NewSessionStatsItem() interface{}

	// ResetSessionStatsList resets the sessions stats list.
	ResetSessionStatsList()

	// DumpStatsDeltaToKV sweeps the whole list and updates the global map, then we dumps every table that held in map to KV.
	DumpStatsDeltaToKV(dumpAll bool) error

	// DumpColStatsUsageToKV sweeps the whole list, updates the column stats usage map and dumps it to KV.
	DumpColStatsUsageToKV() error
}

// StatsHistory is used to manage historical stats.
type StatsHistory interface {
	// RecordHistoricalStatsMeta records stats meta of the specified version to stats_meta_history.
	RecordHistoricalStatsMeta(tableID int64, version uint64, source string)

	// CheckHistoricalStatsEnable check whether historical stats is enabled.
	CheckHistoricalStatsEnable() (enable bool, err error)

	// RecordHistoricalStatsToStorage records the given table's stats data to mysql.stats_history
	RecordHistoricalStatsToStorage(dbName string, tableInfo *model.TableInfo, physicalID int64, isPartition bool) (uint64, error)
}

// StatsAnalyze is used to handle auto-analyze and manage analyze jobs.
type StatsAnalyze interface {
	// InsertAnalyzeJob inserts analyze job into mysql.analyze_jobs and gets job ID for further updating job.
	InsertAnalyzeJob(job *statistics.AnalyzeJob, instance string, procID uint64) error

	// DeleteAnalyzeJobs deletes the analyze jobs whose update time is earlier than updateTime.
	DeleteAnalyzeJobs(updateTime time.Time) error

	// HandleAutoAnalyze analyzes the newly created table or index.
	HandleAutoAnalyze(is infoschema.InfoSchema) (analyzed bool)

	// CheckAnalyzeVersion checks whether all the statistics versions of this table's columns and indexes are the same.
	CheckAnalyzeVersion(tblInfo *model.TableInfo, physicalIDs []int64, version *int) bool
}

// StatsCache is used to manage all table statistics in memory.
type StatsCache interface {
	// Close closes this cache.
	Close()

	// Clear clears this cache.
	Clear()

	// Update reads stats meta from store and updates the stats map.
	Update(is infoschema.InfoSchema) error

	// MemConsumed returns its memory usage.
	MemConsumed() (size int64)

	// Get returns the specified table's stats.
	Get(tableID int64) (*statistics.Table, bool)

	// Put puts this table stats into the cache.
	Put(tableID int64, t *statistics.Table)

	// UpdateStatsCache updates the cache.
	UpdateStatsCache(addedTables []*statistics.Table, deletedTableIDs []int64)

	// MaxTableStatsVersion returns the version of the current cache, which is defined as
	// the max table stats version the cache has in its lifecycle.
	MaxTableStatsVersion() uint64

	// Values returns all values in this cache.
	Values() []*statistics.Table

	// Len returns the length of this cache.
	Len() int

	// SetStatsCacheCapacity sets the cache's capacity.
	SetStatsCacheCapacity(capBytes int64)

	// Replace replaces this cache.
	Replace(cache StatsCache)

	// UpdateStatsHealthyMetrics updates stats healthy distribution metrics according to stats cache.
	UpdateStatsHealthyMetrics()
}

// StatsLockTable is the table info of which will be locked.
type StatsLockTable struct {
	PartitionInfo map[int64]string
	// schema name + table name.
	FullName string
}

// StatsLock is used to manage locked stats.
type StatsLock interface {
	// LockTables add locked tables id to store.
	// - tables: tables that will be locked.
	// Return the message of skipped tables and error.
	LockTables(tables map[int64]*StatsLockTable) (skipped string, err error)

	// LockPartitions add locked partitions id to store.
	// If the whole table is locked, then skip all partitions of the table.
	// - tid: table id of which will be locked.
	// - tableName: table name of which will be locked.
	// - pidNames: partition ids of which will be locked.
	// Return the message of skipped tables and error.
	// Note: If the whole table is locked, then skip all partitions of the table.
	LockPartitions(
		tid int64,
		tableName string,
		pidNames map[int64]string,
	) (skipped string, err error)

	// RemoveLockedTables remove tables from table locked records.
	// - tables: tables of which will be unlocked.
	// Return the message of skipped tables and error.
	RemoveLockedTables(tables map[int64]*StatsLockTable) (skipped string, err error)

	// RemoveLockedPartitions remove partitions from table locked records.
	// - tid: table id of which will be unlocked.
	// - tableName: table name of which will be unlocked.
	// - pidNames: partition ids of which will be unlocked.
	// Note: If the whole table is locked, then skip all partitions of the table.
	RemoveLockedPartitions(
		tid int64,
		tableName string,
		pidNames map[int64]string,
	) (skipped string, err error)

	// GetLockedTables returns the locked status of the given tables.
	// Note: This function query locked tables from store, so please try to batch the query.
	GetLockedTables(tableIDs ...int64) (map[int64]struct{}, error)

	// GetTableLockedAndClearForTest for unit test only.
	GetTableLockedAndClearForTest() (map[int64]struct{}, error)
}

// StatsReadWriter is used to read and write stats to the storage.
type StatsReadWriter interface {
	// TableStatsFromStorage loads table stats info from storage.
	TableStatsFromStorage(tableInfo *model.TableInfo, physicalID int64, loadAll bool, snapshot uint64) (statsTbl *statistics.Table, err error)

	// StatsMetaCountAndModifyCount reads count and modify_count for the given table from mysql.stats_meta.
	StatsMetaCountAndModifyCount(tableID int64) (count, modifyCount int64, err error)

	// LoadNeededHistograms will load histograms for those needed columns/indices and put them into the cache.
	LoadNeededHistograms() (err error)

	// ReloadExtendedStatistics drops the cache for extended statistics and reload data from mysql.stats_extended.
	ReloadExtendedStatistics() error

	//// SaveTableStatsToStorage saves the stats of a table to storage.
	//SaveTableStatsToStorage(results *statistics.AnalyzeResults, analyzeSnapshot bool, source string) (err error)

	// SaveStatsToStorage save the stats data to the storage.
	SaveStatsToStorage(tableID int64, count, modifyCount int64, isIndex int, hg *statistics.Histogram,
		cms *statistics.CMSketch, topN *statistics.TopN, statsVersion int, isAnalyzed int64, updateAnalyzeTime bool, source string) (err error)

	// SaveMetaToStorage saves stats meta to the storage.
	SaveMetaToStorage(tableID, count, modifyCount int64, source string) (err error)

	// SaveStatsFromJSON saves stats from JSON to the storage.
	// TODO: use *storage.JSONTable instead of interface{} (which is used to avoid cycle import).
	SaveStatsFromJSON(tableInfo *model.TableInfo, physicalID int64, jsonTbl interface{}) error

	// TableStatsToJSON dumps table stats to JSON.
	TableStatsToJSON(dbName string, tableInfo *model.TableInfo, physicalID int64, snapshot uint64) (*JSONTable, error)

	// DumpStatsToJSON dumps statistic to json.
	DumpStatsToJSON(dbName string, tableInfo *model.TableInfo,
		historyStatsExec sqlexec.RestrictedSQLExecutor, dumpPartitionStats bool) (*JSONTable, error)

	// DumpHistoricalStatsBySnapshot dumped json tables from mysql.stats_meta_history and mysql.stats_history.
	// As implemented in getTableHistoricalStatsToJSONWithFallback, if historical stats are nonexistent, it will fall back
	// to the latest stats, and these table names (and partition names) will be returned in fallbackTbls.
	DumpHistoricalStatsBySnapshot(
		dbName string,
		tableInfo *model.TableInfo,
		snapshot uint64,
	) (
		jt *JSONTable,
		fallbackTbls []string,
		err error,
	)

	// DumpStatsToJSONBySnapshot dumps statistic to json.
	DumpStatsToJSONBySnapshot(dbName string, tableInfo *model.TableInfo, snapshot uint64, dumpPartitionStats bool) (*JSONTable, error)

	// LoadStatsFromJSON will load statistic from JSONTable, and save it to the storage.
	// In final, it will also udpate the stats cache.
	LoadStatsFromJSON(ctx context.Context, is infoschema.InfoSchema, jsonTbl *JSONTable, concurrencyForPartition uint8) error

	// LoadStatsFromJSONNoUpdate will load statistic from JSONTable, and save it to the storage.
	LoadStatsFromJSONNoUpdate(ctx context.Context, is infoschema.InfoSchema, jsonTbl *JSONTable, concurrencyForPartition uint8) error

	// Methods for extended stast.

	// InsertExtendedStats inserts a record into mysql.stats_extended and update version in mysql.stats_meta.
	InsertExtendedStats(statsName string, colIDs []int64, tp int, tableID int64, ifNotExists bool) (err error)

	// MarkExtendedStatsDeleted update the status of mysql.stats_extended to be `deleted` and the version of mysql.stats_meta.
	MarkExtendedStatsDeleted(statsName string, tableID int64, ifExists bool) (err error)

	// SaveExtendedStatsToStorage writes extended stats of a table into mysql.stats_extended.
	SaveExtendedStatsToStorage(tableID int64, extStats *statistics.ExtendedStatsColl, isLoad bool) (err error)
}

// StatsHandle is used to manage TiDB Statistics.
type StatsHandle interface {
	// GPool returns the goroutine pool.
	GPool() *gp.Pool

	// SPool returns the session pool.
	SPool() SessionPool

	// Lease returns the stats lease.
	Lease() time.Duration

	// SysProcTracker is used to track sys process like analyze
	SysProcTracker() sessionctx.SysProcTracker

	// AutoAnalyzeProcID generates an analyze ID.
	AutoAnalyzeProcID() uint64

	// GetTableStats retrieves the statistics table from cache, and the cache will be updated by a goroutine.
	GetTableStats(tblInfo *model.TableInfo) *statistics.Table

	// GetPartitionStats retrieves the partition stats from cache.
	GetPartitionStats(tblInfo *model.TableInfo, pid int64) *statistics.Table

	// GetCurrentPruneMode returns the current latest partitioning table prune mode.
	GetCurrentPruneMode() (mode string, err error)

	// TableInfoGetter is used to get table meta info.
	TableInfoGetter

	// StatsGC is used to do the GC job.
	StatsGC

	// StatsUsage is used to handle table delta and stats usage.
	StatsUsage

	// StatsHistory is used to manage historical stats.
	StatsHistory

	// StatsAnalyze is used to handle auto-analyze and manage analyze jobs.
	StatsAnalyze

	// StatsCache is used to manage all table statistics in memory.
	StatsCache

	// StatsLock is used to manage locked stats.
	StatsLock

	// StatsReadWriter is used to read and write stats to the storage.
	StatsReadWriter
}
