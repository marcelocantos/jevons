// Copyright 2026 The sqlpipe Authors
// SPDX-License-Identifier: Apache-2.0
#pragma once

#define SQLPIPE_VERSION       "0.31.0"
#define SQLPIPE_VERSION_MAJOR 0
#define SQLPIPE_VERSION_MINOR 31
#define SQLPIPE_VERSION_PATCH 0

// ── Bundled: sqldeep (query transpiler) ─────────────────────────
// Converts sqldeep extended SQL syntax to standard SQL.
// Integrated into Database::exec/query/subscribe automatically.

#define SQLDEEP_VERSION       "0.23.0"
#define SQLDEEP_VERSION_MAJOR 0
#define SQLDEEP_VERSION_MINOR 23
#define SQLDEEP_VERSION_PATCH 0

typedef struct sqlite3 sqlite3;
typedef struct pg_conn PGconn;

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    const char* from_column;
    const char* to_column;
} sqldeep_column_pair;

typedef struct {
    const char* from_table;
    const char* to_table;
    const sqldeep_column_pair* columns;
    int column_count;
} sqldeep_foreign_key;

typedef enum {
    SQLDEEP_SQLITE = 0,
    SQLDEEP_POSTGRES = 1,
    SQLDEEP_SQLITE_VANILLA = 2,
} sqldeep_backend;

/// Transpile sqldeep syntax to standard SQL (SQLite backend).
/// Returns malloc'd string (caller frees with sqldeep_free).
/// On error returns NULL, sets err_msg/err_line/err_col.
char* sqldeep_transpile(const char* input,
                        char** err_msg, int* err_line, int* err_col);

/// Transpile with explicit foreign key relationships.
char* sqldeep_transpile_fk(const char* input,
                           const sqldeep_foreign_key* fks, int fk_count,
                           char** err_msg, int* err_line, int* err_col);

/// Transpile for a specific backend (SQLite or Postgres).
char* sqldeep_transpile_backend(const char* input, sqldeep_backend backend,
                                char** err_msg, int* err_line, int* err_col);

/// Transpile with FKs for a specific backend.
char* sqldeep_transpile_fk_backend(const char* input, sqldeep_backend backend,
                                   const sqldeep_foreign_key* fks, int fk_count,
                                   char** err_msg, int* err_line, int* err_col);

const char* sqldeep_version(void);
void sqldeep_free(void* ptr);

/// Register sqldeep SQLite runtime functions (xml_element, xml_attrs, xml_agg,
/// sqldeep_json, sqldeep_json_object, sqldeep_json_array, sqldeep_json_group_array).
/// Called automatically by Database constructor. Returns SQLITE_OK on success.
int sqldeep_register_sqlite(sqlite3* db);

#ifdef __cplusplus
}
#endif

// ── Bundled: sqlift (schema migration) ──────────────────────────
// Declarative SQLite schema diffing and migration.
// Auto-synced from vendor/include/sqlift.h by scripts/bundle-deps.sh —
// do not edit this block by hand.

#define SQLIFT_VERSION "0.18.0"
#define SQLIFT_VERSION_MAJOR 0
#define SQLIFT_VERSION_MINOR 18
#define SQLIFT_VERSION_PATCH 0

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

enum sqlift_error_type {
    SQLIFT_OK               = 0,
    SQLIFT_ERROR            = 1,
    SQLIFT_PARSE_ERROR      = 2,
    SQLIFT_EXTRACT_ERROR    = 3,
    SQLIFT_DIFF_ERROR       = 4,
    SQLIFT_APPLY_ERROR      = 5,
    SQLIFT_DRIFT_ERROR      = 6,
    SQLIFT_DESTRUCTIVE_ERROR = 7,
    SQLIFT_BREAKING_CHANGE_ERROR = 8,
    SQLIFT_JSON_ERROR       = 9,
    SQLIFT_REBUILD_ERROR    = 10,
};

// Atomic permission bits for sqlift_apply_options.allow.
#define SQLIFT_ALLOW_REBUILD     (1u << 0)
#define SQLIFT_ALLOW_DESTRUCTIVE (1u << 1)
#define SQLIFT_ALLOW_LOOSEN      (1u << 2)
#define SQLIFT_ALLOW_DATA_DEPENDENT (1u << 3)
#define SQLIFT_ALLOW_NONE        0u
#define SQLIFT_ALLOW_ALL            (SQLIFT_ALLOW_REBUILD | SQLIFT_ALLOW_DESTRUCTIVE | SQLIFT_ALLOW_LOOSEN | SQLIFT_ALLOW_DATA_DEPENDENT)

typedef struct sqlift_apply_options {
    // Bitmask of SQLIFT_ALLOW_* flags. 0 = deny everything (strictest).
    unsigned int allow;
} sqlift_apply_options;

typedef struct sqlift_db sqlift_db;

enum sqlift_engine {
    SQLIFT_ENGINE_SQLITE   = 0,
    SQLIFT_ENGINE_POSTGRES = 1,
};
typedef struct sqlift_exec {
    void* ctx;
    int (*exec)(void* ctx, const char* sql, char** err_msg);
    int (*query)(void* ctx, const char* sql,
                 void (*row)(void* row_ctx, int ncols, const char* const* cols),
                 void* row_ctx, char** err_msg);
} sqlift_exec;
sqlift_db* sqlift_db_open(const char* path, int flags,
                          int* err_type, char** err_msg);
typedef struct sqlite3 sqlite3;
sqlift_db* sqlift_db_wrap_sqlite(sqlite3* handle,
                                 int* err_type, char** err_msg);
sqlift_db* sqlift_db_wrap_exec(int engine, sqlift_exec exec,
                               int* err_type, char** err_msg);
char* sqlift_parse_on(sqlift_db* db, const char* ddl,
                      int* err_type, char** err_msg);
void sqlift_db_close(sqlift_db* db);
int sqlift_db_exec(sqlift_db* db, const char* sql, char** err_msg);
char* sqlift_parse(const char* ddl, int* err_type, char** err_msg);
char* sqlift_extract(sqlift_db* db, int* err_type, char** err_msg);
char* sqlift_diff(const char* current_json, const char* desired_json,
                  int* err_type, char** err_msg);
int sqlift_apply(sqlift_db* db, const char* plan_json,
                 const sqlift_apply_options opts,
                 int* err_type, char** err_msg);
int64_t sqlift_migration_version(sqlift_db* db, int* err_type, char** err_msg);
char* sqlift_detect_redundant_indexes(const char* schema_json,
                                      int* err_type, char** err_msg);
char* sqlift_schema_hash(const char* schema_json,
                         int* err_type, char** err_msg);
int sqlift_db_query_int64(sqlift_db* db, const char* sql,
                          int64_t* result, char** err_msg);
char* sqlift_db_query_text(sqlift_db* db, const char* sql, char** err_msg);
void sqlift_free(void* ptr);
// sqlpipe shim: wrap an existing sqlite3* (not owned). Implemented in the
// bundled sqlift section of dist/sqlpipe.cpp by scripts/bundle-deps.sh.
sqlift_db* sqlift_db_wrap(sqlite3* handle);

#ifdef __cplusplus
}
#endif

// ── sqlpipe ────────────────────────────────────────────────────

#include <cstdint>
#include <cstdlib>
#include <functional>
#include <memory>
#include <new>
#include <optional>
#include <set>
#include <span>
#include <sqlite3.h>
#include <stdexcept>
#include <string>
#include <variant>
#include <vector>

// ── types.h ─────────────────────────────────────────────────────
namespace sqlpipe {

namespace detail {
/// std::malloc that throws std::bad_alloc on failure. FFI shims allocate the
/// buffers they hand back to managed runtimes through this so an allocation
/// failure surfaces as a clean error via their try/catch, instead of
/// dereferencing a null pointer in a subsequent memcpy (F9). Header-inline so
/// the C-API and Wasm shims (separate translation units) share one definition.
inline void* checked_malloc(std::size_t n) {
    void* p = std::malloc(n);
    if (!p) throw std::bad_alloc{};
    return p;
}
}  // namespace detail

/// Monotonically increasing sequence number for changesets.
using Seq = std::int64_t;

/// Structural schema fingerprint used to gate replication compatibility.
///
/// Opaque, variable-length bytes: a leading algorithm-id byte followed by the
/// raw digest — `[algo_id][digest...]`. Comparison is byte-wise equality. The
/// representation is deliberately algorithm-agnostic: changing the hash
/// algorithm or digest width later changes only the algo id and digest bytes
/// and needs no further wire or protocol change (mismatched fingerprints simply
/// compare unequal, which fails the compatibility gate cleanly).
using SchemaVersion = std::vector<std::uint8_t>;

/// Algorithm ids for the leading byte of a SchemaVersion fingerprint.
enum class SchemaFingerprintAlgo : std::uint8_t {
    SqliftSha256   = 0x01,  ///< sqlift structural hash, SHA-256 digest (32 bytes).
    SqliftPgSha256 = 0x02,  ///< sqlift Postgres IR hash, SHA-256 digest (32 bytes).
};

enum class Engine : std::uint8_t {
    Sqlite   = 0,
    Postgres = 1,
};

/// A byte buffer holding a raw SQLite changeset blob.
using Changeset = std::vector<std::uint8_t>;

/// Column value. Uses std::variant to represent SQLite types.
using Value = std::variant<
    std::monostate,            // NULL
    std::int64_t,              // INTEGER
    double,                    // REAL
    std::string,               // TEXT
    std::vector<std::uint8_t>  // BLOB
>;

/// The type of row operation.
enum class OpType : std::uint8_t {
    Insert = 1,
    Update = 2,
    Delete = 3,
};

/// A single row-level change extracted from a changeset.
struct ChangeEvent {
    std::string        table;
    OpType             op;
    std::vector<bool>  pk_flags;    // true for PK columns
    std::vector<Value> old_values;  // populated for UPDATE, DELETE
    std::vector<Value> new_values;  // populated for INSERT, UPDATE
};

/// Conflict resolution action returned by ConflictCallback.
enum class ConflictAction : std::uint8_t {
    Omit,      ///< Skip this change; the conflicting row is left as-is.
    Replace,   ///< Overwrite the conflicting row with the incoming change.
    Abort,     ///< Abort the entire changeset application.
};

/// Conflict type reported during changeset application.
enum class ConflictType : std::uint8_t {
    Data,        ///< A directly conflicting change (different values for same row).
    NotFound,    ///< The row to update/delete was not found on the replica.
    Conflict,    ///< A row with the same PK already exists (on INSERT).
    Constraint,  ///< A constraint (UNIQUE, NOT NULL, CHECK) would be violated.
    ForeignKey,  ///< A foreign key constraint would be violated.
};

/// Callback for conflict resolution on the replica side.
using ConflictCallback = std::function<ConflictAction(
    ConflictType type, const ChangeEvent& event)>;

/// Default bucket size for the diff protocol (rows per bucket).
inline constexpr std::int64_t kDefaultBucketSize = 1024;

/// Maximum serialized message size accepted by deserialize() (64 MB).
inline constexpr std::size_t kMaxMessageSize = 64 * 1024 * 1024;

/// Maximum number of elements in a deserialized array (10 M).
inline constexpr std::uint32_t kMaxArrayCount = 10'000'000;

// ── Delivery hint ──────────────────────────────────────────────────

// ── Delivery hint ──────────────────────────────────────────────────

/// Transport delivery hint for outgoing messages.
/// Reliable messages should be sent via an ordered, guaranteed channel
/// (e.g. QUIC stream). BestEffort messages may be sent via an
/// unreliable channel (e.g. QUIC datagram); if lost, the convergence
/// loop will regenerate them.
enum class Delivery : std::uint8_t {
    Reliable,    ///< Must arrive (changesets, errors, acks, hellos, patchsets).
    BestEffort,  ///< Regenerable by convergence (bucket/row hashes, need-buckets).
};

/// Opaque handle for a query subscription.
using SubscriptionId = std::uint64_t;

/// Full result set of a subscribed query.
struct QueryResult {
    SubscriptionId                  id;
    std::vector<std::string>        columns;  ///< Column names.
    std::vector<std::vector<Value>> rows;     ///< Result rows.
};

// ── Diff sync progress ──────────────────────────────────────────

/// Phase of the diff sync protocol for progress reporting.
enum class DiffPhase : std::uint8_t {
    ComputingBuckets,   ///< Computing bucket hashes for tables.
    ComparingBuckets,   ///< Comparing bucket hashes (master only).
    ComputingRowHashes, ///< Computing per-row hashes for mismatched buckets.
    BuildingPatchset,   ///< Building INSERT patchset (master only).
    ApplyingPatchset,   ///< Applying patchset + deletes (replica only).
};

/// Progress information emitted during diff sync.
struct DiffProgress {
    DiffPhase    phase;
    std::string  table;           ///< Current table (empty if N/A).
    std::int64_t items_done = 0;  ///< Buckets/rows/tables processed so far.
    std::int64_t items_total = 0; ///< Total items (0 if unknown).
};

/// Callback for diff sync progress reporting.
using ProgressCallback = std::function<void(const DiffProgress&)>;

// ── Logging ────────────────────────────────────────────────────────

/// Log severity level.
enum class LogLevel : uint8_t { Debug = 0, Info, Warn, Error };

/// Callback for library log output. If not set, logs are silently discarded.
using LogCallback = std::function<void(LogLevel level, std::string_view message)>;


/// Called when a schema mismatch is detected during handshake.
/// Receives (remote_fingerprint, local_fingerprint, remote_schema_sql).
/// remote_schema_sql contains the remote side's sorted CREATE TABLE statements
/// (semicolon-separated). On the master side, this is empty because the replica
/// only sends a fingerprint in HelloMsg, not its full schema.
/// The callback may ALTER the local database to resolve the mismatch.
/// Return true to recompute fingerprints and retry; false to proceed
/// with the default behaviour (ErrorMsg on master, Error state on replica).
using SchemaMismatchCallback = std::function<bool(
    const SchemaVersion& remote_sv, const SchemaVersion& local_sv,
    const std::string& remote_schema_sql)>;

/// Execute a one-shot SQL query and return the result set.
/// The returned QueryResult has id = 0 (not a subscription).
/// Throws Error on SQL errors.
QueryResult query(sqlite3* db, const std::string& sql);

/// Callback for subscription results.
using SubscriptionCallback = std::function<void(const QueryResult&)>;

} // namespace sqlpipe

// ── database.h ──────────────────────────────────────────────────
namespace sqlpipe {

class Database;

/// RAII handle for a query subscription. Auto-unsubscribes on destruction.
class Subscription {
public:
    ~Subscription();
    Subscription(Subscription&&) noexcept;
    Subscription& operator=(Subscription&&) noexcept;
    Subscription(const Subscription&) = delete;
    Subscription& operator=(const Subscription&) = delete;

private:
    friend class Database;
    struct Impl;
    std::unique_ptr<Impl> impl_;
    Subscription(std::unique_ptr<Impl> impl);
};

/// Unified database handle with schema migration and query subscriptions.
///
/// Opens a SQLite database, optionally applying schema migrations via
/// sqlift, and provides exec/query/subscribe operations. Subscriptions
/// fire automatically after exec() when data changes.
///
/// Owns the sqlite3* handle. Use handle() to pass it to Master, Replica,
/// or Peer for replication. After replication operations that modify the
/// database, call notify() to fire subscriptions.
class Database {
public:
    /// Open or create a database at path. Use ":memory:" for in-memory.
    /// If schema_ddl is non-empty, applies schema migration using sqlift
    /// (creates tables if new, alters if existing schema differs).
    explicit Database(const std::string& path,
                      const std::string& schema_ddl = {});
#ifndef EMSCRIPTEN
    static Database postgres(const std::string& conninfo,
                             const std::string& schema_ddl = {});
#endif
    ~Database();

    Database(Database&&) noexcept;
    Database& operator=(Database&&) noexcept;
    Database(const Database&) = delete;
    Database& operator=(const Database&) = delete;

    /// Execute DDL/DML SQL. Subscriptions fire if data changes.
    void exec(const std::string& sql);

    /// Execute a query and return the full result set.
    QueryResult query(const std::string& sql) const;

    /// Subscribe to a query. The callback fires whenever the query's
    /// result set changes — from local exec(), replication, or any
    /// other source of writes to the underlying database.
    /// The callback fires once immediately with the initial result.
    Subscription subscribe(const std::string& sql, SubscriptionCallback cb);

    /// Fire subscriptions for changes to the given tables. Called
    /// automatically by exec(); call manually after Master/Replica/Peer
    /// operations that modify the database.
    void notify(const std::set<std::string>& affected_tables);

    /// Fire subscriptions by scanning all tracked tables.
    /// Convenience overload when you don't know which tables changed.
    void notify();

    /// Active subscription registrations. Zero after each Subscription
    /// is destroyed or the database is closed.
    std::size_t subscription_count() const;

    /// Access the underlying sqlite3 handle for use with
    /// Master/Replica/Peer constructors.
    sqlite3* handle() const;

    /// Engine this Database was opened against.
    Engine engine() const;

    /// SQLite handle, or nullptr when engine() is Postgres.
    sqlite3* sqlite_handle() const;

#ifndef EMSCRIPTEN
    /// Postgres handle, or nullptr when engine() is Sqlite.
    PGconn* postgres_handle() const;
#endif

    /// Generate schema migration SQL from old DDL to new DDL using sqlift.
    /// Returns empty string if schemas are equivalent.
    static std::string migration(const std::string& from_ddl,
                                 const std::string& to_ddl);

private:
    friend class Subscription;
    struct Impl;
    std::shared_ptr<Impl> impl_;
    Database() = default;
};

} // namespace sqlpipe

// ── query_watch.h ───────────────────────────────────────────────
namespace sqlpipe {

/// Standalone query subscription engine for a single sqlite3* handle.
///
/// Monitors registered SQL queries and detects when their results change.
/// Result-change detection uses FNV-1a hashing — queries are only reported
/// as changed when the actual result set differs, not on every call to
/// notify().
///
/// Usage:
///   QueryWatch w(db);
///   auto result = w.subscribe("SELECT * FROM items WHERE active = 1");
///   // ... perform writes ...
///   auto changed = w.notify({"items"});  // re-evaluates affected queries
///
/// Delivery is synchronous backpressure. `notify` evaluates and returns
/// before the caller continues, and the watch keeps no queue of pending
/// payloads (structural bound: zero buffered results). A slow consumer
/// stalls the caller; it does not accumulate results. Unchanged hashes
/// are omitted. Latest-state delivery may therefore skip intermediate
/// results when the caller applies a burst and notifies once
/// (`Replica::handle_messages` does that). This is not exactly-once
/// event delivery.
///
/// Evaluation failure throws `ErrorCode::QueryFailed`, publishes nothing,
/// and does not advance the last successful result hash. The next
/// `notify` retries. An empty vector means "unchanged", never "failed".
///
/// Does NOT own the sqlite3* handle.
class QueryWatch {
public:
    explicit QueryWatch(sqlite3* db);
#ifndef EMSCRIPTEN
    explicit QueryWatch(PGconn* conn);
#endif
    ~QueryWatch();

    QueryWatch(const QueryWatch&) = delete;
    QueryWatch& operator=(const QueryWatch&) = delete;
    QueryWatch(QueryWatch&&) noexcept;
    QueryWatch& operator=(QueryWatch&&) noexcept;

    /// Register a query subscription. Returns the subscription ID.
    /// Results arrive via notify() — subscribe does not evaluate the query.
    SubscriptionId subscribe(const std::string& sql);

    /// Remove a subscription.
    void unsubscribe(SubscriptionId id);

    /// Re-evaluate subscriptions that depend on any of the given tables.
    /// Returns results only for subscriptions whose output actually changed.
    std::vector<QueryResult> notify(const std::set<std::string>& affected_tables);

    /// Re-evaluate subscriptions with predicate-aware filtering.
    /// Subscriptions with extractable predicates skip re-evaluation when
    /// the changeset doesn't contain matching rows.
    std::vector<QueryResult> notify(const std::set<std::string>& affected_tables,
                                    const Changeset& changeset_data);

    /// Returns true if there are no active subscriptions.
    bool empty() const;

    /// Active registrations. Does not grow with producer writes.
    std::size_t subscription_count() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace sqlpipe

// ── error.h ─────────────────────────────────────────────────────
namespace sqlpipe {

/// Error codes returned by sqlpipe operations.
enum class ErrorCode : int {
    Ok = 0,
    SqliteError,        ///< An underlying SQLite call failed.
    ProtocolError,      ///< Malformed or unexpected message.
    SchemaMismatch,     ///< Master and replica schemas differ.
    InvalidState,       ///< Operation not valid in the current state.
    OwnershipRejected,  ///< Peer ownership request was rejected by the server.
    WithoutRowidTable,  ///< Table uses WITHOUT ROWID (not supported).
    PostgresError,      ///< A libpq / Postgres call failed.
    UnsupportedTable,   ///< PK or column type rejected by the engine gate.
    QueryFailed,        ///< Subscription SQL failed to prepare or evaluate.
                        ///< Local only: not a wire ErrorMsg. Distinct from a
                        ///< healthy notify that returns no rows.
};

/// Exception thrown by sqlpipe operations.
class Error : public std::runtime_error {
public:
    Error(ErrorCode code, const std::string& msg)
        : std::runtime_error(msg), code_(code) {}

    ErrorCode code() const noexcept { return code_; }

private:
    ErrorCode code_;
};

} // namespace sqlpipe

// ── protocol.h ──────────────────────────────────────────────────
namespace sqlpipe {

inline constexpr std::uint32_t kProtocolVersion         = 7;
inline constexpr std::uint32_t kPostgresProtocolVersion = 8;

// ── Message types ───────────────────────────────────────────────────

/// Sent by both sides during handshake to exchange schema state.
struct HelloMsg {
    std::uint32_t protocol_version;  ///< Must match kProtocolVersion.
    SchemaVersion schema_version;    ///< Sender's schema fingerprint.
    std::set<std::string> owned_tables;  ///< Tables the sender wants to own (Peer mode).
    Seq last_seq = -1;  ///< Sender's current seq (-1 = not provided).
                        ///< When replica's seq matches master's and both > 0,
                        ///< diff sync is skipped (fast reconnect).
};

/// A single changeset (one flush() worth of changes). Used in live streaming.
struct ChangesetMsg {
    Seq       seq;   ///< Sequence number assigned by the master.
    Changeset data;  ///< Raw SQLite changeset blob.
};

/// Acknowledgement sent by the replica after applying a changeset.
struct AckMsg {
    Seq seq;  ///< The sequence number that was applied.
};

/// Protocol-level error. Receiving side should transition to Error state.
struct ErrorMsg {
    ErrorCode     code{};                      ///< Machine-readable error category.
    std::string   detail;                      ///< Human-readable description.
    SchemaVersion remote_schema_version;       ///< Remote fingerprint (SchemaMismatch only; empty otherwise).
    std::string   remote_schema_sql;           ///< Remote CREATE TABLE SQL (SchemaMismatch only).

    ErrorMsg() = default;
    ErrorMsg(ErrorCode c, std::string d,
             SchemaVersion rsv = {}, std::string rsql = {})
        : code(c), detail(std::move(d)),
          remote_schema_version(rsv), remote_schema_sql(std::move(rsql)) {}
};

// ── Diff protocol messages ──────────────────────────────────────────

/// One bucket's hash in BucketHashesMsg.
struct BucketHashEntry {
    std::string   table;
    std::int64_t  bucket_lo;     ///< Inclusive rowid lower bound.
    std::int64_t  bucket_hi;     ///< Inclusive rowid upper bound.
    std::uint64_t hash;          ///< XOR of fnv1a(rowid||row_hash) per row.
    std::int64_t  row_count;     ///< Number of rows in this bucket.
};

/// Sent by replica after receiving master's HelloMsg.
/// Contains per-table bucket hashes for the diff protocol.
struct BucketHashesMsg {
    std::vector<BucketHashEntry> buckets;
    Seq                last_seq = -1;           ///< Sender's last applied seq (-1 = unknown).
    std::uint32_t      protocol_version = 0;    ///< Sender's protocol version (0 = legacy).
    SchemaVersion      schema_version;          ///< Sender's schema fingerprint (empty = legacy).
};

/// One bucket range the master needs row-level detail for.
struct NeedBucketRange {
    std::string  table;
    std::int64_t lo;
    std::int64_t hi;
};

/// Sent by master after comparing bucket hashes.
/// Lists the buckets that differ and need row-level detail.
struct NeedBucketsMsg {
    std::vector<NeedBucketRange> ranges;
};

/// A contiguous run of rowids with their hashes.
struct RowHashRun {
    std::int64_t               start_rowid;  ///< First rowid in the run.
    std::int64_t               count;        ///< Number of contiguous rowids.
    std::vector<std::uint64_t> hashes;       ///< One hash per rowid.
};

/// Row hashes for one bucket in RowHashesMsg.
struct RowHashesEntry {
    std::string              table;
    std::int64_t             lo;     ///< Bucket rowid lower bound.
    std::int64_t             hi;     ///< Bucket rowid upper bound.
    std::vector<RowHashRun>  runs;   ///< Run-length encoded row hashes.
};

/// Sent by replica with per-row hashes for requested buckets.
struct RowHashesMsg {
    std::vector<RowHashesEntry> entries;
};

/// Per-table list of rowids to delete.
struct TableDeletes {
    std::string               table;
    std::vector<std::int64_t> rowids;
};

/// Sent by master with the computed diff. Carries an INSERT patchset for
/// rows to add/update and per-table rowid lists for rows to delete.
struct DiffReadyMsg {
    Seq                        seq;       ///< Master's current seq.
    Changeset                  patchset;  ///< INSERT records for insert+update.
    std::vector<TableDeletes>  deletes;   ///< Rowids to delete per table.
};

using Message = std::variant<
    HelloMsg,
    ChangesetMsg,
    AckMsg,
    ErrorMsg,
    BucketHashesMsg,
    NeedBucketsMsg,
    RowHashesMsg,
    DiffReadyMsg
>;

// ── OutMessage ──────────────────────────────────────────────────────

/// A message paired with a transport delivery hint.
struct OutMessage {
    Message  msg;
    Delivery delivery;
};

/// Determine the delivery mode for a message based on its type.
/// BucketHashesMsg, NeedBucketsMsg, RowHashesMsg → BestEffort;
/// everything else → Reliable.
inline Delivery delivery_for(const Message& msg) {
    return std::visit([](const auto& m) -> Delivery {
        using T = std::decay_t<decltype(m)>;
        if constexpr (std::is_same_v<T, BucketHashesMsg> ||
                      std::is_same_v<T, NeedBucketsMsg> ||
                      std::is_same_v<T, RowHashesMsg>) {
            return Delivery::BestEffort;
        }
        return Delivery::Reliable;
    }, msg);
}

/// Wrap a Message with its default delivery hint.
inline OutMessage tagged(Message msg) {
    auto d = delivery_for(msg);
    return {std::move(msg), d};
}

/// Wrap a vector of Messages with their default delivery hints.
inline std::vector<OutMessage> tagged(std::vector<Message> msgs) {
    std::vector<OutMessage> result;
    result.reserve(msgs.size());
    for (auto& m : msgs) {
        result.push_back(tagged(std::move(m)));
    }
    return result;
}

// ── Wire format tags ────────────────────────────────────────────────

enum class MessageTag : std::uint8_t {
    Hello        = 0x01,
    Changeset    = 0x03,
    Ack          = 0x08,
    Error        = 0x09,
    BucketHashes = 0x0A,
    NeedBuckets  = 0x0B,
    RowHashes    = 0x0C,
    DiffReady    = 0x0D,
};

// ── Serialization ───────────────────────────────────────────────────

/// Serialize a Message to a length-prefixed byte buffer.
/// Format: [4-byte total_length LE][1-byte tag][payload...]
std::vector<std::uint8_t> serialize(const Message& msg);

/// Deserialize a byte buffer (including the 4-byte length prefix) into a Message.
Message deserialize(std::span<const std::uint8_t> buf);

/// Callback invoked automatically when a write transaction commits on a
/// Master's database. Receives the tagged changeset messages ready to send.
/// When set, the Master hooks into SQLite's commit notification so that
/// callers never need to call flush() explicitly.
using FlushCallback = std::function<void(const std::vector<OutMessage>&)>;

} // namespace sqlpipe

// ── master.h ────────────────────────────────────────────────────
namespace sqlpipe {

struct MasterConfig {
    /// If set, only track these tables. nullopt = track all.
    /// An empty set means track nothing.
    std::optional<std::set<std::string>> table_filter;

    /// Meta-table key for storing the sequence number.
    std::string seq_key = "seq";

    /// Rows per bucket for the diff protocol.
    std::int64_t bucket_size = kDefaultBucketSize;

    /// Diff sync progress callback. nullptr = no reporting.
    ProgressCallback on_progress = nullptr;

    /// Called when a replica's schema fingerprint doesn't match.
    /// The callback may ALTER the local DB to resolve the mismatch,
    /// then return true to recompute and retry the comparison.
    /// nullptr or returning false = send ErrorMsg (default behaviour).
    SchemaMismatchCallback on_schema_mismatch = nullptr;

    /// Log callback. nullptr = discard all log output.
    LogCallback on_log = nullptr;

    /// Automatic flush callback. When set, the Master hooks into SQLite's
    /// commit notification and delivers tagged changeset messages automatically
    /// after each write transaction. No explicit flush() needed.
    /// nullptr = manual flush only (caller must call flush()).
    FlushCallback on_flush = nullptr;

    /// Maximum number of recent changesets to retain for queue-based
    /// replay on reconnect. When a replica reconnects with a seq that
    /// is behind but still within the queue, the master replays from
    /// the queue instead of running a full diff sync.
    /// 0 = no queue (always diff sync on reconnect). Default: 64.
    std::size_t changeset_queue_size = 64;
};

/// The sending side of the replication protocol.
///
/// Does NOT own the sqlite3* handle. Caller must keep it open for
/// the Master's lifetime.
class Master {
public:
    explicit Master(sqlite3* db, MasterConfig config = {});
#ifndef EMSCRIPTEN
    explicit Master(PGconn* conn, MasterConfig config = {});
#endif
    ~Master();

    Master(const Master&) = delete;
    Master& operator=(const Master&) = delete;
    Master(Master&&) noexcept;
    Master& operator=(Master&&) noexcept;

    /// Execute SQL on the master's database. If on_flush is set, any
    /// committed changes are automatically delivered via the callback.
    void exec(const std::string& sql);

    /// Call after committing a write transaction. Extracts the changeset,
    /// assigns a sequence number, and returns the tagged messages to send
    /// to connected replicas. Returns empty if nothing changed.
    std::vector<OutMessage> flush();

    /// Process an incoming message from a replica.
    std::vector<OutMessage> handle_message(const Message& msg);

    Seq current_seq() const;
    SchemaVersion schema_version() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace sqlpipe

// ── replica.h ───────────────────────────────────────────────────
namespace sqlpipe {

struct ReplicaConfig {
    /// Called when a conflict occurs during changeset application.
    /// Default (nullptr): ConflictAction::Abort.
    ConflictCallback on_conflict = nullptr;

    /// If set, only manage these tables during diff sync.
    /// nullopt = all tracked tables (default). Empty set = nothing.
    std::optional<std::set<std::string>> table_filter;

    /// Meta-table key for storing the sequence number.
    std::string seq_key = "seq";

    /// Rows per bucket for the diff protocol.
    std::int64_t bucket_size = kDefaultBucketSize;

    /// Diff sync progress callback. nullptr = no reporting.
    ProgressCallback on_progress = nullptr;

    /// Called when a SchemaMismatch error is received from the master.
    /// The callback may ALTER the local DB to match the master's schema,
    /// then return true to auto-reset to Init state (caller should retry
    /// the handshake). nullptr or returning false = transition to Error
    /// state (default behaviour).
    SchemaMismatchCallback on_schema_mismatch = nullptr;

    /// Log callback. nullptr = discard all log output.
    LogCallback on_log = nullptr;

    /// When true, messages received while a prediction is active (Drafting
    /// or Committed) are queued instead of applied, so inbound truth cannot
    /// clobber the local sandbox mid-gesture. Call `end_prediction()` (or
    /// `rollback_prediction()`) to roll back the savepoint and apply the
    /// queue in order. Default false preserves historical behaviour: a
    /// Committed prediction is auto-rolled back on the next handle_message.
    bool queue_while_predicting = false;
};

/// Protocol progress visible without reading `_sqlpipe_meta`.
/// `Live` means the handshake finished, not that the replica has every
/// committed row and not that the source data is fresh. Compare
/// `current_seq()` with the writer's sequence; remote progress stays
/// unknown (`observed_remote_seq() == nullopt`) until a Hello, DiffReady,
/// or applied changeset supplies one. The core does not time out a link.
/// The caller detects transport loss or a stall and calls `reset()`,
/// which returns the replica to `Unsynchronized`.
enum class SyncStatus : std::uint8_t {
    Unsynchronized,  ///< Init, or reset() after the caller reports link loss.
    Synchronizing,   ///< Handshake or diff in progress.
    Live,            ///< Handshake complete. Not a freshness certificate.
    Failed,          ///< Terminal protocol error (State::Error).
};

/// Return type for Replica::handle_message.
struct HandleResult {
    std::vector<OutMessage>   messages;       ///< Tagged protocol responses to send back.
    std::vector<ChangeEvent>  changes;        ///< Row-level changes applied this call.
    std::vector<QueryResult>  subscriptions;  ///< Invalidated subscription results.
};

/// The receiving side of the replication protocol.
///
/// Does NOT own the sqlite3* handle. Caller must keep it open for
/// the Replica's lifetime.
class Replica {
public:
    explicit Replica(sqlite3* db, ReplicaConfig config = {});
#ifndef EMSCRIPTEN
    explicit Replica(PGconn* conn, ReplicaConfig config = {});
#endif
    ~Replica();

    Replica(const Replica&) = delete;
    Replica& operator=(const Replica&) = delete;
    Replica(Replica&&) noexcept;
    Replica& operator=(Replica&&) noexcept;

    /// Generate the initial HelloMsg to send to the master.
    OutMessage hello() const;

    /// Initiate a convergence round. Computes and returns bucket hashes
    /// for the current local state. The master compares these against its
    /// own state and responds with NeedBuckets/DiffReady.
    ///
    /// Can be called in any state — Init, Live, or after reset().
    /// Unlike hello(), does not require a handshake sequence. The master
    /// processes the BucketHashes directly without a prior HelloMsg.
    ///
    /// Use this for:
    /// - Periodic convergence checks (are we still in sync?)
    /// - Reconnection without full handshake
    /// - Loss-tolerant sync over unreliable channels
    std::vector<OutMessage> converge();

    /// Process an incoming message from the master.
    HandleResult handle_message(const Message& msg);

    /// Process multiple messages, deferring subscription evaluation until
    /// all are applied. More efficient than calling handle_message() in a
    /// loop when processing a burst of messages.
    HandleResult handle_messages(std::span<const Message> msgs);

    /// Subscribe to a SQL query. Returns the subscription ID.
    /// The initial result and subsequent updates arrive via
    /// HandleResult::subscriptions — subscribe does not evaluate the query.
    /// Before Live state, evaluation is deferred until sync completes.
    /// After Live, the subscription is evaluated on the next handle_message.
    SubscriptionId subscribe(const std::string& sql);

    /// Remove a subscription.
    void unsubscribe(SubscriptionId id);

    /// Begin an optimistic prediction. Creates a SQLite SAVEPOINT.
    /// Only one prediction can be active at a time.
    /// While active, the client can write optimistically to the local
    /// database and subscriptions will fire with the predicted state.
    void begin_prediction();

    /// Finalise the prediction — the client is done editing and will
    /// send the action to the server. The savepoint stays open until
    /// `end_prediction()` / `rollback_prediction()`, or (when
    /// `queue_while_predicting` is false) the next `handle_message`,
    /// which auto-rollbacks.
    void commit_prediction();

    /// End an active prediction: roll back the savepoint, then apply any
    /// messages queued under `queue_while_predicting`. Returns the combined
    /// HandleResult from applying the queue (empty if none were queued).
    /// Throws if no prediction is active.
    HandleResult end_prediction();

    /// Cancel the prediction (same savepoint rollback as `end_prediction`).
    /// Any queued inbound messages are still applied afterwards so truth
    /// catches up; their HandleResult is discarded. Prefer `end_prediction`
    /// when you need subscription/change results from the drain.
    void rollback_prediction();

    /// Number of inbound messages held while `queue_while_predicting` is
    /// set and a prediction is active. Zero when not queueing.
    std::size_t prediction_queue_size() const;

    /// Whether this replica was constructed with queue_while_predicting.
    bool queues_while_predicting() const;

    /// Reset to Init state for reconnection. Call this when the transport
    /// reports disconnect or a stalled link; sqlpipe does not detect that
    /// itself. Subscriptions are preserved; they re-evaluate after the next
    /// handshake applies changes. Also rolls back any active prediction and
    /// drops any queued inbound messages without applying them
    /// (reconnect/diff will resync). Clears observed remote progress.
    void reset();

    Seq current_seq() const;

    /// Last remote sequence observed from the master. `nullopt` until a
    /// Hello, DiffReady, or applied changeset reports one (unknown).
    std::optional<Seq> observed_remote_seq() const;

    SchemaVersion schema_version() const;

    /// Replica connection lifecycle state.
    enum class State : std::uint8_t {
        Init,        ///< Created but hello() not yet called.
        Handshake,   ///< hello() sent, awaiting master's response.
        DiffBuckets, ///< Sent bucket hashes, awaiting NeedBucketsMsg.
        DiffRows,    ///< Sent row hashes (or skipped), awaiting DiffReadyMsg.
        Live,        ///< Streaming; ready for real-time changesets.
        Error,       ///< A protocol or application error occurred.
    };

    State state() const;

    /// `state()` grouped for callers that should not treat Live as freshness.
    SyncStatus sync_status() const;

    /// Active query registrations on this replica. Unchanged by writes.
    std::size_t subscription_count() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace sqlpipe

// ── peer.h ──────────────────────────────────────────────────────
namespace sqlpipe {

/// Server-side callback to approve or reject a client's ownership request.
/// Receives the set of tables the client wants to own.
/// Returns true to approve, false to reject.
using ApproveOwnershipCallback = std::function<bool(
    const std::set<std::string>& requested_tables)>;

/// Explicit role for a Peer in the handshake protocol.
enum class PeerRole : std::uint8_t {
    Client,  ///< Initiates the handshake via start(). Sends owned_tables.
    Server,  ///< Waits for the client's hello. Validates ownership.
};

struct PeerConfig {
    /// Peer role — determines handshake behaviour.
    PeerRole role = PeerRole::Client;

    /// Glob patterns for tables this peer wants to own (client-side).
    /// Patterns are matched against tracked tables using SQLite GLOB
    /// semantics (* = any sequence, ? = any char, [...] = character class).
    /// Use "*" to own all user tables. Resolved at start() time.
    /// Ignored on the server side (computed as complement of client's).
    std::set<std::string> owned_tables;

    /// If set, only consider these tables for replication.
    /// nullopt = all user tables (default). owned_tables must be a subset.
    /// Complement (remote tables) is computed within this filtered set.
    std::optional<std::set<std::string>> table_filter;

    /// Server-side ownership validation callback (server role only).
    /// nullptr = auto-approve any request.
    ApproveOwnershipCallback approve_ownership = nullptr;

    /// Conflict callback for the internal Replica.
    ConflictCallback on_conflict = nullptr;

    /// Diff sync progress callback. nullptr = no reporting.
    /// Forwarded to both the internal Master and Replica.
    ProgressCallback on_progress = nullptr;

    /// Called when a schema mismatch is detected during handshake.
    /// Forwarded to both the internal Master and Replica.
    SchemaMismatchCallback on_schema_mismatch = nullptr;

    /// Log callback. nullptr = discard all log output.
    /// Forwarded to both the internal Master and Replica.
    LogCallback on_log = nullptr;
};

/// Identifies whether the sender was acting as master or replica.
enum class SenderRole : std::uint8_t {
    AsMaster  = 0,  ///< Sender is acting as master (replicating owned tables).
    AsReplica = 1,  ///< Sender is acting as replica (acks/hellos for other side).
};

/// A protocol message with a directional tag for peer-to-peer routing.
struct PeerMessage {
    SenderRole sender_role;
    Message    payload;
};

/// A peer message paired with a transport delivery hint.
struct PeerOutMessage {
    PeerMessage msg;
    Delivery    delivery;
};

/// Wrap a PeerMessage with its default delivery hint.
inline PeerOutMessage tagged(PeerMessage msg) {
    auto d = delivery_for(msg.payload);
    return {std::move(msg), d};
}

/// Wrap a vector of PeerMessages with their default delivery hints.
inline std::vector<PeerOutMessage> tagged(std::vector<PeerMessage> msgs) {
    std::vector<PeerOutMessage> result;
    result.reserve(msgs.size());
    for (auto& m : msgs) {
        result.push_back(tagged(std::move(m)));
    }
    return result;
}

/// Return type for Peer::handle_message.
struct PeerHandleResult {
    std::vector<PeerOutMessage> messages;       ///< Tagged protocol responses.
    std::vector<ChangeEvent>    changes;        ///< Row-level changes applied.
    std::vector<QueryResult>    subscriptions;  ///< Invalidated subscription results.
};

/// Bidirectional replication peer.
///
/// Wraps a Master (for owned tables) and a Replica (for the remote peer's
/// tables) behind a symmetric API.
///
/// Does NOT own the sqlite3* handle.
class Peer {
public:
    explicit Peer(sqlite3* db, PeerConfig config = {});
#ifndef EMSCRIPTEN
    explicit Peer(PGconn* conn, PeerConfig config = {});
#endif
    ~Peer();

    Peer(const Peer&) = delete;
    Peer& operator=(const Peer&) = delete;
    Peer(Peer&&) noexcept;
    Peer& operator=(Peer&&) noexcept;

    /// Initiate the handshake (client only).
    /// Returns tagged PeerMessages to send to the remote peer.
    std::vector<PeerOutMessage> start();

    /// Flush local changes on owned tables.
    /// Returns tagged PeerMessages to send to the remote peer.
    std::vector<PeerOutMessage> flush();

    /// Process an incoming PeerMessage from the remote peer.
    PeerHandleResult handle_message(const PeerMessage& msg);

    /// Subscribe to a SQL query on the replica side. Returns the
    /// subscription ID. Results arrive via PeerHandleResult::subscriptions.
    SubscriptionId subscribe(const std::string& sql);

    /// Remove a subscription.
    void unsubscribe(SubscriptionId id);

    /// Reset to Init for reconnection. Call start() again to re-handshake.
    /// Table ownership is preserved. Subscriptions are not: they belong to
    /// the internal Replica, which this destroys. Subscribe again after
    /// start(); there is no subscription cache to repair by polling.
    void reset();

    /// Peer lifecycle state.
    enum class State : std::uint8_t {
        Init,        ///< Created, not yet started.
        Negotiating, ///< Ownership negotiation in progress.
        Diffing,     ///< Diff sync in progress.
        Live,        ///< Both directions are live.
        Error,       ///< A protocol or application error occurred.
    };

    State state() const;

    /// `state()` grouped the same way as `Replica::sync_status`.
    SyncStatus sync_status() const;

    /// Registrations on the internal replica. Zero after `reset()` until
    /// the caller subscribes again.
    std::size_t subscription_count() const;

    /// The tables this peer owns (mastering).
    const std::set<std::string>& owned_tables() const;

    /// The tables the remote peer owns (replicating).
    const std::set<std::string>& remote_tables() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

/// Serialize a PeerMessage to a length-prefixed byte buffer.
/// Format: [4B LE length][1B sender_role][1B tag][payload...]
std::vector<std::uint8_t> serialize(const PeerMessage& msg);

/// Deserialize a byte buffer into a PeerMessage.
PeerMessage deserialize_peer(std::span<const std::uint8_t> buf);

// ── relay.h ─────────────────────────────────────────────────────

/// Callback to deliver a tagged message to a downstream sink.
using SinkCallback = std::function<void(const OutMessage&)>;

/// Configuration for a Relay node.
struct RelayConfig {
    /// Optional table filter (same semantics as MasterConfig/ReplicaConfig).
    std::optional<std::set<std::string>> table_filter;

    /// Conflict callback for the internal Replica.
    ConflictCallback on_conflict = nullptr;

    /// Schema mismatch callback for the internal Replica.
    SchemaMismatchCallback on_schema_mismatch = nullptr;

    /// Log callback forwarded to both internal Master and Replica.
    LogCallback on_log = nullptr;
};

/// A relay node that receives changesets from an upstream Master and
/// broadcasts them to downstream Replica sinks.
///
/// Internally owns a Replica (upstream) and a Master (downstream) on
/// the same sqlite3* handle. The SQLite session extension captures
/// changeset_apply writes, so the Master's session sees the Replica's
/// applied changes and can flush them downstream.
///
/// Does NOT own the sqlite3* handle.
class Relay {
public:
    explicit Relay(sqlite3* db, RelayConfig config = {});
    ~Relay();

    Relay(const Relay&) = delete;
    Relay& operator=(const Relay&) = delete;
    Relay(Relay&&) noexcept;
    Relay& operator=(Relay&&) noexcept;

    /// Register a downstream sink. Returns an ID for removal.
    std::size_t add_sink(SinkCallback cb);

    /// Remove a downstream sink by ID.
    void remove_sink(std::size_t id);

    /// Generate the HelloMsg to send to the upstream master.
    OutMessage hello();

    /// Handle an incoming message from the upstream master.
    /// Applies the changeset locally, flushes to the internal Master,
    /// and broadcasts to all registered sinks.
    /// Returns tagged response messages to send back to the upstream master.
    std::vector<OutMessage> handle_upstream(const Message& msg);

    /// Handle an incoming message from a downstream sink (handshake).
    /// Returns tagged response messages to send back to that sink.
    std::vector<OutMessage> handle_downstream(const Message& msg);

    /// Subscribe to a query (on the replica side).
    SubscriptionId subscribe(const std::string& sql);

    /// Remove a subscription.
    void unsubscribe(SubscriptionId id);

    /// Reset for reconnection to upstream.
    void reset();

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

// ── Schema utilities ────────────────────────────────────────────

/// Generate migration DDL from old_ddl to new_ddl using sqlift.
/// Returns the SQL statements to transform the schema, or empty string
/// if schemas are equivalent. Throws Error on parse/diff failures.
std::string generate_migration(const std::string& old_ddl,
                               const std::string& new_ddl);

// ── Convenience utilities ───────────────────────────────────────

/// Drive the Master/Replica handshake protocol to completion when both
/// are in the same process. Exchanges messages until no more remain.
void sync_handshake(Master& master, Replica& replica);

/// Drive the Peer handshake protocol to completion when both peers are
/// in the same process. The client initiates; messages are exchanged
/// until both peers reach Live state or no more messages remain.
void sync_handshake(Peer& client, Peer& server);

/// Drive the Master/Relay handshake (upstream side).
void sync_handshake(Master& master, Relay& relay);

} // namespace sqlpipe
