// Copyright 2026 The sqlpipe Authors
// SPDX-License-Identifier: Apache-2.0
#include "sqlpipe.h"

#include <algorithm>
#include <cassert>
#include <cstdlib>
#include <cstring>
#include <deque>
#include <map>
#include <format>
#include <lz4.h>
#include <unordered_map>
#include <unordered_set>

// ── Logging macro ───────────────────────────────────────────────
#define SQLPIPE_LOG(cb, level, ...) \
    do { if (cb) (cb)(level, std::format(__VA_ARGS__)); } while(0)

// ── sqlite_util.h ───────────────────────────────────────────────
namespace sqlpipe::detail {

/// RAII wrapper for sqlite3_stmt*.
class StmtGuard {
public:
    StmtGuard() = default;
    explicit StmtGuard(sqlite3_stmt* s) : stmt_(s) {}
    ~StmtGuard() { if (stmt_) sqlite3_finalize(stmt_); }

    StmtGuard(const StmtGuard&) = delete;
    StmtGuard& operator=(const StmtGuard&) = delete;
    StmtGuard(StmtGuard&& o) noexcept : stmt_(o.stmt_) { o.stmt_ = nullptr; }
    StmtGuard& operator=(StmtGuard&& o) noexcept {
        if (this != &o) {
            if (stmt_) sqlite3_finalize(stmt_);
            stmt_ = o.stmt_;
            o.stmt_ = nullptr;
        }
        return *this;
    }

    sqlite3_stmt* get() const { return stmt_; }

    sqlite3_stmt* release() {
        auto* s = stmt_;
        stmt_ = nullptr;
        return s;
    }

private:
    sqlite3_stmt* stmt_ = nullptr;
};

/// Execute SQL or throw.
inline void exec(sqlite3* db, const char* sql) {
    char* err = nullptr;
    int rc = sqlite3_exec(db, sql, nullptr, nullptr, &err);
    if (rc != SQLITE_OK) {
        std::string msg = err ? err : "unknown error";
        sqlite3_free(err);
        throw Error(ErrorCode::SqliteError, msg);
    }
}

/// Prepare a statement or throw.
inline StmtGuard prepare(sqlite3* db, const char* sql) {
    sqlite3_stmt* stmt = nullptr;
    int rc = sqlite3_prepare_v2(db, sql, -1, &stmt, nullptr);
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError, sqlite3_errmsg(db));
    }
    return StmtGuard(stmt);
}

/// Step a statement expecting SQLITE_DONE, or throw.
inline void step_done(sqlite3* db, sqlite3_stmt* stmt) {
    int rc = sqlite3_step(stmt);
    if (rc != SQLITE_DONE) {
        throw Error(ErrorCode::SqliteError, sqlite3_errmsg(db));
    }
}

} // namespace sqlpipe::detail

// ── session_guard.h ─────────────────────────────────────────────
namespace sqlpipe::detail {

/// RAII wrapper for sqlite3_session*.
class SessionGuard {
public:
    SessionGuard() = default;
    explicit SessionGuard(sqlite3_session* s) : session_(s) {}
    ~SessionGuard() { if (session_) sqlite3session_delete(session_); }

    SessionGuard(const SessionGuard&) = delete;
    SessionGuard& operator=(const SessionGuard&) = delete;
    SessionGuard(SessionGuard&& o) noexcept : session_(o.session_) { o.session_ = nullptr; }
    SessionGuard& operator=(SessionGuard&& o) noexcept {
        if (this != &o) {
            if (session_) sqlite3session_delete(session_);
            session_ = o.session_;
            o.session_ = nullptr;
        }
        return *this;
    }

    sqlite3_session* get() const { return session_; }

    sqlite3_session* release() {
        auto* s = session_;
        session_ = nullptr;
        return s;
    }

private:
    sqlite3_session* session_ = nullptr;
};

} // namespace sqlpipe::detail

// ── meta.h ──────────────────────────────────────────────────────
namespace sqlpipe::detail {

/// Create _sqlpipe_meta table if it doesn't exist.
void ensure_meta_table(sqlite3* db);

/// Read the current sequence number from _sqlpipe_meta.
Seq read_seq(sqlite3* db, const std::string& key = "seq");

/// Write the sequence number to _sqlpipe_meta.
void write_seq(sqlite3* db, Seq seq, const std::string& key = "seq");

/// Compute a fingerprint of the user table definitions (excludes internal tables).
/// Uses FNV-1a over the sorted CREATE TABLE SQL.
/// If filter is non-null, only include tables in the filter.
SchemaVersion compute_schema_fingerprint(
    sqlite3* db, const std::set<std::string>* filter = nullptr);
SchemaVersion compute_schema_fingerprint_pg(const sqlift_exec& ex);

/// Get all user table names (excludes _sqlpipe_* and sqlite_* tables).
/// Only includes tables with explicit PRIMARY KEYs.
/// Rejects WITHOUT ROWID tables.
/// If filter is non-null, only include tables in the filter.
std::vector<std::string> get_tracked_tables(
    sqlite3* db, const std::set<std::string>* filter = nullptr,
    const LogCallback* on_log = nullptr);

/// Get the CREATE TABLE SQL for all tracked user tables.
/// If filter is non-null, only include tables in the filter.
std::string get_schema_sql(
    sqlite3* db, const std::set<std::string>* filter = nullptr);

/// Get the CREATE TABLE SQL for a single table.
std::string get_table_create_sql(sqlite3* db, const std::string& table);

/// Migrate the local schema to match the remote schema using sqlift.
/// Returns true if migration succeeded, false on error.
bool auto_migrate_schema(sqlite3* db, const std::string& remote_schema_sql,
                         const LogCallback& on_log);

/// Check if a table uses WITHOUT ROWID.
bool is_without_rowid(sqlite3* db, const std::string& table);

} // namespace sqlpipe::detail

// ── hash.h ──────────────────────────────────────────────────────
namespace sqlpipe::detail {

/// 64-bit FNV-1a hash of a row's column values (type-tagged).
std::uint64_t hash_row(sqlite3_stmt* stmt, int ncols);

/// Hash a (rowid, row_hash) pair for bucket accumulation.
std::uint64_t hash_bucket_entry(std::int64_t rowid, std::uint64_t row_hash);

struct RowHashInfo {
    std::int64_t  rowid;
    std::uint64_t hash;
};

/// Compute per-row hashes for rows in [lo, hi] rowid range.
std::vector<RowHashInfo> compute_row_hashes(
    sqlite3* db, const std::string& table,
    std::int64_t lo, std::int64_t hi);

struct BucketInfo {
    std::int64_t  lo, hi;
    std::uint64_t hash;
    std::int64_t  count;
};

/// Compute bucket hashes for a single table.
std::vector<BucketInfo> compute_table_buckets(
    sqlite3* db, const std::string& table, std::int64_t bucket_size);

/// Compute bucket hashes for all tracked tables, respecting filter.
std::vector<BucketHashEntry> compute_all_buckets(
    sqlite3* db, const std::set<std::string>* filter,
    std::int64_t bucket_size);

/// Build an INSERT patchset for specific rowids in a table.
Changeset build_insert_patchset(
    sqlite3* db, const std::string& table,
    const std::vector<std::int64_t>& rowids);

/// Combine multiple patchsets into one via sqlite3changegroup.
Changeset combine_patchsets(const std::vector<Changeset>& parts);

} // namespace sqlpipe::detail

// ── changeset_iter.h ────────────────────────────────────────────
namespace sqlpipe::detail {

/// Convert a sqlite3_value* to our Value variant.
Value to_value(sqlite3_value* val);

/// Convert an SQLite op code (SQLITE_INSERT/UPDATE/DELETE) to OpType.
OpType sqlite_op_to_optype(int sqlite_op);

/// Collect all row-level changes from a changeset into a vector.
std::vector<ChangeEvent> collect_events(const Changeset& data);

/// Build a ChangeEvent from a changeset iterator at its current position.
ChangeEvent extract_event(sqlite3_changeset_iter* iter);

} // namespace sqlpipe::detail

// ── protocol.cpp ────────────────────────────────────────────────
namespace sqlpipe {

namespace {

// ── Little-endian encoding helpers ──────────────────────────────────

void put_u8(std::vector<std::uint8_t>& buf, std::uint8_t v) {
    buf.push_back(v);
}

void put_u32(std::vector<std::uint8_t>& buf, std::uint32_t v) {
    buf.push_back(static_cast<std::uint8_t>(v));
    buf.push_back(static_cast<std::uint8_t>(v >> 8));
    buf.push_back(static_cast<std::uint8_t>(v >> 16));
    buf.push_back(static_cast<std::uint8_t>(v >> 24));
}

void put_i32(std::vector<std::uint8_t>& buf, std::int32_t v) {
    put_u32(buf, static_cast<std::uint32_t>(v));
}

void put_i64(std::vector<std::uint8_t>& buf, std::int64_t v) {
    auto u = static_cast<std::uint64_t>(v);
    for (int i = 0; i < 8; ++i) {
        buf.push_back(static_cast<std::uint8_t>(u >> (i * 8)));
    }
}

void put_u64(std::vector<std::uint8_t>& buf, std::uint64_t v) {
    for (int i = 0; i < 8; ++i) {
        buf.push_back(static_cast<std::uint8_t>(v >> (i * 8)));
    }
}

void put_bytes(std::vector<std::uint8_t>& buf,
               const void* data, std::uint32_t len) {
    put_u32(buf, len);
    auto* p = static_cast<const std::uint8_t*>(data);
    buf.insert(buf.end(), p, p + len);
}

void put_string(std::vector<std::uint8_t>& buf, const std::string& s) {
    put_bytes(buf, s.data(), static_cast<std::uint32_t>(s.size()));
}

// Schema fingerprint: length-prefixed opaque bytes (algorithm-agnostic wire
// format — the digest width may change without a wire/protocol change).
void put_fingerprint(std::vector<std::uint8_t>& buf, const SchemaVersion& fp) {
    put_bytes(buf, fp.data(), static_cast<std::uint32_t>(fp.size()));
}

void put_changeset(std::vector<std::uint8_t>& buf, const Changeset& cs) {
    constexpr std::size_t kCompressionThreshold = 64;
    if (cs.size() < kCompressionThreshold) {
        // Uncompressed: [u32 total] [0x00] [raw_data]
        put_u32(buf, static_cast<std::uint32_t>(cs.size() + 1));
        put_u8(buf, 0x00);
        buf.insert(buf.end(), cs.begin(), cs.end());
    } else {
        int src_size = static_cast<int>(cs.size());
        int max_dst = LZ4_compressBound(src_size);
        std::vector<std::uint8_t> tmp(max_dst);
        int compressed_size = LZ4_compress_default(
            reinterpret_cast<const char*>(cs.data()),
            reinterpret_cast<char*>(tmp.data()),
            src_size, max_dst);
        if (compressed_size > 0 &&
            compressed_size < static_cast<int>(cs.size())) {
            // LZ4: [u32 total] [0x01] [u32 original_len] [compressed_data]
            put_u32(buf, static_cast<std::uint32_t>(1 + 4 + compressed_size));
            put_u8(buf, 0x01);
            put_u32(buf, static_cast<std::uint32_t>(cs.size()));
            buf.insert(buf.end(), tmp.begin(),
                       tmp.begin() + compressed_size);
        } else {
            // Fallback: uncompressed
            put_u32(buf, static_cast<std::uint32_t>(cs.size() + 1));
            put_u8(buf, 0x00);
            buf.insert(buf.end(), cs.begin(), cs.end());
        }
    }
}

// ── Reader for deserialization ──────────────────────────────────────

class Reader {
public:
    Reader(std::span<const std::uint8_t> buf)
        : data_(buf.data()), size_(buf.size()), pos_(0) {}

    std::uint8_t read_u8() {
        check(1);
        return data_[pos_++];
    }

    std::uint32_t read_u32() {
        check(4);
        std::uint32_t v = 0;
        for (int i = 0; i < 4; ++i)
            v |= static_cast<std::uint32_t>(data_[pos_++]) << (i * 8);
        return v;
    }

    std::int32_t read_i32() {
        return static_cast<std::int32_t>(read_u32());
    }

    std::int64_t read_i64() {
        check(8);
        std::uint64_t v = 0;
        for (int i = 0; i < 8; ++i)
            v |= static_cast<std::uint64_t>(data_[pos_++]) << (i * 8);
        return static_cast<std::int64_t>(v);
    }

    std::uint64_t read_u64() {
        check(8);
        std::uint64_t v = 0;
        for (int i = 0; i < 8; ++i)
            v |= static_cast<std::uint64_t>(data_[pos_++]) << (i * 8);
        return v;
    }

    std::string read_string() {
        auto len = read_u32();
        if (len > kMaxMessageSize) {
            throw Error(ErrorCode::ProtocolError,
                        "string length exceeds limit");
        }
        check(len);
        std::string s(reinterpret_cast<const char*>(data_ + pos_), len);
        pos_ += len;
        return s;
    }

    SchemaVersion read_fingerprint() {
        auto len = read_u32();
        if (len > kMaxMessageSize) {
            throw Error(ErrorCode::ProtocolError,
                        "schema fingerprint length exceeds limit");
        }
        check(len);
        SchemaVersion fp(data_ + pos_, data_ + pos_ + len);
        pos_ += len;
        return fp;
    }

    Changeset read_changeset() {
        auto len = read_u32();
        if (len == 0) return {};
        check(len);
        auto type = read_u8();
        auto payload_len = len - 1;
        if (type == 0x00) {
            // Uncompressed
            Changeset cs(data_ + pos_, data_ + pos_ + payload_len);
            pos_ += payload_len;
            return cs;
        } else if (type == 0x01) {
            // LZ4
            auto original_len = read_u32();
            auto compressed_len = payload_len - 4;
            check(compressed_len);
            // Validate the wire-supplied decompressed length before allocating:
            // an unbounded original_len is a memory-amplification DoS and, for
            // values above INT_MAX, feeds a negative dstCapacity to LZ4 (F4).
            if (original_len > kMaxMessageSize) {
                throw Error(ErrorCode::ProtocolError,
                            "decompressed changeset length exceeds limit");
            }
            Changeset cs(original_len);
            int result = LZ4_decompress_safe(
                reinterpret_cast<const char*>(data_ + pos_),
                reinterpret_cast<char*>(cs.data()),
                static_cast<int>(compressed_len),
                static_cast<int>(original_len));
            if (result < 0) {
                throw Error(ErrorCode::ProtocolError,
                            "LZ4 decompression failed");
            }
            if (static_cast<std::size_t>(result) != original_len) {
                throw Error(ErrorCode::ProtocolError,
                            "decompressed changeset length mismatch");
            }
            pos_ += compressed_len;
            return cs;
        } else {
            throw Error(ErrorCode::ProtocolError,
                        "unknown changeset compression type");
        }
    }

    bool at_end() const { return pos_ >= size_; }

private:
    void check(std::size_t n) {
        if (pos_ + n > size_) {
            throw Error(ErrorCode::ProtocolError, "unexpected end of message");
        }
    }

    const std::uint8_t* data_;
    std::size_t size_;
    std::size_t pos_;
};

} // namespace

// ── serialize ───────────────────────────────────────────────────────

std::vector<std::uint8_t> serialize(const Message& msg) {
    std::vector<std::uint8_t> buf;
    // Reserve space for the 4-byte length prefix.
    buf.resize(4);

    std::visit([&](const auto& m) {
        using T = std::decay_t<decltype(m)>;

        if constexpr (std::is_same_v<T, HelloMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::Hello));
            put_u32(buf, m.protocol_version);
            put_fingerprint(buf, m.schema_version);
            put_u32(buf, static_cast<std::uint32_t>(m.owned_tables.size()));
            for (const auto& t : m.owned_tables) {
                put_string(buf, t);
            }
            put_i64(buf, m.last_seq);
        }
        else if constexpr (std::is_same_v<T, ChangesetMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::Changeset));
            put_i64(buf, m.seq);
            put_changeset(buf, m.data);
        }
        else if constexpr (std::is_same_v<T, AckMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::Ack));
            put_i64(buf, m.seq);
        }
        else if constexpr (std::is_same_v<T, ErrorMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::Error));
            put_i32(buf, static_cast<std::int32_t>(m.code));
            put_string(buf, m.detail);
            put_fingerprint(buf, m.remote_schema_version);
            put_string(buf, m.remote_schema_sql);
        }
        else if constexpr (std::is_same_v<T, BucketHashesMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::BucketHashes));
            put_i64(buf, m.last_seq);
            put_u32(buf, m.protocol_version);
            put_fingerprint(buf, m.schema_version);
            put_u32(buf, static_cast<std::uint32_t>(m.buckets.size()));
            for (const auto& b : m.buckets) {
                put_string(buf, b.table);
                put_i64(buf, b.bucket_lo);
                put_i64(buf, b.bucket_hi);
                put_u64(buf, b.hash);
                put_i64(buf, b.row_count);
            }
        }
        else if constexpr (std::is_same_v<T, NeedBucketsMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::NeedBuckets));
            put_u32(buf, static_cast<std::uint32_t>(m.ranges.size()));
            for (const auto& r : m.ranges) {
                put_string(buf, r.table);
                put_i64(buf, r.lo);
                put_i64(buf, r.hi);
            }
        }
        else if constexpr (std::is_same_v<T, RowHashesMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::RowHashes));
            put_u32(buf, static_cast<std::uint32_t>(m.entries.size()));
            for (const auto& e : m.entries) {
                put_string(buf, e.table);
                put_i64(buf, e.lo);
                put_i64(buf, e.hi);
                put_u32(buf, static_cast<std::uint32_t>(e.runs.size()));
                for (const auto& run : e.runs) {
                    put_i64(buf, run.start_rowid);
                    put_i64(buf, run.count);
                    for (std::int64_t i = 0; i < run.count; ++i) {
                        put_u64(buf, run.hashes[static_cast<std::size_t>(i)]);
                    }
                }
            }
        }
        else if constexpr (std::is_same_v<T, DiffReadyMsg>) {
            put_u8(buf, static_cast<std::uint8_t>(MessageTag::DiffReady));
            put_i64(buf, m.seq);
            put_changeset(buf, m.patchset);
            put_u32(buf, static_cast<std::uint32_t>(m.deletes.size()));
            for (const auto& td : m.deletes) {
                put_string(buf, td.table);
                put_u32(buf, static_cast<std::uint32_t>(td.rowids.size()));
                for (auto rid : td.rowids) {
                    put_i64(buf, rid);
                }
            }
        }
    }, msg);

    // Patch the length prefix: total_length = tag + payload (excludes the 4 prefix bytes).
    std::uint32_t total = static_cast<std::uint32_t>(buf.size() - 4);
    buf[0] = static_cast<std::uint8_t>(total);
    buf[1] = static_cast<std::uint8_t>(total >> 8);
    buf[2] = static_cast<std::uint8_t>(total >> 16);
    buf[3] = static_cast<std::uint8_t>(total >> 24);

    return buf;
}

// ── deserialize ─────────────────────────────────────────────────────

Message deserialize(std::span<const std::uint8_t> buf) {
    if (buf.size() < 5) {
        throw Error(ErrorCode::ProtocolError, "message too short");
    }
    if (buf.size() > kMaxMessageSize + 4) {
        throw Error(ErrorCode::ProtocolError,
                    "message exceeds maximum size (" +
                    std::to_string(buf.size()) + " bytes)");
    }

    Reader r(buf);
    auto total_len = r.read_u32();
    (void)total_len;  // already have the full buffer

    auto tag = static_cast<MessageTag>(r.read_u8());

    auto check_count = [](std::uint32_t n) {
        if (n > kMaxArrayCount) {
            throw Error(ErrorCode::ProtocolError,
                        "array count exceeds limit (" +
                        std::to_string(n) + ")");
        }
    };

    switch (tag) {
    case MessageTag::Hello: {
        HelloMsg m;
        m.protocol_version = r.read_u32();
        m.schema_version = r.read_fingerprint();
        {
            auto count = r.read_u32();
            check_count(count);
            for (std::uint32_t i = 0; i < count; ++i) {
                m.owned_tables.insert(r.read_string());
            }
        }
        m.last_seq = r.read_i64();
        return m;
    }
    case MessageTag::Changeset: {
        ChangesetMsg m;
        m.seq = r.read_i64();
        m.data = r.read_changeset();
        return m;
    }
    case MessageTag::Ack: {
        AckMsg m;
        m.seq = r.read_i64();
        return m;
    }
    case MessageTag::Error: {
        ErrorMsg m;
        m.code = static_cast<ErrorCode>(r.read_i32());
        m.detail = r.read_string();
        m.remote_schema_version = r.read_fingerprint();
        m.remote_schema_sql = r.read_string();
        return m;
    }
    case MessageTag::BucketHashes: {
        BucketHashesMsg m;
        m.last_seq = r.read_i64();
        m.protocol_version = r.read_u32();
        m.schema_version = r.read_fingerprint();
        auto count = r.read_u32();
        check_count(count);
        m.buckets.resize(count);
        for (std::uint32_t i = 0; i < count; ++i) {
            m.buckets[i].table = r.read_string();
            m.buckets[i].bucket_lo = r.read_i64();
            m.buckets[i].bucket_hi = r.read_i64();
            m.buckets[i].hash = r.read_u64();
            m.buckets[i].row_count = r.read_i64();
        }
        return m;
    }
    case MessageTag::NeedBuckets: {
        NeedBucketsMsg m;
        auto count = r.read_u32();
        check_count(count);
        m.ranges.resize(count);
        for (std::uint32_t i = 0; i < count; ++i) {
            m.ranges[i].table = r.read_string();
            m.ranges[i].lo = r.read_i64();
            m.ranges[i].hi = r.read_i64();
        }
        return m;
    }
    case MessageTag::RowHashes: {
        RowHashesMsg m;
        auto entry_count = r.read_u32();
        check_count(entry_count);
        m.entries.resize(entry_count);
        for (std::uint32_t i = 0; i < entry_count; ++i) {
            m.entries[i].table = r.read_string();
            m.entries[i].lo = r.read_i64();
            m.entries[i].hi = r.read_i64();
            auto run_count = r.read_u32();
            check_count(run_count);
            m.entries[i].runs.resize(run_count);
            for (std::uint32_t j = 0; j < run_count; ++j) {
                m.entries[i].runs[j].start_rowid = r.read_i64();
                auto run_len = r.read_i64();
                // run.count is an i64 read straight off the wire; validate it
                // before resize so a negative value (std::length_error) or a
                // huge one (std::bad_alloc / memory amplification) surfaces as
                // a ProtocolError instead of escaping the contract (F5).
                if (run_len < 0 ||
                    static_cast<std::uint64_t>(run_len) > kMaxArrayCount) {
                    throw Error(ErrorCode::ProtocolError,
                                "row hash run count out of range");
                }
                m.entries[i].runs[j].count = run_len;
                m.entries[i].runs[j].hashes.resize(
                    static_cast<std::size_t>(run_len));
                for (std::int64_t k = 0; k < m.entries[i].runs[j].count; ++k) {
                    m.entries[i].runs[j].hashes[static_cast<std::size_t>(k)] =
                        r.read_u64();
                }
            }
        }
        return m;
    }
    case MessageTag::DiffReady: {
        DiffReadyMsg m;
        m.seq = r.read_i64();
        m.patchset = r.read_changeset();
        auto del_count = r.read_u32();
        check_count(del_count);
        m.deletes.resize(del_count);
        for (std::uint32_t i = 0; i < del_count; ++i) {
            m.deletes[i].table = r.read_string();
            auto rid_count = r.read_u32();
            check_count(rid_count);
            m.deletes[i].rowids.resize(rid_count);
            for (std::uint32_t j = 0; j < rid_count; ++j) {
                m.deletes[i].rowids[j] = r.read_i64();
            }
        }
        return m;
    }
    default:
        throw Error(ErrorCode::ProtocolError,
                    "unknown message tag: " +
                    std::to_string(static_cast<int>(tag)));
    }
}

} // namespace sqlpipe

// ── meta.cpp ────────────────────────────────────────────────────

namespace sqlpipe::detail {

void ensure_meta_table(sqlite3* db) {
    exec(db,
        "CREATE TABLE IF NOT EXISTS _sqlpipe_meta ("
        "  key   TEXT PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ")");

    // Seed seq=0 if not present.
    exec(db,
        "INSERT OR IGNORE INTO _sqlpipe_meta (key, value) "
        "VALUES ('seq', '0')");
}

Seq read_seq(sqlite3* db, const std::string& key) {
    auto stmt = prepare(db,
        "SELECT value FROM _sqlpipe_meta WHERE key=?");
    sqlite3_bind_text(stmt.get(), 1, key.c_str(),
                      static_cast<int>(key.size()), SQLITE_TRANSIENT);
    int rc = sqlite3_step(stmt.get());
    if (rc == SQLITE_ROW) {
        return sqlite3_column_int64(stmt.get(), 0);
    }
    return 0;
}

void write_seq(sqlite3* db, Seq seq, const std::string& key) {
    auto stmt = prepare(db,
        "INSERT OR REPLACE INTO _sqlpipe_meta (key, value) VALUES (?, ?)");
    sqlite3_bind_text(stmt.get(), 1, key.c_str(),
                      static_cast<int>(key.size()), SQLITE_TRANSIENT);
    sqlite3_bind_int64(stmt.get(), 2, seq);
    step_done(db, stmt.get());
}

SchemaVersion compute_schema_fingerprint(
        sqlite3* db, const std::set<std::string>* filter) {
    auto sql = get_schema_sql(db, filter);

    int err_type;
    char* err_msg = nullptr;

    char* json = sqlift_parse(sql.c_str(), &err_type, &err_msg);
    if (!json) {
        std::string msg = err_msg ? err_msg : "unknown error";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError,
                    "sqlift_parse failed: " + msg);
    }

    char* hex = sqlift_schema_hash(json, &err_type, &err_msg);
    sqlift_free(json);
    if (!hex) {
        std::string msg = err_msg ? err_msg : "unknown error";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError,
                    "sqlift_schema_hash failed: " + msg);
    }

    // Carry the full structural hash as [algo_id][raw digest bytes]. sqlift
    // returns a hex string of a SHA-256 digest; decode it to raw bytes rather
    // than folding to a fixed-width integer (folding would reintroduce the
    // collisions this fingerprint exists to prevent). The leading algo byte
    // keeps the format open-ended: a future hash change bumps the id + digest
    // and needs no wire/protocol change.
    auto nibble = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return 0;
    };
    SchemaVersion fp;
    fp.push_back(static_cast<std::uint8_t>(
        SchemaFingerprintAlgo::SqliftSha256));
    for (const char* p = hex; p[0] && p[1]; p += 2) {
        fp.push_back(static_cast<std::uint8_t>(
            (nibble(p[0]) << 4) | nibble(p[1])));
    }
    sqlift_free(hex);
    return fp;
}

SchemaVersion fingerprint_from_sqlift_hex(
        SchemaFingerprintAlgo algo, const char* hex) {
    auto nibble = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return 0;
    };
    SchemaVersion fp;
    fp.push_back(static_cast<std::uint8_t>(algo));
    for (const char* p = hex; p[0] && p[1]; p += 2) {
        fp.push_back(static_cast<std::uint8_t>(
            (nibble(p[0]) << 4) | nibble(p[1])));
    }
    return fp;
}

SchemaVersion compute_schema_fingerprint_pg(const sqlift_exec& ex) {
    int err_type = 0;
    char* err_msg = nullptr;
    sqlift_db* sdb = sqlift_db_wrap_exec(SQLIFT_ENGINE_POSTGRES, ex,
                                         &err_type, &err_msg);
    if (!sdb) {
        std::string msg = err_msg ? err_msg : "wrap_exec failed";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError, "sqlift_db_wrap_exec: " + msg);
    }
    char* json = sqlift_extract(sdb, &err_type, &err_msg);
    sqlift_db_close(sdb);
    if (!json) {
        std::string msg = err_msg ? err_msg : "extract failed";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError, "sqlift_extract: " + msg);
    }
    char* hex = sqlift_schema_hash(json, &err_type, &err_msg);
    sqlift_free(json);
    if (!hex) {
        std::string msg = err_msg ? err_msg : "hash failed";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError, "sqlift_schema_hash: " + msg);
    }
    auto fp = fingerprint_from_sqlift_hex(
        SchemaFingerprintAlgo::SqliftPgSha256, hex);
    sqlift_free(hex);
    return fp;
}

// Lowercase-hex rendering of a schema fingerprint, for logs and error text.
std::string fingerprint_hex(const SchemaVersion& fp) {
    static const char digits[] = "0123456789abcdef";
    std::string s;
    s.reserve(fp.size() * 2);
    for (auto b : fp) {
        s.push_back(digits[b >> 4]);
        s.push_back(digits[b & 0x0F]);
    }
    return s;
}

bool is_without_rowid(sqlite3* db, const std::string& table) {
    // A WITHOUT ROWID table will fail when you try to select rowid.
    std::string sql = "SELECT rowid FROM \"" + table + "\" LIMIT 0";
    sqlite3_stmt* raw = nullptr;
    int rc = sqlite3_prepare_v2(db, sql.c_str(), -1, &raw, nullptr);
    if (raw) sqlite3_finalize(raw);
    return rc != SQLITE_OK;
}

std::vector<std::string> get_tracked_tables(
        sqlite3* db, const std::set<std::string>* filter,
        const LogCallback* on_log) {
    auto stmt = prepare(db,
        "SELECT name FROM sqlite_master "
        "WHERE type='table' "
        "  AND name NOT LIKE '_sqlpipe_%' "
        "  AND name NOT LIKE '_sqlift_%' "
        "  AND name NOT LIKE 'sqlite_%' "
        "ORDER BY name");

    std::vector<std::string> tables;
    while (sqlite3_step(stmt.get()) == SQLITE_ROW) {
        std::string name(reinterpret_cast<const char*>(
            sqlite3_column_text(stmt.get(), 0)));

        // Skip tables not in the filter (if provided).
        if (filter && filter->find(name) == filter->end()) {
            continue;
        }

        // Inspect the primary key. Diff sync identifies rows by rowid, which
        // is only stable across databases when the PK is a single INTEGER
        // PRIMARY KEY (a rowid alias). PRAGMA table_info column 5 is the
        // 1-based PK position (0 = not part of the PK); column 2 is the
        // declared type.
        std::string pragma = "PRAGMA table_info('" + name + "')";
        auto pk_stmt = prepare(db, pragma.c_str());
        int pk_count = 0;
        std::string pk_type;
        while (sqlite3_step(pk_stmt.get()) == SQLITE_ROW) {
            if (sqlite3_column_int(pk_stmt.get(), 5) > 0) {  // pk column
                ++pk_count;
                const unsigned char* t = sqlite3_column_text(pk_stmt.get(), 2);
                pk_type = t ? reinterpret_cast<const char*>(t) : "";
            }
        }

        if (pk_count == 0) {
            SQLPIPE_LOG(on_log ? *on_log : LogCallback{}, LogLevel::Warn,
                       "table '{}' has no explicit PRIMARY KEY, skipping", name);
            continue;
        }

        // Reject WITHOUT ROWID tables.
        if (is_without_rowid(db, name)) {
            throw Error(ErrorCode::WithoutRowidTable,
                        "table '" + name + "' uses WITHOUT ROWID (not supported)");
        }

        // Reject tables whose PK is not an INTEGER PRIMARY KEY rowid alias
        // (a composite PK, or a single PK column not declared exactly
        // "INTEGER"). For those the rowid is assigned independently per
        // database, so rowid-keyed diff sync would conflate distinct logical
        // rows and silently diverge / lose data. Fail closed at construction.
        for (auto& ch : pk_type) {
            if (ch >= 'a' && ch <= 'z') ch = static_cast<char>(ch - 32);
        }
        if (pk_count != 1 || pk_type != "INTEGER") {
            throw Error(ErrorCode::WithoutRowidTable,
                        "table '" + name + "' primary key is not an INTEGER "
                        "PRIMARY KEY rowid alias (not supported)");
        }

        tables.push_back(std::move(name));
    }

    return tables;
}

std::string get_schema_sql(
        sqlite3* db, const std::set<std::string>* filter) {
    auto tables = get_tracked_tables(db, filter);
    std::string sql;
    for (const auto& t : tables) {
        if (!sql.empty()) sql += ";\n";
        sql += get_table_create_sql(db, t);
    }
    if (!sql.empty()) sql += ";";
    return sql;
}

bool auto_migrate_schema(sqlite3* db, const std::string& remote_schema_sql,
                         const LogCallback& on_log) {
    int err_type = 0;
    char* err_msg = nullptr;

    // Parse the remote (desired) schema.
    char* desired_json = sqlift_parse(remote_schema_sql.c_str(),
                                      &err_type, &err_msg);
    if (!desired_json) {
        SQLPIPE_LOG(on_log, LogLevel::Error,
                    "auto_migrate: failed to parse remote schema: {}",
                    err_msg ? err_msg : "unknown");
        sqlift_free(err_msg);
        return false;
    }

    // Get the local user-table schema (excludes _sqlpipe_meta and other
    // internal tables), then parse it with sqlift.
    auto local_sql = get_schema_sql(db);
    char* current_json = sqlift_parse(
        local_sql.empty() ? "" : local_sql.c_str(),
        &err_type, &err_msg);
    if (!current_json) {
        SQLPIPE_LOG(on_log, LogLevel::Error,
                    "auto_migrate: failed to parse local schema: {}",
                    err_msg ? err_msg : "unknown");
        sqlift_free(err_msg);
        sqlift_free(desired_json);
        return false;
    }

    // Diff current → desired.
    char* plan_json = sqlift_diff(current_json, desired_json,
                                  &err_type, &err_msg);
    sqlift_free(current_json);
    sqlift_free(desired_json);
    if (!plan_json) {
        SQLPIPE_LOG(on_log, LogLevel::Error,
                    "auto_migrate: failed to diff schemas: {}",
                    err_msg ? err_msg : "unknown");
        sqlift_free(err_msg);
        return false;
    }

    // Apply the migration plan (allow rebuilds + destructive — master is authoritative).
    sqlift_db* sdb = sqlift_db_wrap(db);
    int rc = sqlift_apply(sdb, plan_json, {.allow = SQLIFT_ALLOW_ALL},
                          &err_type, &err_msg);
    if (rc != 0) {
        // format_sqlift_failure is defined later in this TU (with Database).
        // Inline the same shape here for log-only auto-migrate failures.
        std::string detail = err_msg ? err_msg : "unknown";
        SQLPIPE_LOG(on_log, LogLevel::Error,
                    "auto_migrate: failed to apply migration [sqlift err_type={}]: {} | plan={}",
                    err_type, detail, plan_json ? plan_json : "");
        sqlift_free(err_msg);
        sqlift_free(plan_json);
        sqlift_db_close(sdb);
        return false;
    }
    sqlift_free(plan_json);
    sqlift_db_close(sdb);

    SQLPIPE_LOG(on_log, LogLevel::Info,
                "auto_migrate: schema migrated to match master");
    return true;
}

std::string get_table_create_sql(sqlite3* db, const std::string& table) {
    auto stmt = prepare(db,
        "SELECT sql FROM sqlite_master WHERE type='table' AND name=?");
    sqlite3_bind_text(stmt.get(), 1, table.c_str(),
                      static_cast<int>(table.size()), SQLITE_TRANSIENT);
    int rc = sqlite3_step(stmt.get());
    if (rc == SQLITE_ROW) {
        return reinterpret_cast<const char*>(sqlite3_column_text(stmt.get(), 0));
    }
    throw Error(ErrorCode::SqliteError,
                "table '" + table + "' not found in sqlite_master");
}

} // namespace sqlpipe::detail

// ── hash.cpp ────────────────────────────────────────────────────

namespace sqlpipe::detail {

namespace {
constexpr std::uint64_t kFnv64Offset = 14695981039346656037ULL;
constexpr std::uint64_t kFnv64Prime  = 1099511628211ULL;

inline void fnv64_byte(std::uint64_t& hash, std::uint8_t b) {
    hash ^= b;
    hash *= kFnv64Prime;
}

inline void fnv64_bytes(std::uint64_t& hash,
                        const void* data, std::size_t len) {
    auto* p = static_cast<const std::uint8_t*>(data);
    for (std::size_t i = 0; i < len; ++i) {
        fnv64_byte(hash, p[i]);
    }
}
} // namespace

/// Feed a Value into a running FNV-1a hash (type-tagged, same encoding as hash_row).
inline void hash_value(std::uint64_t& hash, const Value& v) {
    std::visit([&](const auto& val) {
        using T = std::decay_t<decltype(val)>;
        if constexpr (std::is_same_v<T, std::monostate>) {
            fnv64_byte(hash, 0x00);
        } else if constexpr (std::is_same_v<T, std::int64_t>) {
            fnv64_byte(hash, 0x01);
            auto u = static_cast<std::uint64_t>(val);
            std::uint8_t bytes[8];
            for (int j = 0; j < 8; ++j)
                bytes[j] = static_cast<std::uint8_t>(u >> (j * 8));
            fnv64_bytes(hash, bytes, 8);
        } else if constexpr (std::is_same_v<T, double>) {
            fnv64_byte(hash, 0x02);
            std::uint8_t bytes[8];
            std::memcpy(bytes, &val, 8);
            fnv64_bytes(hash, bytes, 8);
        } else if constexpr (std::is_same_v<T, std::string>) {
            fnv64_byte(hash, 0x03);
            std::uint32_t ulen = static_cast<std::uint32_t>(val.size());
            std::uint8_t lenbytes[4];
            for (int j = 0; j < 4; ++j)
                lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
            fnv64_bytes(hash, lenbytes, 4);
            fnv64_bytes(hash, val.data(), val.size());
        } else if constexpr (std::is_same_v<T, std::vector<std::uint8_t>>) {
            fnv64_byte(hash, 0x04);
            std::uint32_t ulen = static_cast<std::uint32_t>(val.size());
            std::uint8_t lenbytes[4];
            for (int j = 0; j < 4; ++j)
                lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
            fnv64_bytes(hash, lenbytes, 4);
            fnv64_bytes(hash, val.data(), val.size());
        }
    }, v);
}

std::uint64_t hash_row(sqlite3_stmt* stmt, int ncols) {
    std::uint64_t hash = kFnv64Offset;

    for (int i = 0; i < ncols; ++i) {
        int type = sqlite3_column_type(stmt, i);
        switch (type) {
        case SQLITE_NULL:
            fnv64_byte(hash, 0x00);
            break;
        case SQLITE_INTEGER: {
            fnv64_byte(hash, 0x01);
            auto val = sqlite3_column_int64(stmt, i);
            auto u = static_cast<std::uint64_t>(val);
            std::uint8_t bytes[8];
            for (int j = 0; j < 8; ++j)
                bytes[j] = static_cast<std::uint8_t>(u >> (j * 8));
            fnv64_bytes(hash, bytes, 8);
            break;
        }
        case SQLITE_FLOAT: {
            fnv64_byte(hash, 0x02);
            double val = sqlite3_column_double(stmt, i);
            std::uint8_t bytes[8];
            std::memcpy(bytes, &val, 8);
            fnv64_bytes(hash, bytes, 8);
            break;
        }
        case SQLITE_TEXT: {
            fnv64_byte(hash, 0x03);
            int len = sqlite3_column_bytes(stmt, i);
            std::uint32_t ulen = static_cast<std::uint32_t>(len);
            std::uint8_t lenbytes[4];
            for (int j = 0; j < 4; ++j)
                lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
            fnv64_bytes(hash, lenbytes, 4);
            fnv64_bytes(hash, sqlite3_column_text(stmt, i),
                        static_cast<std::size_t>(len));
            break;
        }
        case SQLITE_BLOB: {
            fnv64_byte(hash, 0x04);
            int len = sqlite3_column_bytes(stmt, i);
            std::uint32_t ulen = static_cast<std::uint32_t>(len);
            std::uint8_t lenbytes[4];
            for (int j = 0; j < 4; ++j)
                lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
            fnv64_bytes(hash, lenbytes, 4);
            fnv64_bytes(hash, sqlite3_column_blob(stmt, i),
                        static_cast<std::size_t>(len));
            break;
        }
        default:
            fnv64_byte(hash, 0x00);
            break;
        }
    }
    return hash;
}

std::uint64_t hash_bucket_entry(std::int64_t rowid, std::uint64_t row_hash) {
    std::uint64_t hash = kFnv64Offset;
    auto u = static_cast<std::uint64_t>(rowid);
    std::uint8_t bytes[8];
    for (int i = 0; i < 8; ++i)
        bytes[i] = static_cast<std::uint8_t>(u >> (i * 8));
    fnv64_bytes(hash, bytes, 8);
    for (int i = 0; i < 8; ++i)
        bytes[i] = static_cast<std::uint8_t>(row_hash >> (i * 8));
    fnv64_bytes(hash, bytes, 8);
    return hash;
}

std::vector<RowHashInfo> compute_row_hashes(
        sqlite3* db, const std::string& table,
        std::int64_t lo, std::int64_t hi) {
    std::string sql = "SELECT rowid, * FROM \"" + table +
                      "\" WHERE rowid >= ? AND rowid <= ? ORDER BY rowid";
    auto stmt = prepare(db, sql.c_str());
    sqlite3_bind_int64(stmt.get(), 1, lo);
    sqlite3_bind_int64(stmt.get(), 2, hi);

    int ncols = sqlite3_column_count(stmt.get());

    std::vector<RowHashInfo> result;
    while (sqlite3_step(stmt.get()) == SQLITE_ROW) {
        std::int64_t rowid = sqlite3_column_int64(stmt.get(), 0);
        // Hash columns 1..ncols-1 (skip the rowid column at position 0).
        std::uint64_t h = kFnv64Offset;
        for (int c = 1; c < ncols; ++c) {
            int type = sqlite3_column_type(stmt.get(), c);
            switch (type) {
            case SQLITE_NULL:
                fnv64_byte(h, 0x00);
                break;
            case SQLITE_INTEGER: {
                fnv64_byte(h, 0x01);
                auto val = sqlite3_column_int64(stmt.get(), c);
                auto u = static_cast<std::uint64_t>(val);
                std::uint8_t bytes[8];
                for (int j = 0; j < 8; ++j)
                    bytes[j] = static_cast<std::uint8_t>(u >> (j * 8));
                fnv64_bytes(h, bytes, 8);
                break;
            }
            case SQLITE_FLOAT: {
                fnv64_byte(h, 0x02);
                double val = sqlite3_column_double(stmt.get(), c);
                std::uint8_t bytes[8];
                std::memcpy(bytes, &val, 8);
                fnv64_bytes(h, bytes, 8);
                break;
            }
            case SQLITE_TEXT: {
                fnv64_byte(h, 0x03);
                int len = sqlite3_column_bytes(stmt.get(), c);
                std::uint32_t ulen = static_cast<std::uint32_t>(len);
                std::uint8_t lenbytes[4];
                for (int j = 0; j < 4; ++j)
                    lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
                fnv64_bytes(h, lenbytes, 4);
                fnv64_bytes(h, sqlite3_column_text(stmt.get(), c),
                            static_cast<std::size_t>(len));
                break;
            }
            case SQLITE_BLOB: {
                fnv64_byte(h, 0x04);
                int len = sqlite3_column_bytes(stmt.get(), c);
                std::uint32_t ulen = static_cast<std::uint32_t>(len);
                std::uint8_t lenbytes[4];
                for (int j = 0; j < 4; ++j)
                    lenbytes[j] = static_cast<std::uint8_t>(ulen >> (j * 8));
                fnv64_bytes(h, lenbytes, 4);
                fnv64_bytes(h, sqlite3_column_blob(stmt.get(), c),
                            static_cast<std::size_t>(len));
                break;
            }
            default:
                fnv64_byte(h, 0x00);
                break;
            }
        }
        result.push_back({rowid, h});
    }
    return result;
}

std::vector<BucketInfo> compute_table_buckets(
        sqlite3* db, const std::string& table, std::int64_t bucket_size) {
    // Get min and max rowid.
    std::string minmax_sql = "SELECT MIN(rowid), MAX(rowid) FROM \"" + table + "\"";
    auto stmt = prepare(db, minmax_sql.c_str());
    if (sqlite3_step(stmt.get()) != SQLITE_ROW ||
        sqlite3_column_type(stmt.get(), 0) == SQLITE_NULL) {
        return {};  // empty table
    }
    std::int64_t min_rid = sqlite3_column_int64(stmt.get(), 0);
    std::int64_t max_rid = sqlite3_column_int64(stmt.get(), 1);

    std::int64_t lo_bucket = min_rid / bucket_size;
    if (min_rid < 0) lo_bucket = (min_rid - bucket_size + 1) / bucket_size;
    std::int64_t hi_bucket = max_rid / bucket_size;
    if (max_rid < 0) hi_bucket = (max_rid - bucket_size + 1) / bucket_size;

    std::vector<BucketInfo> buckets;
    for (std::int64_t k = lo_bucket; k <= hi_bucket; ++k) {
        std::int64_t blo = k * bucket_size;
        std::int64_t bhi = blo + bucket_size - 1;

        auto rows = compute_row_hashes(db, table, blo, bhi);
        if (rows.empty()) continue;

        std::uint64_t bucket_hash = 0;
        for (const auto& row : rows) {
            bucket_hash ^= hash_bucket_entry(row.rowid, row.hash);
        }
        buckets.push_back({blo, bhi, bucket_hash,
                           static_cast<std::int64_t>(rows.size())});
    }
    return buckets;
}

std::vector<BucketHashEntry> compute_all_buckets(
        sqlite3* db, const std::set<std::string>* filter,
        std::int64_t bucket_size) {
    auto tables = get_tracked_tables(db, filter);
    std::vector<BucketHashEntry> result;
    for (const auto& table : tables) {
        auto buckets = compute_table_buckets(db, table, bucket_size);
        for (auto& b : buckets) {
            result.push_back(BucketHashEntry{
                table, b.lo, b.hi, b.hash, b.count});
        }
    }
    return result;
}

Changeset build_insert_patchset(
        sqlite3* db, const std::string& table,
        const std::vector<std::int64_t>& rowids) {
    if (rowids.empty()) return {};

    auto create_sql = get_table_create_sql(db, table);

    exec(db, "ATTACH ':memory:' AS _sqlpipe_stage");

    // Detach _sqlpipe_stage on every exit path, including exceptions. Without
    // this, a throw between here and the explicit detach (e.g. a generated
    // column making the INSERT ... SELECT * fail) leaves the schema name bound
    // to the caller's connection, and every future diff sync fails at ATTACH
    // with "database _sqlpipe_stage is already in use" (F8). The session
    // (SessionGuard, declared below) is destroyed before this guard runs, so
    // the detach always sees no attached session.
    struct DetachGuard {
        sqlite3* db;
        ~DetachGuard() {
            sqlite3_exec(db, "DETACH _sqlpipe_stage", nullptr, nullptr, nullptr);
        }
    } detach_guard{db};

    // Create table in _sqlpipe_stage.
    std::string prefixed = create_sql;
    auto pos = prefixed.find("CREATE TABLE ");
    if (pos != std::string::npos) {
        prefixed.insert(pos + 13, "_sqlpipe_stage.");
    }
    exec(db, prefixed.c_str());

    // Create session on _sqlpipe_stage.
    sqlite3_session* raw = nullptr;
    int rc = sqlite3session_create(db, "_sqlpipe_stage", &raw);
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError,
                    std::string("session_create: ") + sqlite3_errmsg(db));
    }
    SessionGuard session(raw);

    rc = sqlite3session_attach(raw, table.c_str());
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError,
                    std::string("session_attach: ") + sqlite3_errmsg(db));
    }

    // Insert target rows from main into _sqlpipe_stage.
    // Build the IN clause or use individual inserts.
    for (auto rid : rowids) {
        std::string ins_sql =
            "INSERT INTO _sqlpipe_stage.\"" + table +
            "\" SELECT * FROM main.\"" + table + "\" WHERE rowid = ?";
        auto ins_stmt = prepare(db, ins_sql.c_str());
        sqlite3_bind_int64(ins_stmt.get(), 1, rid);
        step_done(db, ins_stmt.get());
    }

    // Extract patchset.
    int n = 0;
    void* p = nullptr;
    rc = sqlite3session_patchset(raw, &n, &p);
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError,
                    std::string("session_patchset: ") + sqlite3_errmsg(db));
    }

    Changeset cs;
    if (n > 0 && p) {
        cs.assign(static_cast<std::uint8_t*>(p),
                  static_cast<std::uint8_t*>(p) + n);
    }
    sqlite3_free(p);

    // Cleanup. Free the session, then drop the staged table; DetachGuard
    // detaches _sqlpipe_stage on return.
    session = SessionGuard{};  // delete before detach
    exec(db, ("DROP TABLE _sqlpipe_stage.\"" + table + "\"").c_str());

    return cs;
}

Changeset combine_patchsets(const std::vector<Changeset>& parts) {
    if (parts.empty()) return {};
    if (parts.size() == 1) return parts[0];

    sqlite3_changegroup* grp = nullptr;
    int rc = sqlite3changegroup_new(&grp);
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError, "sqlite3changegroup_new failed");
    }

    for (const auto& cs : parts) {
        if (cs.empty()) continue;
        rc = sqlite3changegroup_add(grp,
            static_cast<int>(cs.size()),
            const_cast<void*>(static_cast<const void*>(cs.data())));
        if (rc != SQLITE_OK) {
            sqlite3changegroup_delete(grp);
            throw Error(ErrorCode::SqliteError, "sqlite3changegroup_add failed");
        }
    }

    int n = 0;
    void* p = nullptr;
    rc = sqlite3changegroup_output(grp, &n, &p);
    sqlite3changegroup_delete(grp);

    if (rc != SQLITE_OK) {
        sqlite3_free(p);
        throw Error(ErrorCode::SqliteError, "sqlite3changegroup_output failed");
    }

    Changeset result;
    if (n > 0 && p) {
        result.assign(static_cast<std::uint8_t*>(p),
                      static_cast<std::uint8_t*>(p) + n);
    }
    sqlite3_free(p);
    return result;
}

} // namespace sqlpipe::detail

// ── changeset_iter.cpp ──────────────────────────────────────────

namespace sqlpipe::detail {

Value to_value(sqlite3_value* val) {
    if (!val) return std::monostate{};

    switch (sqlite3_value_type(val)) {
    case SQLITE_NULL:
        return std::monostate{};
    case SQLITE_INTEGER:
        return sqlite3_value_int64(val);
    case SQLITE_FLOAT:
        return sqlite3_value_double(val);
    case SQLITE_TEXT: {
        auto* text = reinterpret_cast<const char*>(sqlite3_value_text(val));
        int len = sqlite3_value_bytes(val);
        return std::string(text, static_cast<std::size_t>(len));
    }
    case SQLITE_BLOB: {
        auto* data = static_cast<const std::uint8_t*>(sqlite3_value_blob(val));
        int len = sqlite3_value_bytes(val);
        return std::vector<std::uint8_t>(data, data + len);
    }
    default:
        return std::monostate{};
    }
}

OpType sqlite_op_to_optype(int sqlite_op) {
    switch (sqlite_op) {
    case SQLITE_INSERT: return OpType::Insert;
    case SQLITE_UPDATE: return OpType::Update;
    case SQLITE_DELETE: return OpType::Delete;
    default:
        throw Error(ErrorCode::ProtocolError,
                    "unknown sqlite operation: " + std::to_string(sqlite_op));
    }
}

ChangeEvent extract_event(sqlite3_changeset_iter* iter) {
    const char* table = nullptr;
    int ncol = 0, op = 0, indirect = 0;
    sqlite3changeset_op(iter, &table, &ncol, &op, &indirect);

    unsigned char* pk_raw = nullptr;
    int pk_ncol = 0;
    sqlite3changeset_pk(iter, &pk_raw, &pk_ncol);

    ChangeEvent event;
    event.table = table ? table : "";
    event.op = sqlite_op_to_optype(op);

    event.pk_flags.resize(static_cast<std::size_t>(ncol));
    for (int i = 0; i < ncol; ++i) {
        event.pk_flags[static_cast<std::size_t>(i)] = (pk_raw[i] != 0);
    }

    if (op == SQLITE_UPDATE || op == SQLITE_DELETE) {
        event.old_values.resize(static_cast<std::size_t>(ncol));
        for (int i = 0; i < ncol; ++i) {
            sqlite3_value* val = nullptr;
            sqlite3changeset_old(iter, i, &val);
            event.old_values[static_cast<std::size_t>(i)] = to_value(val);
        }
    }

    if (op == SQLITE_INSERT || op == SQLITE_UPDATE) {
        event.new_values.resize(static_cast<std::size_t>(ncol));
        for (int i = 0; i < ncol; ++i) {
            sqlite3_value* val = nullptr;
            sqlite3changeset_new(iter, i, &val);
            event.new_values[static_cast<std::size_t>(i)] = to_value(val);
        }
    }

    return event;
}

std::vector<ChangeEvent> collect_events(const Changeset& data) {
    std::vector<ChangeEvent> events;
    if (data.empty()) return events;

    sqlite3_changeset_iter* iter = nullptr;
    int rc = sqlite3changeset_start(
        &iter,
        static_cast<int>(data.size()),
        const_cast<void*>(static_cast<const void*>(data.data())));
    if (rc != SQLITE_OK) {
        throw Error(ErrorCode::SqliteError, "sqlite3changeset_start failed");
    }

    while (sqlite3changeset_next(iter) == SQLITE_ROW) {
        events.push_back(extract_event(iter));
    }

    sqlite3changeset_finalize(iter);
    return events;
}

} // namespace sqlpipe::detail

// ── master.cpp ──────────────────────────────────────────────────
namespace sqlpipe {

#ifndef EMSCRIPTEN
#ifdef SQLPIPE_HAVE_POSTGRES
struct PgMasterState;
struct PgReplicaState;
#endif
#endif

struct Master::Impl {
    sqlite3*                 db = nullptr;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    std::unique_ptr<PgMasterState> pg;
#endif
#endif
    MasterConfig             config;
    detail::SessionGuard     session;
    std::vector<std::string> tracked_tables;
    Seq                      seq = 0;
    SchemaVersion            cached_sv;  // empty until first computed

    // Changeset queue for replay on reconnect.
    struct QueuedChangeset {
        Seq       seq;
        Changeset data;
    };
    std::deque<QueuedChangeset> changeset_queue;

    void enqueue_changeset(Seq s, const Changeset& data) {
        if (config.changeset_queue_size == 0) return;
        changeset_queue.push_back({s, data});
        while (changeset_queue.size() > config.changeset_queue_size) {
            changeset_queue.pop_front();
        }
    }

    // Diff handshake state.
    enum class HSState : std::uint8_t {
        Idle,
        WaitBucketHashes,
        WaitRowHashes,
        Live,
    };
    HSState hs_state = HSState::Idle;

    // Stored between rounds: the ranges we asked for row hashes.
    std::vector<NeedBucketRange> pending_ranges;

    const std::set<std::string>* filter() const {
        return config.table_filter ? &*config.table_filter : nullptr;
    }

    void report(DiffPhase phase, const std::string& table,
                std::int64_t done, std::int64_t total) {
        if (config.on_progress) {
            config.on_progress(DiffProgress{phase, table, done, total});
        }
    }

    bool flush_pending = false;
    bool in_auto_flush = false;

    static int commit_hook_cb(void* ctx) {
        auto* self = static_cast<Impl*>(ctx);
        if (!self->in_auto_flush) {
            self->flush_pending = true;
        }
        return 0;
    }

    void auto_flush() {
        // flush_pending is one-shot: consume it on entry so a later statement
        // that does not commit never re-triggers a flush (F2). in_auto_flush
        // is reset on every exit path — including exceptions from on_flush or
        // write_seq — so a transient failure cannot permanently disarm the
        // commit hook (F12).
        flush_pending = false;
        in_auto_flush = true;
        struct ResetGuard {
            Impl* self;
            ~ResetGuard() { self->in_auto_flush = false; }
        } reset_guard{this};

        // Check for schema changes.
        auto sv = detail::compute_schema_fingerprint(db, filter());
        if (sv != cached_sv) {
            cached_sv = sv;
            scan_tables();
            recreate_session();
        }

        auto cs = extract_changeset();
        if (!cs.empty()) {
            recreate_session();
            seq++;
            detail::write_seq(db, seq, config.seq_key);

            SQLPIPE_LOG(config.on_log, LogLevel::Debug,
                        "auto-flushed changeset seq={} ({} bytes)", seq, cs.size());

            enqueue_changeset(seq, cs);

            std::vector<OutMessage> msgs = {
                tagged(Message{ChangesetMsg{seq, std::move(cs)}})};
            config.on_flush(msgs);
        }
    }

    void init() {
        detail::ensure_meta_table(db);
        seq = detail::read_seq(db, config.seq_key);
        cached_sv = detail::compute_schema_fingerprint(db, filter());
        scan_tables();
        recreate_session();

        if (config.on_flush) {
            sqlite3_commit_hook(db, &Impl::commit_hook_cb, this);
        }

        SQLPIPE_LOG(config.on_log, LogLevel::Info, "master initialized at seq={}", seq);
    }

    void scan_tables() {
        tracked_tables = detail::get_tracked_tables(db, filter(),
            config.on_log ? &config.on_log : nullptr);
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "tracking {} tables", tracked_tables.size());
    }

    void recreate_session() {
        session = detail::SessionGuard{};  // delete old

        sqlite3_session* raw = nullptr;
        int rc = sqlite3session_create(db, "main", &raw);
        if (rc != SQLITE_OK) {
            throw Error(ErrorCode::SqliteError,
                        std::string("sqlite3session_create: ") + sqlite3_errmsg(db));
        }
        session = detail::SessionGuard(raw);

        for (const auto& t : tracked_tables) {
            rc = sqlite3session_attach(raw, t.c_str());
            if (rc != SQLITE_OK) {
                throw Error(ErrorCode::SqliteError,
                            std::string("sqlite3session_attach: ") + sqlite3_errmsg(db));
            }
        }
    }

    Changeset extract_changeset() {
        int n = 0;
        void* p = nullptr;
        int rc = sqlite3session_changeset(session.get(), &n, &p);
        if (rc != SQLITE_OK) {
            throw Error(ErrorCode::SqliteError,
                        std::string("sqlite3session_changeset: ") + sqlite3_errmsg(db));
        }

        Changeset cs;
        if (n > 0 && p) {
            cs.assign(static_cast<std::uint8_t*>(p),
                      static_cast<std::uint8_t*>(p) + n);
        }
        sqlite3_free(p);
        return cs;
    }

    std::vector<OutMessage> handle_hello(const HelloMsg& hello) {
        if (hello.protocol_version != kProtocolVersion) {
            return {tagged(Message{ErrorMsg{ErrorCode::ProtocolError,
                "unsupported protocol version: " +
                std::to_string(hello.protocol_version)}})};
        }

        auto my_sv = detail::compute_schema_fingerprint(db, filter());

        // Schema mismatch → invoke callback or error.
        if (hello.schema_version != my_sv) {
            if (config.on_schema_mismatch &&
                config.on_schema_mismatch(hello.schema_version, my_sv, "")) {
                // Callback may have modified the schema. Recompute.
                cached_sv = detail::compute_schema_fingerprint(db, filter());
                scan_tables();
                recreate_session();
                my_sv = cached_sv;
            }
            if (hello.schema_version != my_sv) {
                SQLPIPE_LOG(config.on_log, LogLevel::Info,
                            "schema mismatch (replica={}, master={})",
                            detail::fingerprint_hex(hello.schema_version),
                            detail::fingerprint_hex(my_sv));
                return {tagged(Message{ErrorMsg{ErrorCode::SchemaMismatch,
                    "schema mismatch: replica=" +
                    detail::fingerprint_hex(hello.schema_version) +
                    " master=" + detail::fingerprint_hex(my_sv),
                    my_sv,
                    detail::get_schema_sql(db, filter())}})};
            }
        }

        // Fast reconnect: if replica's seq matches ours and both > 0,
        // skip diff sync entirely.
        if (hello.last_seq > 0 && hello.last_seq == seq) {
            hs_state = HSState::Live;
            SQLPIPE_LOG(config.on_log, LogLevel::Info,
                        "hello ok, seq match ({}), skipping diff sync", seq);
            return {tagged(Message{HelloMsg{kProtocolVersion, my_sv, {}, seq}})};
        }

        // Queue replay: if replica's seq is behind but still within our
        // changeset queue, replay the queued changesets instead of
        // running full diff sync. Send HelloMsg with last_seq matching
        // the replica's seq so it triggers fast reconnect to Live,
        // then the queued changesets apply in Live state.
        if (hello.last_seq > 0 && !changeset_queue.empty() &&
            hello.last_seq >= changeset_queue.front().seq - 1) {
            // Find the first changeset after the replica's seq.
            std::vector<OutMessage> result;
            result.push_back(tagged(Message{
                HelloMsg{kProtocolVersion, my_sv, {}, hello.last_seq}}));
            std::size_t replayed = 0;
            for (const auto& qc : changeset_queue) {
                if (qc.seq > hello.last_seq) {
                    result.push_back(tagged(Message{
                        ChangesetMsg{qc.seq, qc.data}}));
                    ++replayed;
                }
            }
            if (replayed > 0) {
                hs_state = HSState::Live;
                SQLPIPE_LOG(config.on_log, LogLevel::Info,
                            "queue replay: {} changesets (seq {}→{})",
                            replayed, hello.last_seq + 1, seq);
                return result;
            }
        }

        hs_state = HSState::WaitBucketHashes;
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "hello ok, waiting for bucket hashes");
        return {tagged(Message{HelloMsg{kProtocolVersion, my_sv, {}, -1}})};
    }

    std::vector<OutMessage> handle_bucket_hashes(const BucketHashesMsg& msg) {
        // Accept BucketHashes in any state — enables convergence loop
        // where the replica can initiate diff sync at any time, including
        // re-convergence checks while already Live.
        //
        // Clear any pending state from a previous round. If the replica
        // calls converge() while a previous round is in flight, this
        // ensures the master starts fresh rather than mixing rounds.
        pending_ranges.clear();

        // Protocol version check (if provided).
        if (msg.protocol_version != 0 &&
            msg.protocol_version != kProtocolVersion) {
            return {tagged(Message{ErrorMsg{ErrorCode::ProtocolError,
                "unsupported protocol version: " +
                std::to_string(msg.protocol_version)}})};
        }

        // Schema check (if provided).
        if (!msg.schema_version.empty()) {
            auto my_sv = detail::compute_schema_fingerprint(db, filter());
            if (msg.schema_version != my_sv) {
                if (config.on_schema_mismatch &&
                    config.on_schema_mismatch(msg.schema_version, my_sv, "")) {
                    cached_sv = detail::compute_schema_fingerprint(db, filter());
                    scan_tables();
                    recreate_session();
                    my_sv = cached_sv;
                }
                if (msg.schema_version != my_sv) {
                    SQLPIPE_LOG(config.on_log, LogLevel::Info,
                                "schema mismatch (replica={}, master={})",
                                detail::fingerprint_hex(msg.schema_version),
                                detail::fingerprint_hex(my_sv));
                    return {tagged(Message{ErrorMsg{ErrorCode::SchemaMismatch,
                        "schema mismatch: replica=" +
                        detail::fingerprint_hex(msg.schema_version) +
                        " master=" + detail::fingerprint_hex(my_sv),
                        my_sv,
                        detail::get_schema_sql(db, filter())}})};
                }
            }
        }

        // Fast path: if the replica's seq is behind but within the
        // changeset queue, replay from the queue instead of diffing.
        if (msg.last_seq > 0 && msg.last_seq < seq &&
            !changeset_queue.empty() &&
            msg.last_seq >= changeset_queue.front().seq - 1) {
            std::vector<OutMessage> result;
            std::size_t replayed = 0;
            for (const auto& qc : changeset_queue) {
                if (qc.seq > msg.last_seq) {
                    result.push_back(tagged(Message{
                        ChangesetMsg{qc.seq, qc.data}}));
                    ++replayed;
                }
            }
            if (replayed > 0) {
                // Send an empty DiffReady first to transition the replica
                // from DiffBuckets to Live, then the queued changesets.
                std::vector<OutMessage> out;
                out.push_back(tagged(Message{
                    NeedBucketsMsg{}}));
                out.push_back(tagged(Message{
                    DiffReadyMsg{msg.last_seq, {}, {}}}));
                out.insert(out.end(), result.begin(), result.end());
                hs_state = HSState::Live;
                SQLPIPE_LOG(config.on_log, LogLevel::Info,
                            "converge: queue replay {} changesets (seq {}→{})",
                            replayed, msg.last_seq + 1, seq);
                return out;
            }
        }

        // Compute our own bucket hashes.
        report(DiffPhase::ComputingBuckets, {},
               0, static_cast<std::int64_t>(tracked_tables.size()));
        auto my_buckets = detail::compute_all_buckets(
            db, filter(), config.bucket_size);
        report(DiffPhase::ComputingBuckets, {},
               static_cast<std::int64_t>(tracked_tables.size()),
               static_cast<std::int64_t>(tracked_tables.size()));

        // Build lookup: (table, lo) → hash for our buckets.
        struct BucketKey {
            std::string table;
            std::int64_t lo;
            bool operator==(const BucketKey& o) const {
                return table == o.table && lo == o.lo;
            }
        };
        struct BucketKeyHash {
            std::size_t operator()(const BucketKey& k) const {
                return std::hash<std::string>{}(k.table) ^
                       (std::hash<std::int64_t>{}(k.lo) << 1);
            }
        };
        std::unordered_map<BucketKey, std::uint64_t, BucketKeyHash>
            my_bucket_map;
        std::unordered_set<BucketKey, BucketKeyHash> my_bucket_keys;
        // Track each bucket's transmitted inclusive upper bound (bucket_hi),
        // taking the max across both sides, so a differing bucket is diffed
        // across its full span even when the two sides use a different
        // bucket_size (F3).
        std::unordered_map<BucketKey, std::int64_t, BucketKeyHash> hi_map;
        auto record_hi = [&](const BucketKey& k, std::int64_t hi) {
            auto it = hi_map.find(k);
            if (it == hi_map.end() || hi > it->second) hi_map[k] = hi;
        };
        for (const auto& b : my_buckets) {
            BucketKey key{b.table, b.bucket_lo};
            my_bucket_map[key] = b.hash;
            my_bucket_keys.insert(key);
            record_hi(key, b.bucket_hi);
        }

        // Build lookup for replica's buckets.
        std::unordered_map<BucketKey, std::uint64_t, BucketKeyHash>
            their_bucket_map;
        std::unordered_set<BucketKey, BucketKeyHash> their_bucket_keys;
        for (const auto& b : msg.buckets) {
            BucketKey key{b.table, b.bucket_lo};
            their_bucket_map[key] = b.hash;
            their_bucket_keys.insert(key);
            record_hi(key, b.bucket_hi);
        }

        // Find mismatched buckets.
        NeedBucketsMsg need;

        // Buckets on master or replica (union of both key sets).
        std::unordered_set<BucketKey, BucketKeyHash> all_keys;
        all_keys.insert(my_bucket_keys.begin(), my_bucket_keys.end());
        all_keys.insert(their_bucket_keys.begin(), their_bucket_keys.end());

        for (const auto& key : all_keys) {
            auto my_it = my_bucket_map.find(key);
            auto their_it = their_bucket_map.find(key);

            bool differs = false;
            if (my_it == my_bucket_map.end() ||
                their_it == their_bucket_map.end()) {
                differs = true;  // one side has it, the other doesn't
            } else if (my_it->second != their_it->second) {
                differs = true;  // both have it, hashes differ
            }

            if (differs) {
                // Use the widest transmitted upper bound for this bucket so a
                // bucket_size mismatch never leaves part of a bucket un-diffed;
                // fall back to our own bucket span if unknown (F3).
                std::int64_t hi = key.lo + config.bucket_size - 1;
                auto hi_it = hi_map.find(key);
                if (hi_it != hi_map.end()) hi = std::max(hi, hi_it->second);
                need.ranges.push_back(
                    NeedBucketRange{key.table, key.lo, hi});
            }
        }

        report(DiffPhase::ComparingBuckets, {},
               static_cast<std::int64_t>(need.ranges.size()),
               static_cast<std::int64_t>(all_keys.size()));

        // Sort ranges for deterministic order.
        std::sort(need.ranges.begin(), need.ranges.end(),
            [](const NeedBucketRange& a, const NeedBucketRange& b) {
                if (a.table != b.table) return a.table < b.table;
                return a.lo < b.lo;
            });

        if (need.ranges.empty()) {
            // All buckets match. Skip row-hash exchange.
            hs_state = HSState::Live;
            SQLPIPE_LOG(config.on_log, LogLevel::Info, "all buckets match, entering live at seq={}", seq);
            return {tagged(Message{NeedBucketsMsg{}}),
                    tagged(Message{DiffReadyMsg{seq, {}, {}}})};
        }

        pending_ranges = need.ranges;
        hs_state = HSState::WaitRowHashes;
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "{} mismatched bucket ranges", need.ranges.size());
        return {tagged(Message{std::move(need)})};
    }

    std::vector<OutMessage> handle_row_hashes(const RowHashesMsg& msg) {
        // Accept RowHashes when we have pending ranges (from a prior
        // BucketHashes comparison). This enables convergence loop
        // without strict state gating.
        if (pending_ranges.empty()) {
            return {tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "RowHashesMsg without pending ranges"}})};
        }

        // Build map of replica's rows: table → (rowid → hash).
        std::map<std::string,
                 std::map<std::int64_t, std::uint64_t>> replica_rows;
        for (const auto& entry : msg.entries) {
            auto& tbl_map = replica_rows[entry.table];
            for (const auto& run : entry.runs) {
                for (std::int64_t i = 0; i < run.count; ++i) {
                    tbl_map[run.start_rowid + i] =
                        run.hashes[static_cast<std::size_t>(i)];
                }
            }
        }

        // Compute diff per table and build patchset + deletes.
        std::vector<Changeset> patchsets;
        std::vector<TableDeletes> deletes;

        // Group pending_ranges by table.
        std::map<std::string, std::vector<std::pair<std::int64_t, std::int64_t>>>
            table_ranges;
        for (const auto& r : pending_ranges) {
            table_ranges[r.table].push_back({r.lo, r.hi});
        }

        std::int64_t tables_done = 0;
        auto tables_total = static_cast<std::int64_t>(table_ranges.size());

        for (const auto& [table, ranges] : table_ranges) {
            report(DiffPhase::ComputingRowHashes, table,
                   tables_done, tables_total);

            std::vector<std::int64_t> insert_rowids;
            std::vector<std::int64_t> update_rowids;
            std::vector<std::int64_t> delete_rowids;

            auto replica_it = replica_rows.find(table);

            for (const auto& [lo, hi] : ranges) {
                // Compute master's row hashes for this range.
                auto master_rows = detail::compute_row_hashes(db, table, lo, hi);

                // Build replica's row hash map for this range.
                std::map<std::int64_t, std::uint64_t> rep_range;
                if (replica_it != replica_rows.end()) {
                    auto& tbl_map = replica_it->second;
                    for (auto it = tbl_map.lower_bound(lo);
                         it != tbl_map.end() && it->first <= hi; ++it) {
                        rep_range[it->first] = it->second;
                    }
                }

                // Build master's row hash map for this range.
                std::map<std::int64_t, std::uint64_t> mas_range;
                for (const auto& row : master_rows) {
                    mas_range[row.rowid] = row.hash;
                }

                // Diff.
                for (const auto& [rid, mhash] : mas_range) {
                    auto rep_it = rep_range.find(rid);
                    if (rep_it == rep_range.end()) {
                        insert_rowids.push_back(rid);
                    } else if (rep_it->second != mhash) {
                        update_rowids.push_back(rid);
                    }
                }
                for (const auto& [rid, _] : rep_range) {
                    if (mas_range.find(rid) == mas_range.end()) {
                        delete_rowids.push_back(rid);
                    }
                }
            }

            // Build INSERT patchset for insert + update rowids.
            std::vector<std::int64_t> upsert_rowids;
            upsert_rowids.reserve(insert_rowids.size() + update_rowids.size());
            upsert_rowids.insert(upsert_rowids.end(),
                                 insert_rowids.begin(), insert_rowids.end());
            upsert_rowids.insert(upsert_rowids.end(),
                                 update_rowids.begin(), update_rowids.end());

            if (!upsert_rowids.empty()) {
                report(DiffPhase::BuildingPatchset, table,
                       static_cast<std::int64_t>(upsert_rowids.size()), 0);
                auto ps = detail::build_insert_patchset(db, table, upsert_rowids);
                if (!ps.empty()) {
                    patchsets.push_back(std::move(ps));
                }
            }

            if (!delete_rowids.empty()) {
                std::sort(delete_rowids.begin(), delete_rowids.end());
                deletes.push_back(TableDeletes{table, std::move(delete_rowids)});
            }

            ++tables_done;
        }

        // Combine all per-table patchsets.
        Changeset combined = detail::combine_patchsets(patchsets);

        hs_state = HSState::Live;
        pending_ranges.clear();
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "diff computed, entering live at seq={}", seq);

        return {tagged(Message{DiffReadyMsg{seq, std::move(combined), std::move(deletes)}})};
    }
};

} // namespace sqlpipe

#ifndef EMSCRIPTEN
#ifdef SQLPIPE_HAVE_POSTGRES
#include "sqlpipe_pg.cpp"
#endif
#endif

namespace sqlpipe {

// ── Public API ──────────────────────────────────────────────────────

#ifndef EMSCRIPTEN
Master::Master(PGconn* conn, MasterConfig config) {
#ifdef SQLPIPE_HAVE_POSTGRES
    impl_ = std::make_unique<Impl>();
    impl_->pg = std::make_unique<PgMasterState>(conn, std::move(config));
#else
    (void)conn;
    (void)config;
    throw Error(ErrorCode::PostgresError,
                "sqlpipe built without Postgres support (-DSQLPIPE_HAVE_POSTGRES)");
#endif
}
#endif

Master::Master(sqlite3* db, MasterConfig config)
    : impl_(std::make_unique<Impl>()) {
    impl_->db = db;
    impl_->config = config;
    impl_->init();
}

Master::~Master() {
    if (impl_ && impl_->db && impl_->config.on_flush) {
        sqlite3_commit_hook(impl_->db, nullptr, nullptr);
    }
}
Master::Master(Master&&) noexcept = default;
Master& Master::operator=(Master&&) noexcept = default;

void Master::exec(const std::string& sql) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        impl_->pg->exec(sql);
        return;
    }
#endif
#endif
    detail::exec(impl_->db, sql.c_str());
    // Auto-flush only after a real commit: flush_pending was set by the commit
    // hook (which fires on COMMIT, not ROLLBACK), and the connection must be
    // back in autocommit mode so we never flush uncommitted rows mid-transaction
    // (F2).
    if (impl_->config.on_flush && impl_->flush_pending &&
        sqlite3_get_autocommit(impl_->db)) {
        impl_->auto_flush();
    }
}

std::vector<OutMessage> Master::flush() {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->flush();
#endif
#endif
    // If DDL ran since last flush, the tracked table set may have changed.
    auto sv = detail::compute_schema_fingerprint(impl_->db, impl_->filter());
    if (sv != impl_->cached_sv) {
        impl_->cached_sv = sv;
        impl_->scan_tables();
        impl_->recreate_session();
    }

    auto cs = impl_->extract_changeset();
    if (cs.empty()) return {};

    impl_->recreate_session();

    impl_->seq++;
    detail::write_seq(impl_->db, impl_->seq, impl_->config.seq_key);

    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug, "flushed changeset seq={} ({} bytes)", impl_->seq, cs.size());

    impl_->enqueue_changeset(impl_->seq, cs);

    return {tagged(Message{ChangesetMsg{impl_->seq, std::move(cs)}})};
}

std::vector<OutMessage> Master::handle_message(const Message& msg) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->handle_message(msg);
#endif
#endif
    return std::visit([&](const auto& m) -> std::vector<OutMessage> {
        using T = std::decay_t<decltype(m)>;

        if constexpr (std::is_same_v<T, HelloMsg>) {
            return impl_->handle_hello(m);
        }
        else if constexpr (std::is_same_v<T, BucketHashesMsg>) {
            return impl_->handle_bucket_hashes(m);
        }
        else if constexpr (std::is_same_v<T, RowHashesMsg>) {
            return impl_->handle_row_hashes(m);
        }
        else if constexpr (std::is_same_v<T, AckMsg>) {
            SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug, "replica acked seq={}", m.seq);
            return {};
        }
        else {
            return {tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "unexpected message from replica"}})};
        }
    }, msg);
}

Seq Master::current_seq() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->seq;
#endif
#endif
    return impl_->seq;
}

SchemaVersion Master::schema_version() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->fingerprint();
#endif
#endif
    return detail::compute_schema_fingerprint(impl_->db, impl_->filter());
}

} // namespace sqlpipe

// ── relational_algebra.cpp ─────────────────────────────────────

#include <liteparser.h>

namespace sqlpipe::detail {

// ── Schema map ─────────────────────────────────────────────────

/// Column metadata for RA analysis.
struct ColumnInfo {
    std::string name;
    int         index;  // 0-based position in table
};

/// Per-table schema: column name → index.
using TableSchema = std::vector<ColumnInfo>;

/// Database schema: table name → columns.
using SchemaMap = std::unordered_map<std::string, TableSchema>;

/// Build a schema map from the database.
SchemaMap build_schema_map(sqlite3* db) {
    SchemaMap schema;
    auto stmt = prepare(db,
        "SELECT name FROM sqlite_master WHERE type='table' "
        "AND name NOT LIKE '_sqlpipe_%' AND name NOT LIKE 'sqlite_%'");
    while (sqlite3_step(stmt.get()) == SQLITE_ROW) {
        std::string table(reinterpret_cast<const char*>(
            sqlite3_column_text(stmt.get(), 0)));
        std::string pragma = "PRAGMA table_info('" + table + "')";
        auto col_stmt = prepare(db, pragma.c_str());
        TableSchema cols;
        while (sqlite3_step(col_stmt.get()) == SQLITE_ROW) {
            int idx = sqlite3_column_int(col_stmt.get(), 0);
            const char* name = reinterpret_cast<const char*>(
                sqlite3_column_text(col_stmt.get(), 1));
            cols.push_back({name ? name : "", idx});
        }
        schema[table] = std::move(cols);
    }
    return schema;
}

/// Look up a column's index in a table. Returns -1 if not found.
int column_index(const SchemaMap& schema, const std::string& table,
                 const std::string& column) {
    auto it = schema.find(table);
    if (it == schema.end()) return -1;
    for (const auto& ci : it->second) {
        if (ci.name == column) return ci.index;
    }
    return -1;
}

// ── Relational algebra nodes ───────────────────────────────────

struct RANode {
    enum Kind { Scan, Filter, Project, Join, Aggregate, SetOp };
    Kind kind;

    virtual ~RANode() = default;

    /// Collect all table names referenced by this subtree.
    virtual void collect_tables(std::set<std::string>& out) const = 0;
};

using RAPtr = std::unique_ptr<RANode>;

/// Comparison operator for predicate checks.
enum class CheckOp : std::uint8_t {
    Eq,         // column = value
    Ne,         // column != value
    Lt,         // column < value
    Le,         // column <= value
    Gt,         // column > value
    Ge,         // column >= value
    IsNull,     // column IS NULL
    IsNotNull,  // column IS NOT NULL
    InList,     // column IN (v1, v2, ...)
};

/// A resolved predicate: table.column <op> literal value(s).
struct ResolvedPredicate {
    std::string        table;
    std::string        column;
    int                column_index;  // position in table schema (-1 if unknown)
    CheckOp            op = CheckOp::Eq;
    Value              value;    // for Eq/Ne/Lt/Le/Gt/Ge
    std::vector<Value> values;   // for InList
    bool               negated = false;  // emit Not after the check
};

/// A resolved column reference: (table, column_index).
struct ColumnRef {
    std::string table;
    int         column_index;
    bool operator<(const ColumnRef& o) const {
        if (table != o.table) return table < o.table;
        return column_index < o.column_index;
    }
    bool operator==(const ColumnRef& o) const {
        return table == o.table && column_index == o.column_index;
    }
};

/// Set of column references read by an RA subtree.
using ColumnRefSet = std::set<ColumnRef>;

struct RAScan : RANode {
    std::string table;
    RAScan(std::string t) : table(std::move(t)) { kind = Scan; }
    void collect_tables(std::set<std::string>& out) const override {
        out.insert(table);
    }
};

struct RAFilter : RANode {
    RAPtr child;
    /// Equality predicates extractable from this filter.
    std::vector<ResolvedPredicate> predicates;
    /// True if the filter contains terms we couldn't analyze.
    /// When true, we can still use extracted predicates but must
    /// conservatively assume non-analyzed terms could match anything.
    bool has_opaque_terms = false;
    /// All column references in the filter expression (including
    /// columns in predicates and opaque terms).
    ColumnRefSet columns_read;

    RAFilter(RAPtr c) : child(std::move(c)) { kind = Filter; }
    void collect_tables(std::set<std::string>& out) const override {
        child->collect_tables(out);
    }
};

struct RAProject : RANode {
    RAPtr child;
    /// Column references in the SELECT list.
    ColumnRefSet columns_read;

    RAProject(RAPtr c) : child(std::move(c)) { kind = Project; }
    void collect_tables(std::set<std::string>& out) const override {
        child->collect_tables(out);
    }
};

/// A column equivalence from an equijoin ON clause:
/// left_table.left_column = right_table.right_column.
struct JoinEquality {
    std::string left_table;
    std::string left_column;
    int         left_index;    // column position (-1 if unknown)
    std::string right_table;
    std::string right_column;
    int         right_index;   // column position (-1 if unknown)
};

struct RAJoin : RANode {
    RAPtr left, right;
    /// Equijoin conditions extracted from the ON clause.
    std::vector<JoinEquality> equalities;
    /// All column references in ON expressions.
    ColumnRefSet columns_read;

    RAJoin(RAPtr l, RAPtr r) : left(std::move(l)), right(std::move(r)) {
        kind = Join;
    }
    void collect_tables(std::set<std::string>& out) const override {
        left->collect_tables(out);
        right->collect_tables(out);
    }
};

struct RAAggregate : RANode {
    RAPtr child;
    /// Column references in GROUP BY / aggregate expressions.
    ColumnRefSet columns_read;

    RAAggregate(RAPtr c) : child(std::move(c)) { kind = Aggregate; }
    void collect_tables(std::set<std::string>& out) const override {
        child->collect_tables(out);
    }
};

struct RASetOp : RANode {
    RAPtr left, right;
    RASetOp(RAPtr l, RAPtr r) : left(std::move(l)), right(std::move(r)) {
        kind = SetOp;
    }
    void collect_tables(std::set<std::string>& out) const override {
        left->collect_tables(out);
        right->collect_tables(out);
    }
};

// ── Alias resolution scope ─────────────────────────────────────

/// Maps alias → real table name within a FROM clause scope.
using AliasMap = std::unordered_map<std::string, std::string>;

/// Resolve a possibly-aliased table reference to the real table name.
std::string resolve_table(const AliasMap& aliases, const std::string& name) {
    auto it = aliases.find(name);
    return (it != aliases.end()) ? it->second : name;
}

/// Resolve an unqualified column name to a table. Returns empty string
/// if ambiguous or not found.
std::string resolve_unqualified_column(
        const SchemaMap& schema, const AliasMap& aliases,
        const std::string& column) {
    std::string found;
    // Check all tables in scope (values of alias map + any direct table refs).
    std::set<std::string> tables_in_scope;
    for (const auto& [alias, table] : aliases) {
        tables_in_scope.insert(table);
    }
    for (const auto& table : tables_in_scope) {
        auto it = schema.find(table);
        if (it == schema.end()) continue;
        for (const auto& ci : it->second) {
            if (ci.name == column) {
                if (!found.empty() && found != table) return {};  // ambiguous
                found = table;
            }
        }
    }
    return found;
}

// ── AST → RA transform ────────────────────────────────────────

/// Convert a liteparser expression literal to a sqlpipe::Value.
Value lp_literal_to_value(const LpNode* node) {
    switch (node->kind) {
        case LP_EXPR_LITERAL_INT:
            return static_cast<std::int64_t>(
                std::strtoll(node->u.literal.value, nullptr, 10));
        case LP_EXPR_LITERAL_FLOAT:
            return std::strtod(node->u.literal.value, nullptr);
        case LP_EXPR_LITERAL_STRING:
            return std::string(node->u.literal.value);
        case LP_EXPR_LITERAL_NULL:
            return std::monostate{};
        case LP_EXPR_LITERAL_BLOB: {
            // Blob literals: X'hex...'
            const char* hex = node->u.literal.value;
            std::vector<std::uint8_t> blob;
            size_t len = std::strlen(hex);
            blob.reserve(len / 2);
            for (size_t i = 0; i + 1 < len; i += 2) {
                char buf[3] = {hex[i], hex[i+1], 0};
                blob.push_back(
                    static_cast<std::uint8_t>(std::strtoul(buf, nullptr, 16)));
            }
            return blob;
        }
        case LP_EXPR_LITERAL_BOOL:
            return static_cast<std::int64_t>(
                node->u.literal.value[0] == '1' ||
                node->u.literal.value[0] == 't' ||
                node->u.literal.value[0] == 'T' ? 1 : 0);
        default:
            return std::monostate{};
    }
}

/// Check if a liteparser node is a literal value.
bool is_literal(const LpNode* node) {
    return node && (
        node->kind == LP_EXPR_LITERAL_INT ||
        node->kind == LP_EXPR_LITERAL_FLOAT ||
        node->kind == LP_EXPR_LITERAL_STRING ||
        node->kind == LP_EXPR_LITERAL_NULL ||
        node->kind == LP_EXPR_LITERAL_BLOB ||
        node->kind == LP_EXPR_LITERAL_BOOL);
}

/// Check if a liteparser node is a column reference.
bool is_column_ref(const LpNode* node) {
    return node && node->kind == LP_EXPR_COLUMN_REF;
}

/// Extract equality predicates from a WHERE expression.
/// Walks AND-connected terms looking for `column = literal` patterns.
/// Walk an AST expression and collect all column references.
void collect_expr_columns(const LpNode* expr, const SchemaMap& schema,
                          const AliasMap& aliases, ColumnRefSet& out) {
    if (!expr) return;
    switch (expr->kind) {
        case LP_EXPR_COLUMN_REF: {
            std::string table, col;
            col = expr->u.column_ref.column;
            if (expr->u.column_ref.table) {
                table = resolve_table(aliases,
                    std::string(expr->u.column_ref.table));
            } else {
                table = resolve_unqualified_column(schema, aliases, col);
            }
            if (!table.empty()) {
                int idx = column_index(schema, table, col);
                if (idx >= 0) out.insert({table, idx});
            }
            break;
        }
        case LP_EXPR_STAR: {
            // SELECT * or table.* — all columns of the referenced table(s).
            if (expr->u.star.table) {
                std::string table = resolve_table(aliases,
                    std::string(expr->u.star.table));
                auto it = schema.find(table);
                if (it != schema.end()) {
                    for (const auto& ci : it->second) {
                        out.insert({table, ci.index});
                    }
                }
            } else {
                // Bare * — all columns of all tables in scope.
                for (const auto& [alias, table] : aliases) {
                    auto it = schema.find(table);
                    if (it != schema.end()) {
                        for (const auto& ci : it->second) {
                            out.insert({table, ci.index});
                        }
                    }
                }
            }
            break;
        }
        case LP_EXPR_BINARY_OP:
            collect_expr_columns(expr->u.binary.left, schema, aliases, out);
            collect_expr_columns(expr->u.binary.right, schema, aliases, out);
            break;
        case LP_EXPR_UNARY_OP:
            collect_expr_columns(expr->u.unary.operand, schema, aliases, out);
            break;
        case LP_EXPR_FUNCTION:
            for (int i = 0; i < expr->u.function.args.count; ++i) {
                collect_expr_columns(
                    expr->u.function.args.items[i], schema, aliases, out);
            }
            break;
        case LP_EXPR_CAST:
            collect_expr_columns(expr->u.cast.expr, schema, aliases, out);
            break;
        case LP_EXPR_BETWEEN:
            collect_expr_columns(expr->u.between.expr, schema, aliases, out);
            collect_expr_columns(expr->u.between.low, schema, aliases, out);
            collect_expr_columns(expr->u.between.high, schema, aliases, out);
            break;
        case LP_EXPR_IN:
            collect_expr_columns(expr->u.in.expr, schema, aliases, out);
            for (int i = 0; i < expr->u.in.values.count; ++i) {
                collect_expr_columns(
                    expr->u.in.values.items[i], schema, aliases, out);
            }
            break;
        case LP_EXPR_CASE:
            collect_expr_columns(expr->u.case_.operand, schema, aliases, out);
            for (int i = 0; i < expr->u.case_.when_exprs.count; ++i) {
                collect_expr_columns(
                    expr->u.case_.when_exprs.items[i], schema, aliases, out);
            }
            collect_expr_columns(expr->u.case_.else_expr, schema, aliases, out);
            break;
        case LP_EXPR_COLLATE:
            collect_expr_columns(expr->u.collate.expr, schema, aliases, out);
            break;
        case LP_EXPR_SUBQUERY:
            // Subquery — conservatively don't descend. The subquery's
            // dependencies are handled when/if we analyze it separately.
            break;
        case LP_EXPR_EXISTS:
            break;
        default:
            break;
    }
}

/// Resolve a column reference to (table, column, column_index).
/// Returns false if resolution fails.
bool resolve_column(const LpNode* col_node, const SchemaMap& schema,
                    const AliasMap& aliases, std::string& table_out,
                    std::string& col_out, int& idx_out) {
    if (!is_column_ref(col_node)) return false;
    col_out = col_node->u.column_ref.column;
    if (col_node->u.column_ref.table) {
        table_out = resolve_table(aliases,
            std::string(col_node->u.column_ref.table));
    } else {
        table_out = resolve_unqualified_column(schema, aliases, col_out);
    }
    if (table_out.empty()) return false;
    idx_out = column_index(schema, table_out, col_out);
    return true;
}

/// Map liteparser binary op to CheckOp. Returns nullopt for non-comparison ops.
std::optional<CheckOp> binop_to_checkop(LpBinOp op) {
    switch (op) {
        case LP_OP_EQ:  return CheckOp::Eq;
        case LP_OP_NE:  return CheckOp::Ne;
        case LP_OP_LT:  return CheckOp::Lt;
        case LP_OP_LE:  return CheckOp::Le;
        case LP_OP_GT:  return CheckOp::Gt;
        case LP_OP_GE:  return CheckOp::Ge;
        case LP_OP_IS:  return CheckOp::Eq;  // IS is like = for non-NULL
        case LP_OP_ISNOT: return CheckOp::Ne;
        default:        return std::nullopt;
    }
}

/// Flip a comparison op (for normalizing literal on left).
CheckOp flip_checkop(CheckOp op) {
    switch (op) {
        case CheckOp::Lt: return CheckOp::Gt;
        case CheckOp::Le: return CheckOp::Ge;
        case CheckOp::Gt: return CheckOp::Lt;
        case CheckOp::Ge: return CheckOp::Le;
        default: return op;  // Eq, Ne are symmetric
    }
}

void extract_predicates(
        const LpNode* expr,
        const SchemaMap& schema,
        const AliasMap& aliases,
        std::vector<ResolvedPredicate>& out,
        bool& has_opaque) {
    if (!expr) return;

    // AND → recurse into branches.
    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_AND) {
        extract_predicates(
            expr->u.binary.left, schema, aliases, out, has_opaque);
        extract_predicates(
            expr->u.binary.right, schema, aliases, out, has_opaque);
        return;
    }

    // OR → try to merge equalities on the same column into InList.
    // e.g., x = 1 OR x = 2 OR x = 3 → InList(x, {1, 2, 3}).
    // Nested ORs are flattened: OR(OR(a=1, a=2), a=3) → {1, 2, 3}.
    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_OR) {
        // Collect all equality leaves from the OR tree.
        struct EqLeaf { std::string table; std::string col; int idx; Value val; };
        std::vector<EqLeaf> leaves;
        bool all_eq = true;

        std::function<void(const LpNode*)> collect_or = [&](const LpNode* n) {
            if (!n) { all_eq = false; return; }
            if (n->kind == LP_EXPR_BINARY_OP && n->u.binary.op == LP_OP_OR) {
                collect_or(n->u.binary.left);
                collect_or(n->u.binary.right);
                return;
            }
            // Must be column = literal.
            if (n->kind == LP_EXPR_BINARY_OP &&
                (n->u.binary.op == LP_OP_EQ || n->u.binary.op == LP_OP_IS)) {
                const LpNode* lhs = n->u.binary.left;
                const LpNode* rhs = n->u.binary.right;
                if (is_literal(lhs) && is_column_ref(rhs)) std::swap(lhs, rhs);
                if (is_column_ref(lhs) && is_literal(rhs) &&
                    rhs->kind != LP_EXPR_LITERAL_NULL) {
                    std::string table, col;
                    int idx;
                    if (resolve_column(lhs, schema, aliases, table, col, idx)) {
                        leaves.push_back({table, col, idx,
                                          lp_literal_to_value(rhs)});
                        return;
                    }
                }
            }
            all_eq = false;
        };
        collect_or(expr);

        if (all_eq && !leaves.empty()) {
            // Check all leaves reference the same (table, column).
            bool same_col = true;
            for (size_t i = 1; i < leaves.size(); ++i) {
                if (leaves[i].table != leaves[0].table ||
                    leaves[i].col != leaves[0].col) {
                    same_col = false;
                    break;
                }
            }
            if (same_col) {
                std::vector<Value> vals;
                vals.reserve(leaves.size());
                for (auto& l : leaves) vals.push_back(std::move(l.val));
                out.push_back({leaves[0].table, leaves[0].col, leaves[0].idx,
                               CheckOp::InList, {}, std::move(vals)});
                return;
            }
        }
        // OR branches we can't merge → opaque.
        has_opaque = true;
        return;
    }

    // Comparison operators: =, !=, <, <=, >, >=, IS, IS NOT.
    if (expr->kind == LP_EXPR_BINARY_OP) {
        auto check_op = binop_to_checkop(expr->u.binary.op);
        if (check_op) {
            const LpNode* lhs = expr->u.binary.left;
            const LpNode* rhs = expr->u.binary.right;

            // IS NULL / IS NOT NULL: must check before general comparison
            // because is_literal() matches LP_EXPR_LITERAL_NULL, and
            // we need IsNull/IsNotNull opcodes (not Eq/Ne which return
            // NULL under three-valued logic).
            if (expr->u.binary.op == LP_OP_IS ||
                expr->u.binary.op == LP_OP_ISNOT) {
                // Normalize: column on left.
                if (rhs && rhs->kind == LP_EXPR_LITERAL_NULL &&
                    is_column_ref(lhs)) {
                    std::string table, col;
                    int idx;
                    if (resolve_column(lhs, schema, aliases, table, col, idx)) {
                        auto op = (expr->u.binary.op == LP_OP_IS)
                            ? CheckOp::IsNull : CheckOp::IsNotNull;
                        out.push_back({table, col, idx, op, {}, {}});
                        return;
                    }
                }
                if (lhs && lhs->kind == LP_EXPR_LITERAL_NULL &&
                    is_column_ref(rhs)) {
                    std::string table, col;
                    int idx;
                    if (resolve_column(rhs, schema, aliases, table, col, idx)) {
                        auto op = (expr->u.binary.op == LP_OP_IS)
                            ? CheckOp::IsNull : CheckOp::IsNotNull;
                        out.push_back({table, col, idx, op, {}, {}});
                        return;
                    }
                }
            }

            // General comparison: column <op> literal.
            // Normalize: column on left, literal on right.
            bool flipped = false;
            if (is_literal(lhs) && is_column_ref(rhs)) {
                std::swap(lhs, rhs);
                flipped = true;
            }

            if (is_column_ref(lhs) && is_literal(rhs) &&
                rhs->kind != LP_EXPR_LITERAL_NULL) {
                // Skip NULL literals here — they're handled above as IS/IS NOT.
                std::string table, col;
                int idx;
                if (resolve_column(lhs, schema, aliases, table, col, idx)) {
                    auto op = flipped ? flip_checkop(*check_op) : *check_op;
                    out.push_back({table, col, idx, op,
                                   lp_literal_to_value(rhs), {}});
                    return;
                }
            }
        }
    }

    // IN / NOT IN list: column [NOT] IN (val1, val2, ...).
    if (expr->kind == LP_EXPR_IN) {
        const LpNode* col_expr = expr->u.in.expr;
        // Only handle literal value lists (not subqueries).
        if (is_column_ref(col_expr) && !expr->u.in.select &&
            expr->u.in.values.count > 0) {
            std::string table, col;
            int idx;
            if (resolve_column(col_expr, schema, aliases, table, col, idx)) {
                // Check all values are literals.
                std::vector<Value> vals;
                bool all_literal = true;
                for (int i = 0; i < expr->u.in.values.count; ++i) {
                    if (is_literal(expr->u.in.values.items[i])) {
                        vals.push_back(
                            lp_literal_to_value(expr->u.in.values.items[i]));
                    } else {
                        all_literal = false;
                        break;
                    }
                }
                if (all_literal) {
                    out.push_back({table, col, idx, CheckOp::InList,
                                   {}, std::move(vals), expr->u.in.is_not != 0});
                    return;
                }
            }
        }
    }

    // BETWEEN: column BETWEEN low AND high → column >= low AND column <= high.
    // NOT BETWEEN: treated as opaque (requires OR composition in the program).
    if (expr->kind == LP_EXPR_BETWEEN && !expr->u.between.is_not) {
        const LpNode* col_expr = expr->u.between.expr;
        if (is_column_ref(col_expr) &&
            is_literal(expr->u.between.low) &&
            is_literal(expr->u.between.high)) {
            std::string table, col;
            int idx;
            if (resolve_column(col_expr, schema, aliases, table, col, idx)) {
                out.push_back({table, col, idx, CheckOp::Ge,
                               lp_literal_to_value(expr->u.between.low), {}});
                out.push_back({table, col, idx, CheckOp::Le,
                               lp_literal_to_value(expr->u.between.high), {}});
                return;
            }
        }
    }

    // ISNULL / NOTNULL unary expressions.
    // These appear as expr kind LP_EXPR_UNARY_OP with various forms.
    // liteparser may represent `x IS NULL` as a binary IS op (handled above).
    // For explicit `x ISNULL` / `x NOTNULL`, check unary forms if needed.

    // Any term we couldn't extract is opaque.
    has_opaque = true;
}

/// Extract column=column equalities from a JOIN ON expression.
void extract_join_equalities(
        const LpNode* expr,
        const SchemaMap& schema,
        const AliasMap& aliases,
        std::vector<JoinEquality>& out) {
    if (!expr) return;

    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_AND) {
        extract_join_equalities(
            expr->u.binary.left, schema, aliases, out);
        extract_join_equalities(
            expr->u.binary.right, schema, aliases, out);
        return;
    }

    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_EQ) {
        const LpNode* lhs = expr->u.binary.left;
        const LpNode* rhs = expr->u.binary.right;

        // Both sides must be column references for an equijoin.
        if (is_column_ref(lhs) && is_column_ref(rhs)) {
            std::string lt, lc, rt, rc;
            lc = lhs->u.column_ref.column;
            rc = rhs->u.column_ref.column;

            if (lhs->u.column_ref.table) {
                lt = resolve_table(aliases,
                    std::string(lhs->u.column_ref.table));
            } else {
                lt = resolve_unqualified_column(schema, aliases, lc);
            }
            if (rhs->u.column_ref.table) {
                rt = resolve_table(aliases,
                    std::string(rhs->u.column_ref.table));
            } else {
                rt = resolve_unqualified_column(schema, aliases, rc);
            }

            if (!lt.empty() && !rt.empty()) {
                int li = column_index(schema, lt, lc);
                int ri = column_index(schema, rt, rc);
                out.push_back({lt, lc, li, rt, rc, ri});
            }
        }
    }
}

/// Build equijoins from USING clause columns.
void extract_using_equalities(
        const LpNodeList& using_cols,
        const LpNode* left_from,
        const LpNode* right_from,
        const SchemaMap& schema,
        const AliasMap& aliases,
        std::vector<JoinEquality>& out) {
    // Resolve table names from the immediate FROM items.
    auto resolve_from = [&](const LpNode* from) -> std::string {
        if (from && from->kind == LP_FROM_TABLE) {
            std::string name = from->u.from_table.name;
            if (from->u.from_table.alias) {
                return resolve_table(aliases,
                    std::string(from->u.from_table.alias));
            }
            return name;
        }
        return {};
    };

    // For USING, we need the table names from both sides.
    // For nested joins, the "table" is the result of the join —
    // we'd need to search deeper. For now, handle the simple case.
    std::string lt = resolve_from(left_from);
    std::string rt = resolve_from(right_from);
    if (lt.empty() || rt.empty()) return;

    for (int i = 0; i < using_cols.count; ++i) {
        const LpNode* col_node = using_cols.items[i];
        if (!col_node) continue;
        // USING columns are column references or identifiers.
        std::string col;
        if (col_node->kind == LP_EXPR_COLUMN_REF) {
            col = col_node->u.column_ref.column;
        } else if (col_node->kind == LP_EXPR_LITERAL_STRING) {
            col = col_node->u.literal.value;
        }
        if (col.empty()) continue;

        int li = column_index(schema, lt, col);
        int ri = column_index(schema, rt, col);
        if (li >= 0 && ri >= 0) {
            out.push_back({lt, col, li, rt, col, ri});
        }
    }
}

/// Build equijoins from NATURAL join by finding matching column names.
void extract_natural_equalities(
        const LpNode* left_from,
        const LpNode* right_from,
        const SchemaMap& schema,
        const AliasMap& aliases,
        std::vector<JoinEquality>& out) {
    auto resolve_from = [&](const LpNode* from) -> std::string {
        if (from && from->kind == LP_FROM_TABLE) {
            if (from->u.from_table.alias) {
                return resolve_table(aliases,
                    std::string(from->u.from_table.alias));
            }
            return std::string(from->u.from_table.name);
        }
        return {};
    };

    std::string lt = resolve_from(left_from);
    std::string rt = resolve_from(right_from);
    if (lt.empty() || rt.empty()) return;

    auto lit = schema.find(lt);
    auto rit = schema.find(rt);
    if (lit == schema.end() || rit == schema.end()) return;

    // Find columns present in both tables.
    for (const auto& lci : lit->second) {
        for (const auto& rci : rit->second) {
            if (lci.name == rci.name) {
                out.push_back({lt, lci.name, lci.index,
                               rt, rci.name, rci.index});
            }
        }
    }
}

/// Build the RA tree for a FROM clause (table refs and joins).
RAPtr build_from(const LpNode* from, const SchemaMap& schema,
                 AliasMap& aliases) {
    if (!from) return nullptr;

    if (from->kind == LP_FROM_TABLE) {
        std::string table = from->u.from_table.name;
        std::string alias = from->u.from_table.alias
            ? from->u.from_table.alias : table;
        aliases[alias] = table;
        return std::make_unique<RAScan>(table);
    }

    if (from->kind == LP_FROM_SUBQUERY) {
        // Subqueries in FROM: conservatively collect tables from the
        // inner select. We don't push predicates into subqueries.
        // For now, treat as opaque — the outer query depends on
        // all tables the subquery references.
        // TODO: recurse into subquery for deeper analysis.
        return nullptr;
    }

    if (from->kind == LP_JOIN_CLAUSE) {
        auto left = build_from(from->u.join.left, schema, aliases);
        auto right = build_from(from->u.join.right, schema, aliases);
        if (left && right) {
            auto join = std::make_unique<RAJoin>(
                std::move(left), std::move(right));

            // Extract equijoin conditions and column refs from ON clause.
            if (from->u.join.on_expr) {
                extract_join_equalities(
                    from->u.join.on_expr, schema, aliases,
                    join->equalities);
                collect_expr_columns(
                    from->u.join.on_expr, schema, aliases,
                    join->columns_read);
            }

            // USING clause → explicit equijoins.
            if (from->u.join.using_columns.count > 0) {
                extract_using_equalities(
                    from->u.join.using_columns,
                    from->u.join.left, from->u.join.right,
                    schema, aliases, join->equalities);
            }

            // NATURAL join → equijoins on all matching column names.
            if (from->u.join.join_type & LP_JOIN_NATURAL) {
                extract_natural_equalities(
                    from->u.join.left, from->u.join.right,
                    schema, aliases, join->equalities);
            }

            return join;
        }
        return left ? std::move(left) : std::move(right);
    }

    return nullptr;
}

/// Extract implicit equijoin conditions from a WHERE expression.
/// Finds column=column predicates where the two columns belong to
/// different tables (comma-join style). These are added to the
/// nearest enclosing Join node.
void extract_implicit_equijoins(
        const LpNode* expr,
        const SchemaMap& schema,
        const AliasMap& aliases,
        std::vector<JoinEquality>& equijoins) {
    if (!expr) return;

    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_AND) {
        extract_implicit_equijoins(
            expr->u.binary.left, schema, aliases, equijoins);
        extract_implicit_equijoins(
            expr->u.binary.right, schema, aliases, equijoins);
        return;
    }

    if (expr->kind == LP_EXPR_BINARY_OP && expr->u.binary.op == LP_OP_EQ) {
        const LpNode* lhs = expr->u.binary.left;
        const LpNode* rhs = expr->u.binary.right;
        if (is_column_ref(lhs) && is_column_ref(rhs)) {
            std::string lt, lc, rt, rc;
            lc = lhs->u.column_ref.column;
            rc = rhs->u.column_ref.column;

            if (lhs->u.column_ref.table) {
                lt = resolve_table(aliases,
                    std::string(lhs->u.column_ref.table));
            } else {
                lt = resolve_unqualified_column(schema, aliases, lc);
            }
            if (rhs->u.column_ref.table) {
                rt = resolve_table(aliases,
                    std::string(rhs->u.column_ref.table));
            } else {
                rt = resolve_unqualified_column(schema, aliases, rc);
            }

            // Only capture cross-table equalities (implicit joins).
            if (!lt.empty() && !rt.empty() && lt != rt) {
                int li = column_index(schema, lt, lc);
                int ri = column_index(schema, rt, rc);
                equijoins.push_back({lt, lc, li, rt, rc, ri});
            }
        }
    }
}

// ── RA optimization passes ─────────────────────────────────────

/// Collect the set of tables reachable from an RA subtree.
std::set<std::string> tables_of(const RANode* node) {
    std::set<std::string> t;
    if (node) node->collect_tables(t);
    return t;
}

/// Check if a predicate's table is reachable from an RA subtree.
bool predicate_applies_to(const ResolvedPredicate& pred, const RANode* node) {
    auto t = tables_of(node);
    return t.count(pred.table) > 0;
}

/// Push Filter predicates down through the RA tree toward Scan nodes.
/// Returns the optimized tree. Modifies the tree in place where possible.
RAPtr push_filters_down(RAPtr node) {
    if (!node) return nullptr;

    switch (node->kind) {
        case RANode::Filter: {
            auto* f = static_cast<RAFilter*>(node.get());
            // First, recursively optimize the child.
            f->child = push_filters_down(std::move(f->child));

            // If child is a Join, try to push predicates into the
            // join's left or right subtree.
            if (f->child && f->child->kind == RANode::Join) {
                auto* j = static_cast<RAJoin*>(f->child.get());
                std::vector<ResolvedPredicate> remaining;

                for (auto& pred : f->predicates) {
                    bool pushed = false;
                    // Can push to left if predicate's table is in left subtree.
                    if (predicate_applies_to(pred, j->left.get()) &&
                        !predicate_applies_to(pred, j->right.get())) {
                        // Wrap left in a Filter with this predicate.
                        auto lf = std::make_unique<RAFilter>(std::move(j->left));
                        lf->predicates.push_back(std::move(pred));
                        j->left = push_filters_down(std::move(lf));
                        pushed = true;
                    }
                    // Can push to right if predicate's table is in right subtree.
                    else if (predicate_applies_to(pred, j->right.get()) &&
                             !predicate_applies_to(pred, j->left.get())) {
                        auto rf = std::make_unique<RAFilter>(std::move(j->right));
                        rf->predicates.push_back(std::move(pred));
                        j->right = push_filters_down(std::move(rf));
                        pushed = true;
                    }

                    if (!pushed) {
                        remaining.push_back(std::move(pred));
                    }
                }

                f->predicates = std::move(remaining);

                // If all predicates were pushed down, elide the empty Filter.
                if (f->predicates.empty() && !f->has_opaque_terms) {
                    return std::move(f->child);
                }
            }

            // If child is a Project, push through it (project doesn't filter).
            if (f->child && f->child->kind == RANode::Project) {
                auto* p = static_cast<RAProject*>(f->child.get());
                // Move filter below project: Filter(Project(x)) → Project(Filter(x))
                auto inner = std::move(p->child);
                auto new_filter = std::make_unique<RAFilter>(std::move(inner));
                new_filter->predicates = std::move(f->predicates);
                new_filter->has_opaque_terms = f->has_opaque_terms;
                p->child = push_filters_down(std::move(new_filter));
                return std::move(f->child);
            }

            return std::move(node);
        }

        case RANode::Join: {
            auto* j = static_cast<RAJoin*>(node.get());
            j->left = push_filters_down(std::move(j->left));
            j->right = push_filters_down(std::move(j->right));
            return std::move(node);
        }

        case RANode::Project: {
            auto* p = static_cast<RAProject*>(node.get());
            p->child = push_filters_down(std::move(p->child));
            return std::move(node);
        }

        case RANode::Aggregate: {
            auto* a = static_cast<RAAggregate*>(node.get());
            a->child = push_filters_down(std::move(a->child));
            return std::move(node);
        }

        case RANode::SetOp: {
            auto* s = static_cast<RASetOp*>(node.get());
            s->left = push_filters_down(std::move(s->left));
            s->right = push_filters_down(std::move(s->right));
            return std::move(node);
        }

        default:
            return std::move(node);
    }
}

/// Create per-predicate Filter nodes from a Filter with multiple predicates.
/// Filter({a.x=1, b.y=2}, child) → Filter(a.x=1, Filter(b.y=2, child))
RAPtr split_filters(RAPtr node) {
    if (!node) return nullptr;

    switch (node->kind) {
        case RANode::Filter: {
            auto* f = static_cast<RAFilter*>(node.get());
            f->child = split_filters(std::move(f->child));

            if (f->predicates.size() <= 1) return std::move(node);

            // Build a chain: innermost first.
            RAPtr result = std::move(f->child);
            for (auto& pred : f->predicates) {
                auto new_f = std::make_unique<RAFilter>(std::move(result));
                new_f->predicates.push_back(std::move(pred));
                result = std::move(new_f);
            }
            // If the original had opaque terms, add an opaque filter at top.
            if (f->has_opaque_terms) {
                auto opaque = std::make_unique<RAFilter>(std::move(result));
                opaque->has_opaque_terms = true;
                result = std::move(opaque);
            }
            return result;
        }

        case RANode::Join: {
            auto* j = static_cast<RAJoin*>(node.get());
            j->left = split_filters(std::move(j->left));
            j->right = split_filters(std::move(j->right));
            return std::move(node);
        }

        case RANode::Project: {
            auto* p = static_cast<RAProject*>(node.get());
            p->child = split_filters(std::move(p->child));
            return std::move(node);
        }

        case RANode::Aggregate: {
            auto* a = static_cast<RAAggregate*>(node.get());
            a->child = split_filters(std::move(a->child));
            return std::move(node);
        }

        case RANode::SetOp: {
            auto* s = static_cast<RASetOp*>(node.get());
            s->left = split_filters(std::move(s->left));
            s->right = split_filters(std::move(s->right));
            return std::move(node);
        }

        default:
            return std::move(node);
    }
}

/// Add propagated predicates as new Filter nodes above relevant Scans.
/// For each (table, predicate) derived from equijoin propagation,
/// wraps the matching Scan in a Filter if one doesn't already exist.
RAPtr inject_propagated_predicates(
        RAPtr node,
        const std::vector<ResolvedPredicate>& propagated) {
    if (!node || propagated.empty()) return std::move(node);

    switch (node->kind) {
        case RANode::Scan: {
            auto* s = static_cast<RAScan*>(node.get());
            // Collect predicates for this table.
            std::vector<ResolvedPredicate> mine;
            for (const auto& p : propagated) {
                if (p.table == s->table) mine.push_back(p);
            }
            if (mine.empty()) return std::move(node);
            auto f = std::make_unique<RAFilter>(std::move(node));
            f->predicates = std::move(mine);
            return f;
        }

        case RANode::Filter: {
            auto* f = static_cast<RAFilter*>(node.get());
            f->child = inject_propagated_predicates(
                std::move(f->child), propagated);
            return std::move(node);
        }

        case RANode::Join: {
            auto* j = static_cast<RAJoin*>(node.get());
            j->left = inject_propagated_predicates(
                std::move(j->left), propagated);
            j->right = inject_propagated_predicates(
                std::move(j->right), propagated);
            return std::move(node);
        }

        case RANode::Project: {
            auto* p = static_cast<RAProject*>(node.get());
            p->child = inject_propagated_predicates(
                std::move(p->child), propagated);
            return std::move(node);
        }

        case RANode::Aggregate: {
            auto* a = static_cast<RAAggregate*>(node.get());
            a->child = inject_propagated_predicates(
                std::move(a->child), propagated);
            return std::move(node);
        }

        case RANode::SetOp: {
            auto* s = static_cast<RASetOp*>(node.get());
            s->left = inject_propagated_predicates(
                std::move(s->left), propagated);
            s->right = inject_propagated_predicates(
                std::move(s->right), propagated);
            return std::move(node);
        }

        default:
            return std::move(node);
    }
}

// RA-RA transforms for optimization.
RAPtr optimize_ra(RAPtr ra);

/// Transform a SELECT AST into an RA tree.
RAPtr select_to_ra(const LpNode* select, const SchemaMap& schema,
                   AliasMap& aliases) {
    if (!select || select->kind != LP_STMT_SELECT) return nullptr;

    // Build FROM (scans + joins), extracting equijoin conditions.
    RAPtr ra = build_from(select->u.select.from, schema, aliases);
    if (!ra) {
        // No FROM clause — e.g. SELECT 1. No table dependencies.
        return nullptr;
    }

    // WHERE → Filter with predicate extraction + implicit join detection.
    if (select->u.select.where) {
        auto filter = std::make_unique<RAFilter>(std::move(ra));
        extract_predicates(
            select->u.select.where, schema, aliases,
            filter->predicates, filter->has_opaque_terms);
        // Collect all column references in WHERE (including opaque terms).
        collect_expr_columns(
            select->u.select.where, schema, aliases, filter->columns_read);

        // Extract implicit equijoin conditions (column=column across tables)
        // from WHERE and add to the nearest Join node.
        if (filter->child && filter->child->kind == RANode::Join) {
            auto* j = static_cast<RAJoin*>(filter->child.get());
            extract_implicit_equijoins(
                select->u.select.where, schema, aliases,
                j->equalities);
        }

        ra = std::move(filter);
    }

    // GROUP BY → Aggregate with column tracking.
    if (select->u.select.group_by.count > 0) {
        auto agg = std::make_unique<RAAggregate>(std::move(ra));
        for (int i = 0; i < select->u.select.group_by.count; ++i) {
            collect_expr_columns(
                select->u.select.group_by.items[i], schema, aliases,
                agg->columns_read);
        }
        ra = std::move(agg);
    }

    // HAVING → another Filter (on aggregated results).
    if (select->u.select.having) {
        auto filter = std::make_unique<RAFilter>(std::move(ra));
        filter->has_opaque_terms = true;  // HAVING predicates are post-aggregate
        collect_expr_columns(
            select->u.select.having, schema, aliases, filter->columns_read);
        ra = std::move(filter);
    }

    // SELECT list → Project with column tracking.
    {
        auto proj = std::make_unique<RAProject>(std::move(ra));
        for (int i = 0; i < select->u.select.result_columns.count; ++i) {
            auto* rc = select->u.select.result_columns.items[i];
            if (rc && rc->kind == LP_RESULT_COLUMN) {
                collect_expr_columns(
                    rc->u.result_column.expr, schema, aliases,
                    proj->columns_read);
            }
        }
        // ORDER BY columns are also relevant — changing an ORDER BY column
        // can reorder results, which changes the query output.
        for (int i = 0; i < select->u.select.order_by.count; ++i) {
            auto* ot = select->u.select.order_by.items[i];
            if (ot && ot->kind == LP_ORDER_TERM) {
                collect_expr_columns(
                    ot->u.order_term.expr, schema, aliases,
                    proj->columns_read);
            }
        }
        ra = std::move(proj);
    }

    return ra;
}

/// Transform a compound SELECT (UNION/INTERSECT/EXCEPT) into an RA tree.
RAPtr compound_to_ra(const LpNode* node, const SchemaMap& schema) {
    if (node->kind == LP_STMT_SELECT) {
        AliasMap aliases;
        return select_to_ra(node, schema, aliases);
    }
    if (node->kind == LP_COMPOUND_SELECT) {
        auto left = compound_to_ra(node->u.compound.left, schema);
        auto right = compound_to_ra(node->u.compound.right, schema);
        if (left && right) {
            return std::make_unique<RASetOp>(std::move(left), std::move(right));
        }
        return left ? std::move(left) : std::move(right);
    }
    return nullptr;
}

/// Top-level: parse SQL, produce an RA tree, and optimize it.
/// Returns nullptr if the SQL can't be analyzed (not a SELECT, parse error).
RAPtr sql_to_ra(const std::string& sql, const SchemaMap& schema) {
    arena_t* arena = arena_create(4096);
    if (!arena) return nullptr;
    const char* err = nullptr;
    LpNode* ast = lp_parse(sql.c_str(), arena, &err);
    if (!ast) {
        arena_destroy(arena);
        return nullptr;
    }

    RAPtr ra;
    if (ast->kind == LP_STMT_SELECT) {
        AliasMap aliases;
        ra = select_to_ra(ast, schema, aliases);
    } else if (ast->kind == LP_COMPOUND_SELECT) {
        ra = compound_to_ra(ast, schema);
    }

    arena_destroy(arena);

    // Optimize: propagate predicates through equijoins, split, push to leaves.
    ra = optimize_ra(std::move(ra));

    return ra;
}

/// Collect all equality predicates from an RA tree (from all Filter nodes).
/// Collect all column references from the RA tree.
void collect_all_columns(const RANode* node, ColumnRefSet& out) {
    if (!node) return;
    switch (node->kind) {
        case RANode::Scan:
            break;  // Scan itself doesn't read columns; parent nodes do.
        case RANode::Filter: {
            auto* f = static_cast<const RAFilter*>(node);
            out.insert(f->columns_read.begin(), f->columns_read.end());
            // Also include predicate columns.
            for (const auto& pred : f->predicates) {
                if (pred.column_index >= 0)
                    out.insert({pred.table, pred.column_index});
            }
            collect_all_columns(f->child.get(), out);
            break;
        }
        case RANode::Project: {
            auto* p = static_cast<const RAProject*>(node);
            out.insert(p->columns_read.begin(), p->columns_read.end());
            collect_all_columns(p->child.get(), out);
            break;
        }
        case RANode::Join: {
            auto* j = static_cast<const RAJoin*>(node);
            out.insert(j->columns_read.begin(), j->columns_read.end());
            for (const auto& eq : j->equalities) {
                if (eq.left_index >= 0) out.insert({eq.left_table, eq.left_index});
                if (eq.right_index >= 0) out.insert({eq.right_table, eq.right_index});
            }
            collect_all_columns(j->left.get(), out);
            collect_all_columns(j->right.get(), out);
            break;
        }
        case RANode::Aggregate: {
            auto* a = static_cast<const RAAggregate*>(node);
            out.insert(a->columns_read.begin(), a->columns_read.end());
            collect_all_columns(a->child.get(), out);
            break;
        }
        case RANode::SetOp: {
            auto* s = static_cast<const RASetOp*>(node);
            collect_all_columns(s->left.get(), out);
            collect_all_columns(s->right.get(), out);
            break;
        }
        default:
            break;
    }
}

/// Get the set of column indices referenced by a query for a specific table.
std::set<int> columns_for_table(const RANode* ra, const std::string& table) {
    ColumnRefSet all;
    collect_all_columns(ra, all);
    std::set<int> result;
    for (const auto& cr : all) {
        if (cr.table == table) result.insert(cr.column_index);
    }
    return result;
}

void collect_predicates(const RANode* node,
                        std::vector<ResolvedPredicate>& out) {
    if (!node) return;
    switch (node->kind) {
        case RANode::Filter: {
            auto* f = static_cast<const RAFilter*>(node);
            out.insert(out.end(), f->predicates.begin(), f->predicates.end());
            collect_predicates(f->child.get(), out);
            break;
        }
        case RANode::Project:
            collect_predicates(
                static_cast<const RAProject*>(node)->child.get(), out);
            break;
        case RANode::Aggregate:
            collect_predicates(
                static_cast<const RAAggregate*>(node)->child.get(), out);
            break;
        case RANode::Join: {
            auto* j = static_cast<const RAJoin*>(node);
            collect_predicates(j->left.get(), out);
            collect_predicates(j->right.get(), out);
            break;
        }
        case RANode::SetOp: {
            auto* s = static_cast<const RASetOp*>(node);
            collect_predicates(s->left.get(), out);
            collect_predicates(s->right.get(), out);
            break;
        }
        default:
            break;
    }
}

/// Collect all equijoin conditions from an RA tree.
void collect_equijoins(const RANode* node,
                       std::vector<JoinEquality>& out) {
    if (!node) return;
    switch (node->kind) {
        case RANode::Join: {
            auto* j = static_cast<const RAJoin*>(node);
            out.insert(out.end(), j->equalities.begin(), j->equalities.end());
            collect_equijoins(j->left.get(), out);
            collect_equijoins(j->right.get(), out);
            break;
        }
        case RANode::Filter:
            collect_equijoins(
                static_cast<const RAFilter*>(node)->child.get(), out);
            break;
        case RANode::Project:
            collect_equijoins(
                static_cast<const RAProject*>(node)->child.get(), out);
            break;
        case RANode::Aggregate:
            collect_equijoins(
                static_cast<const RAAggregate*>(node)->child.get(), out);
            break;
        case RANode::SetOp: {
            auto* s = static_cast<const RASetOp*>(node);
            collect_equijoins(s->left.get(), out);
            collect_equijoins(s->right.get(), out);
            break;
        }
        default:
            break;
    }
}

/// Propagate predicates through equijoin conditions.
/// For each predicate {table.column = value} and equijoin
/// {A.col1 = B.col2}, if table.column matches one side, derive
/// a predicate for the other side.
/// Repeats until no new predicates are derived (transitive closure).
/// Check if two predicates are structurally identical.
bool predicates_match(const ResolvedPredicate& a, const ResolvedPredicate& b) {
    return a.table == b.table && a.column == b.column &&
           a.op == b.op && a.value == b.value && a.values == b.values &&
           a.negated == b.negated;
}

void propagate_predicates(
        std::vector<ResolvedPredicate>& predicates,
        const std::vector<JoinEquality>& equijoins) {
    if (equijoins.empty()) return;

    bool changed = true;
    while (changed) {
        changed = false;
        for (const auto& eq : equijoins) {
            for (size_t i = 0, n = predicates.size(); i < n; ++i) {
                const auto& pred = predicates[i];

                // Check if predicate matches the left side of the equijoin.
                if (pred.table == eq.left_table &&
                    pred.column == eq.left_column) {
                    ResolvedPredicate derived{
                        eq.right_table, eq.right_column,
                        eq.right_index, pred.op, pred.value, pred.values};
                    bool exists = false;
                    for (const auto& p : predicates) {
                        if (predicates_match(p, derived)) {
                            exists = true; break;
                        }
                    }
                    if (!exists) {
                        predicates.push_back(std::move(derived));
                        changed = true;
                    }
                }

                // Check the right side.
                if (pred.table == eq.right_table &&
                    pred.column == eq.right_column) {
                    ResolvedPredicate derived{
                        eq.left_table, eq.left_column,
                        eq.left_index, pred.op, pred.value, pred.values};
                    bool exists = false;
                    for (const auto& p : predicates) {
                        if (predicates_match(p, derived)) {
                            exists = true; break;
                        }
                    }
                    if (!exists) {
                        predicates.push_back(std::move(derived));
                        changed = true;
                    }
                }
            }
        }
    }
}

/// Run all optimization passes on an RA tree:
/// 1. Propagate predicates through equijoins
/// 2. Inject propagated predicates at leaf Scans
/// 3. Split multi-predicate Filters
/// 4. Push Filters toward leaves
RAPtr optimize_ra(RAPtr ra) {
    if (!ra) return nullptr;

    // Collect all predicates and equijoins.
    std::vector<ResolvedPredicate> predicates;
    collect_predicates(ra.get(), predicates);
    std::vector<JoinEquality> equijoins;
    collect_equijoins(ra.get(), equijoins);

    // Propagate predicates through equijoins to derive new ones.
    auto original_count = predicates.size();
    propagate_predicates(predicates, equijoins);

    // Inject any newly derived predicates at the relevant Scans.
    if (predicates.size() > original_count) {
        std::vector<ResolvedPredicate> new_preds(
            predicates.begin() + static_cast<std::ptrdiff_t>(original_count),
            predicates.end());
        ra = inject_propagated_predicates(std::move(ra), new_preds);
    }

    // Split and push filters down.
    ra = split_filters(std::move(ra));
    ra = push_filters_down(std::move(ra));

    return ra;
}

// ── Predicate VM ────────────────────────────────────────────────
//
// A small bytecode VM that evaluates subscription predicates against
// changeset rows. Compiled once at subscribe time from the RA's
// pushed-down Filter predicates. Evaluated per changeset row.
//
// The program is run twice per row — once against old values, once
// against new values. If either run returns true, the subscription
// is affected. This correctly handles AND semantics: all predicates
// must match on the SAME row state (old or new), not across states.

/// VM opcodes.
enum class VMOp : std::uint8_t {
    LoadCol,    // push column value; next byte = column index
    ConstInt,   // push int64; next 8 bytes = value (LE)
    ConstFloat, // push double; next 8 bytes = value (LE)
    ConstStr,   // push string; next 2 bytes = length (LE), then chars
    ConstNull,  // push null
    ConstBlob,  // push blob; next 4 bytes = length (LE), then bytes
    Eq,         // pop 2, push (a == b)
    Ne,         // pop 2, push (a != b)
    Lt,         // pop 2, push (a < b)
    Le,         // pop 2, push (a <= b)
    Gt,         // pop 2, push (a > b)
    Ge,         // pop 2, push (a >= b)
    IsNull,     // pop 1, push (a is null)
    IsNotNull,  // pop 1, push (a is not null)
    InList,     // next byte = count N; pop value + N items, push (value in items)
    And,        // pop 2 bools, push (a && b)
    Or,         // pop 2 bools, push (a || b)
    Not,        // pop 1 bool, push (!a)
    True,       // push true
    Halt,       // stop; result = top of stack as bool
};

/// A compiled predicate program for one (subscription, table) pair.
using Program = std::vector<std::uint8_t>;

/// Compilation: emit helpers.
void emit_op(Program& p, VMOp op) { p.push_back(static_cast<uint8_t>(op)); }
void emit_u8(Program& p, uint8_t v) { p.push_back(v); }
void emit_u16(Program& p, uint16_t v) {
    p.push_back(v & 0xFF); p.push_back((v >> 8) & 0xFF);
}
void emit_i64(Program& p, int64_t v) {
    auto u = static_cast<uint64_t>(v);
    for (int i = 0; i < 8; ++i) p.push_back((u >> (i * 8)) & 0xFF);
}
void emit_f64(Program& p, double v) {
    uint64_t u;
    std::memcpy(&u, &v, 8);
    for (int i = 0; i < 8; ++i) p.push_back((u >> (i * 8)) & 0xFF);
}

/// Emit a constant value push.
void emit_const(Program& p, const Value& val) {
    std::visit([&](const auto& v) {
        using T = std::decay_t<decltype(v)>;
        if constexpr (std::is_same_v<T, std::monostate>) {
            emit_op(p, VMOp::ConstNull);
        } else if constexpr (std::is_same_v<T, std::int64_t>) {
            emit_op(p, VMOp::ConstInt);
            emit_i64(p, v);
        } else if constexpr (std::is_same_v<T, double>) {
            emit_op(p, VMOp::ConstFloat);
            emit_f64(p, v);
        } else if constexpr (std::is_same_v<T, std::string>) {
            emit_op(p, VMOp::ConstStr);
            emit_u16(p, static_cast<uint16_t>(v.size()));
            p.insert(p.end(), v.begin(), v.end());
        } else if constexpr (std::is_same_v<T, std::vector<uint8_t>>) {
            emit_op(p, VMOp::ConstBlob);
            uint32_t len = static_cast<uint32_t>(v.size());
            for (int i = 0; i < 4; ++i)
                p.push_back((len >> (i * 8)) & 0xFF);
            p.insert(p.end(), v.begin(), v.end());
        }
    }, val);
}

/// Emit a single predicate check: [LoadCol col][const value][cmp op].
void emit_predicate(Program& p, const ResolvedPredicate& pred) {
    if (pred.column_index < 0) {
        // Unknown column → conservative: always true.
        emit_op(p, VMOp::True);
        return;
    }

    switch (pred.op) {
        case CheckOp::Eq: case CheckOp::Ne:
        case CheckOp::Lt: case CheckOp::Le:
        case CheckOp::Gt: case CheckOp::Ge:
            emit_op(p, VMOp::LoadCol);
            emit_u8(p, static_cast<uint8_t>(pred.column_index));
            emit_const(p, pred.value);
            switch (pred.op) {
                case CheckOp::Eq: emit_op(p, VMOp::Eq); break;
                case CheckOp::Ne: emit_op(p, VMOp::Ne); break;
                case CheckOp::Lt: emit_op(p, VMOp::Lt); break;
                case CheckOp::Le: emit_op(p, VMOp::Le); break;
                case CheckOp::Gt: emit_op(p, VMOp::Gt); break;
                case CheckOp::Ge: emit_op(p, VMOp::Ge); break;
                default: break;
            }
            break;
        case CheckOp::IsNull:
            emit_op(p, VMOp::LoadCol);
            emit_u8(p, static_cast<uint8_t>(pred.column_index));
            emit_op(p, VMOp::IsNull);
            break;
        case CheckOp::IsNotNull:
            emit_op(p, VMOp::LoadCol);
            emit_u8(p, static_cast<uint8_t>(pred.column_index));
            emit_op(p, VMOp::IsNotNull);
            break;
        case CheckOp::InList:
            // Push the column value, then all list values, then InList.
            emit_op(p, VMOp::LoadCol);
            emit_u8(p, static_cast<uint8_t>(pred.column_index));
            for (const auto& v : pred.values) emit_const(p, v);
            emit_op(p, VMOp::InList);
            emit_u8(p, static_cast<uint8_t>(pred.values.size()));
            break;
    }
    // Apply negation if set (e.g., NOT IN).
    if (pred.negated) {
        emit_op(p, VMOp::Not);
    }
}

/// Compile predicates for a specific table into a bytecode program.
/// All predicates for the table are AND-connected.
/// Returns an empty program if no predicates apply (= always affected).
/// Compile result: program bytecode and whether preloading is needed.
struct CompileResult {
    Program program;
    bool    needs_preload = false;
};

CompileResult compile_predicates(
        const std::vector<ResolvedPredicate>& predicates,
        const std::string& table) {
    // Collect predicates for this table.
    std::vector<const ResolvedPredicate*> mine;
    for (const auto& pred : predicates) {
        if (pred.table == table && pred.column_index >= 0) {
            mine.push_back(&pred);
        }
    }
    if (mine.empty()) return {};  // no predicates → no program

    // Check if predicates reference more than one distinct column.
    // Multi-column predicates need preloading for UPDATE changesets.
    std::set<int> pred_columns;
    for (const auto* p : mine) pred_columns.insert(p->column_index);
    bool needs_preload = pred_columns.size() > 1;

    Program prog;
    emit_predicate(prog, *mine[0]);
    for (size_t i = 1; i < mine.size(); ++i) {
        emit_predicate(prog, *mine[i]);
        emit_op(prog, VMOp::And);
    }
    emit_op(prog, VMOp::Halt);
    return {std::move(prog), needs_preload};
}

// ── VM evaluator ────────────────────────────────────────────────

/// Convert a sqlite3_value to a sqlpipe::Value.
Value sqlite3_value_to_value(sqlite3_value* v) {
    if (!v) return std::monostate{};
    switch (sqlite3_value_type(v)) {
        case SQLITE_INTEGER:
            return sqlite3_value_int64(v);
        case SQLITE_FLOAT:
            return sqlite3_value_double(v);
        case SQLITE_TEXT: {
            const char* t = reinterpret_cast<const char*>(
                sqlite3_value_text(v));
            return t ? std::string(t) : std::string{};
        }
        case SQLITE_BLOB: {
            int len = sqlite3_value_bytes(v);
            auto* p = static_cast<const std::uint8_t*>(sqlite3_value_blob(v));
            return std::vector<std::uint8_t>(p, p + len);
        }
        default:
            return std::monostate{};
    }
}

/// Compare two Values. Returns <0, 0, >0 following SQLite affinity rules.
int compare_values(const Value& a, const Value& b) {
    bool a_null = std::holds_alternative<std::monostate>(a);
    bool b_null = std::holds_alternative<std::monostate>(b);
    if (a_null && b_null) return 0;
    if (a_null) return -1;
    if (b_null) return 1;

    if (auto* ai = std::get_if<std::int64_t>(&a)) {
        if (auto* bi = std::get_if<std::int64_t>(&b))
            return (*ai < *bi) ? -1 : (*ai > *bi) ? 1 : 0;
        if (auto* bd = std::get_if<double>(&b))
            return (static_cast<double>(*ai) < *bd) ? -1 :
                   (static_cast<double>(*ai) > *bd) ? 1 : 0;
    }
    if (auto* ad = std::get_if<double>(&a)) {
        if (auto* bd = std::get_if<double>(&b))
            return (*ad < *bd) ? -1 : (*ad > *bd) ? 1 : 0;
        if (auto* bi = std::get_if<std::int64_t>(&b))
            return (*ad < static_cast<double>(*bi)) ? -1 :
                   (*ad > static_cast<double>(*bi)) ? 1 : 0;
    }
    if (auto* as = std::get_if<std::string>(&a)) {
        if (auto* bs = std::get_if<std::string>(&b))
            return as->compare(*bs);
    }
    if (auto* ab = std::get_if<std::vector<std::uint8_t>>(&a)) {
        if (auto* bb = std::get_if<std::vector<std::uint8_t>>(&b)) {
            auto minlen = std::min(ab->size(), bb->size());
            int cmp = std::memcmp(ab->data(), bb->data(), minlen);
            if (cmp != 0) return cmp;
            return (ab->size() < bb->size()) ? -1 :
                   (ab->size() > bb->size()) ? 1 : 0;
        }
    }

    auto type_rank = [](const Value& v) -> int {
        if (std::holds_alternative<std::monostate>(v)) return 0;
        if (std::holds_alternative<std::int64_t>(v)) return 1;
        if (std::holds_alternative<double>(v)) return 1;
        if (std::holds_alternative<std::string>(v)) return 2;
        return 3;
    };
    return type_rank(a) < type_rank(b) ? -1 :
           type_rank(a) > type_rank(b) ? 1 : 0;
}

/// Loaded row values for UPDATE operations where the changeset
/// doesn't carry all columns needed by multi-column predicates.
struct LoadedRow {
    std::vector<Value> old_values;
    std::vector<Value> new_values;
};

/// Load old and new row state for an UPDATE changeset entry.
/// The changeset has already been applied, so the current DB state
/// is the new row. Old state is reconstructed by overlaying changeset
/// old values (PK + changed columns) onto the current row (unchanged
/// columns are the same in old and new).
LoadedRow load_update_row(sqlite3* db, const std::string& table,
                          sqlite3_changeset_iter* iter, int ncol) {
    LoadedRow row;
    // Get rowid from changeset PK.
    sqlite3_value* pk_val = nullptr;
    sqlite3changeset_old(iter, 0, &pk_val);
    if (!pk_val) return row;
    int64_t rowid = sqlite3_value_int64(pk_val);

    // Read current row from DB (= new state).
    std::string sql = "SELECT * FROM \"" + table + "\" WHERE rowid = ?";
    sqlite3_stmt* stmt = nullptr;
    if (sqlite3_prepare_v2(db, sql.c_str(), -1, &stmt, nullptr) != SQLITE_OK) {
        if (stmt) sqlite3_finalize(stmt);
        return row;
    }
    sqlite3_bind_int64(stmt, 1, rowid);

    row.new_values.resize(static_cast<size_t>(ncol));
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        for (int i = 0; i < ncol; ++i) {
            row.new_values[static_cast<size_t>(i)] =
                sqlite3_value_to_value(sqlite3_column_value(stmt, i));
        }
    }
    sqlite3_finalize(stmt);

    // Old = new, then overlay changeset old values for changed columns.
    row.old_values = row.new_values;
    for (int i = 0; i < ncol; ++i) {
        sqlite3_value* val = nullptr;
        sqlite3changeset_old(iter, i, &val);
        if (val) {
            row.old_values[static_cast<size_t>(i)] =
                sqlite3_value_to_value(val);
        }
    }
    return row;
}

/// VM execution context: provides column values for LoadCol.
struct VMContext {
    sqlite3_changeset_iter* iter;
    int op;   // SQLITE_INSERT, SQLITE_UPDATE, or SQLITE_DELETE
    bool use_new;  // true = LoadCol reads new values, false = old values

    /// Loaded full row for UPDATE with multi-column predicates.
    /// When set, LoadCol reads from here instead of the changeset.
    const LoadedRow* loaded = nullptr;
};

/// Read a LE uint16 from program at position pc. Advances pc.
uint16_t read_u16(const Program& p, size_t& pc) {
    uint16_t v = p[pc] | (p[pc+1] << 8);
    pc += 2;
    return v;
}

/// Read a LE int64 from program at position pc. Advances pc.
int64_t read_i64(const Program& p, size_t& pc) {
    uint64_t v = 0;
    for (int i = 0; i < 8; ++i) v |= static_cast<uint64_t>(p[pc+i]) << (i*8);
    pc += 8;
    return static_cast<int64_t>(v);
}

/// Read a LE double from program at position pc. Advances pc.
double read_f64(const Program& p, size_t& pc) {
    uint64_t v = 0;
    for (int i = 0; i < 8; ++i) v |= static_cast<uint64_t>(p[pc+i]) << (i*8);
    pc += 8;
    double d;
    std::memcpy(&d, &v, 8);
    return d;
}

/// Run a program against a changeset row context. Returns true if matched.
bool vm_run(const Program& prog, VMContext& ctx) {
    if (prog.empty()) return true;  // no program → conservative match

    std::vector<Value> stack;
    stack.reserve(8);
    size_t pc = 0;

    while (pc < prog.size()) {
        auto op = static_cast<VMOp>(prog[pc++]);
        switch (op) {
            case VMOp::LoadCol: {
                uint8_t col = prog[pc++];

                // If we have a loaded full row, use it directly.
                if (ctx.loaded) {
                    const auto& row = ctx.use_new
                        ? ctx.loaded->new_values
                        : ctx.loaded->old_values;
                    stack.push_back(col < row.size()
                        ? row[col] : Value{std::monostate{}});
                    break;
                }

                // Read directly from changeset.
                sqlite3_value* val = nullptr;
                if (ctx.op == SQLITE_INSERT) {
                    sqlite3changeset_new(ctx.iter, col, &val);
                } else if (ctx.op == SQLITE_DELETE) {
                    sqlite3changeset_old(ctx.iter, col, &val);
                } else if (ctx.op == SQLITE_UPDATE) {
                    // Single-column predicate path: try preferred direction,
                    // fall back to other. If neither has a value, bail.
                    if (ctx.use_new) {
                        sqlite3changeset_new(ctx.iter, col, &val);
                        if (!val) sqlite3changeset_old(ctx.iter, col, &val);
                    } else {
                        sqlite3changeset_old(ctx.iter, col, &val);
                        if (!val) sqlite3changeset_new(ctx.iter, col, &val);
                    }
                    if (!val) return true;  // conservative
                }
                stack.push_back(sqlite3_value_to_value(val));
                break;
            }
            case VMOp::ConstInt:
                stack.push_back(read_i64(prog, pc));
                break;
            case VMOp::ConstFloat:
                stack.push_back(read_f64(prog, pc));
                break;
            case VMOp::ConstStr: {
                uint16_t len = read_u16(prog, pc);
                stack.push_back(std::string(
                    reinterpret_cast<const char*>(&prog[pc]), len));
                pc += len;
                break;
            }
            case VMOp::ConstNull:
                stack.push_back(std::monostate{});
                break;
            case VMOp::ConstBlob: {
                uint32_t len = 0;
                for (int i = 0; i < 4; ++i)
                    len |= static_cast<uint32_t>(prog[pc+i]) << (i*8);
                pc += 4;
                stack.push_back(std::vector<uint8_t>(&prog[pc], &prog[pc+len]));
                pc += len;
                break;
            }
            case VMOp::Eq: case VMOp::Ne:
            case VMOp::Lt: case VMOp::Le:
            case VMOp::Gt: case VMOp::Ge: {
                auto b = stack.back(); stack.pop_back();
                auto a = stack.back(); stack.pop_back();
                // SQL: any comparison with NULL yields NULL.
                bool a_null = std::holds_alternative<std::monostate>(a);
                bool b_null = std::holds_alternative<std::monostate>(b);
                if (a_null || b_null) {
                    stack.push_back(std::monostate{});
                    break;
                }
                int cmp = compare_values(a, b);
                bool result;
                switch (op) {
                    case VMOp::Eq: result = (a == b); break;
                    case VMOp::Ne: result = (a != b); break;
                    case VMOp::Lt: result = cmp < 0; break;
                    case VMOp::Le: result = cmp <= 0; break;
                    case VMOp::Gt: result = cmp > 0; break;
                    case VMOp::Ge: result = cmp >= 0; break;
                    default: result = true; break;
                }
                stack.push_back(result ? std::int64_t{1} : std::int64_t{0});
                break;
            }
            case VMOp::IsNull: {
                auto v = stack.back(); stack.pop_back();
                stack.push_back(std::holds_alternative<std::monostate>(v)
                    ? std::int64_t{1} : std::int64_t{0});
                break;
            }
            case VMOp::IsNotNull: {
                auto v = stack.back(); stack.pop_back();
                stack.push_back(!std::holds_alternative<std::monostate>(v)
                    ? std::int64_t{1} : std::int64_t{0});
                break;
            }
            case VMOp::InList: {
                uint8_t count = prog[pc++];
                // Stack: [... value, list_item_0, ..., list_item_(count-1)]
                std::vector<Value> items(
                    stack.end() - count, stack.end());
                stack.resize(stack.size() - count);
                auto val = stack.back(); stack.pop_back();
                // SQL IN: NULL value → NULL. NULL in list → NULL if no match.
                if (std::holds_alternative<std::monostate>(val)) {
                    stack.push_back(std::monostate{});
                } else {
                    bool found = false;
                    bool has_null_item = false;
                    for (const auto& item : items) {
                        if (std::holds_alternative<std::monostate>(item)) {
                            has_null_item = true;
                        } else if (val == item) {
                            found = true; break;
                        }
                    }
                    if (found) {
                        stack.push_back(std::int64_t{1});
                    } else if (has_null_item) {
                        stack.push_back(std::monostate{});
                    } else {
                        stack.push_back(std::int64_t{0});
                    }
                }
                break;
            }
            case VMOp::And: {
                // SQL three-valued AND:
                // false AND _ = false, _ AND false = false
                // true AND true = true
                // NULL AND true = NULL, true AND NULL = NULL
                // NULL AND NULL = NULL
                auto bv = stack.back(); stack.pop_back();
                auto av = stack.back(); stack.pop_back();
                bool a_null = std::holds_alternative<std::monostate>(av);
                bool b_null = std::holds_alternative<std::monostate>(bv);
                if (!a_null && std::get<std::int64_t>(av) == 0) {
                    stack.push_back(std::int64_t{0});  // false AND _ = false
                } else if (!b_null && std::get<std::int64_t>(bv) == 0) {
                    stack.push_back(std::int64_t{0});  // _ AND false = false
                } else if (a_null || b_null) {
                    stack.push_back(std::monostate{});  // NULL involved = NULL
                } else {
                    stack.push_back(std::int64_t{1});   // true AND true = true
                }
                break;
            }
            case VMOp::Or: {
                // SQL three-valued OR:
                // true OR _ = true, _ OR true = true
                // false OR false = false
                // NULL OR false = NULL, false OR NULL = NULL
                // NULL OR NULL = NULL
                auto bv = stack.back(); stack.pop_back();
                auto av = stack.back(); stack.pop_back();
                bool a_null = std::holds_alternative<std::monostate>(av);
                bool b_null = std::holds_alternative<std::monostate>(bv);
                if (!a_null && std::get<std::int64_t>(av) != 0) {
                    stack.push_back(std::int64_t{1});  // true OR _ = true
                } else if (!b_null && std::get<std::int64_t>(bv) != 0) {
                    stack.push_back(std::int64_t{1});  // _ OR true = true
                } else if (a_null || b_null) {
                    stack.push_back(std::monostate{});  // NULL involved = NULL
                } else {
                    stack.push_back(std::int64_t{0});   // false OR false = false
                }
                break;
            }
            case VMOp::Not: {
                auto v = stack.back(); stack.pop_back();
                if (std::holds_alternative<std::monostate>(v)) {
                    stack.push_back(std::monostate{});  // NOT NULL = NULL
                } else {
                    auto a = std::get<std::int64_t>(v);
                    stack.push_back(std::int64_t{a ? 0 : 1});
                }
                break;
            }
            case VMOp::True:
                stack.push_back(std::int64_t{1});
                break;
            case VMOp::Halt:
                goto done;
        }
    }
done:
    if (stack.empty()) return true;  // empty program → conservative
    return std::holds_alternative<std::int64_t>(stack.back()) &&
           std::get<std::int64_t>(stack.back()) != 0;
}

// ── Predicate index using VM programs ───────────────────────────

/// Per-(subscription, table) compiled program + relevant column set.
struct SubProgram {
    SubscriptionId sub_id;
    Program        program;       // empty = no predicates for this table → always affected
    std::set<int>  columns_used;  // columns the query reads from this table
                                  // empty = unknown → conservative (all columns relevant)
    bool needs_preload = false;   // true if program has multi-column predicates
};

/// The predicate index: maps table → list of compiled programs.
using ProgramIndex = std::unordered_map<std::string, std::vector<SubProgram>>;

/// Evaluate a changeset against compiled VM programs.
/// Runs each program twice (old values, new values) per changeset row.
/// Returns the set of subscription IDs affected.
std::set<SubscriptionId> evaluate_changeset(
        sqlite3* db,
        const Changeset& data,
        const ProgramIndex& index,
        const std::unordered_map<std::string, std::set<SubscriptionId>>& table_subs) {
    std::set<SubscriptionId> affected;
    if (data.empty()) return affected;

    sqlite3_changeset_iter* iter = nullptr;
    int rc = sqlite3changeset_start(&iter,
        static_cast<int>(data.size()),
        const_cast<void*>(static_cast<const void*>(data.data())));
    if (rc != SQLITE_OK) {
        for (const auto& [_, subs] : table_subs) {
            affected.insert(subs.begin(), subs.end());
        }
        return affected;
    }

    while (sqlite3changeset_next(iter) == SQLITE_ROW) {
        const char* tbl = nullptr;
        int ncol = 0, cs_op = 0, indirect = 0;
        sqlite3changeset_op(iter, &tbl, &ncol, &cs_op, &indirect);
        if (!tbl) continue;

        std::string table_name(tbl);
        auto idx_it = index.find(table_name);

        // Subscriptions with no program for this table → check table_subs.
        auto ts_it = table_subs.find(table_name);
        if (ts_it == table_subs.end()) continue;

        if (idx_it == index.end()) {
            // No programs at all for this table → all subs are affected.
            for (auto sub_id : ts_it->second) {
                affected.insert(sub_id);
            }
            continue;
        }

        // For UPDATE, determine which columns changed.
        std::set<int> changed_cols;
        if (cs_op == SQLITE_UPDATE) {
            for (int col = 0; col < ncol; ++col) {
                sqlite3_value* val = nullptr;
                sqlite3changeset_new(iter, col, &val);
                if (val) changed_cols.insert(col);
            }
        }

        // Run each subscription's program.
        for (const auto& sp : idx_it->second) {
            if (affected.count(sp.sub_id)) continue;

            // For UPDATE: if the subscription's relevant columns don't
            // overlap with the changed columns, the result can't change.
            if (cs_op == SQLITE_UPDATE && !sp.columns_used.empty()) {
                bool any_overlap = false;
                for (int col : changed_cols) {
                    if (sp.columns_used.count(col)) {
                        any_overlap = true;
                        break;
                    }
                }
                if (!any_overlap) continue;  // no relevant column changed
            }

            if (sp.program.empty()) {
                // No predicates for this table → always affected.
                affected.insert(sp.sub_id);
                continue;
            }

            VMContext ctx{iter, cs_op, false, nullptr};
            bool old_match = false, new_match = false;

            // For UPDATE with multi-column predicates, load the full
            // row from the DB to provide unchanged column values.
            LoadedRow loaded_row;
            if (cs_op == SQLITE_UPDATE && sp.needs_preload && db) {
                loaded_row = load_update_row(db, table_name, iter, ncol);
                if (!loaded_row.old_values.empty()) {
                    ctx.loaded = &loaded_row;
                }
            }

            // Run against old values (UPDATE, DELETE).
            if (cs_op == SQLITE_UPDATE || cs_op == SQLITE_DELETE) {
                ctx.use_new = false;
                old_match = vm_run(sp.program, ctx);
            }

            // Run against new values (INSERT, UPDATE).
            if (!old_match &&
                (cs_op == SQLITE_INSERT || cs_op == SQLITE_UPDATE)) {
                ctx.use_new = true;
                new_match = vm_run(sp.program, ctx);
            }

            if (old_match || new_match) {
                affected.insert(sp.sub_id);
            }
        }

        // Also check for subscriptions on this table with NO entry in
        // the program index (they have no predicates at all).
        for (auto sub_id : ts_it->second) {
            if (affected.count(sub_id)) continue;
            bool has_program = false;
            for (const auto& sp : idx_it->second) {
                if (sp.sub_id == sub_id) { has_program = true; break; }
            }
            if (!has_program) {
                affected.insert(sub_id);
            }
        }
    }

    sqlite3changeset_finalize(iter);
    return affected;
}

} // namespace sqlpipe::detail

// ── query_watch.cpp ─────────────────────────────────────────────

namespace sqlpipe {

struct QueryWatch::Impl {
    sqlite3* db = nullptr;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    PGconn* pg = nullptr;
#endif
#endif
    detail::SchemaMap schema;

    struct Subscription {
        SubscriptionId               id;
        std::string                  sql;
        std::set<std::string>        tables;
        detail::RAPtr                ra;        // relational algebra tree
        std::vector<detail::ResolvedPredicate> predicates;  // extracted filters
        detail::StmtGuard            stmt;     // cached prepared statement
        std::vector<std::string>     columns;  // cached column names
        std::uint64_t                result_hash = 0;  // hash of last delivered result
    };
    std::map<SubscriptionId, Subscription> subscriptions;
    // Reverse index: table name → subscription IDs that depend on it.
    std::unordered_map<std::string, std::set<SubscriptionId>> table_subs;
    // Program index: table → compiled VM programs per subscription.
    detail::ProgramIndex prog_index;
    // Subscriptions registered since last notify — need initial evaluation.
    std::set<SubscriptionId> pending_new;
    SubscriptionId next_sub_id = 1;

    /// Rebuild the program index from all subscriptions.
    void rebuild_prog_index() {
        prog_index.clear();
        for (const auto& [id, sub] : subscriptions) {
            for (const auto& table : sub.tables) {
                auto cr = detail::compile_predicates(
                    sub.predicates, table);
                std::set<int> cols;
                if (sub.ra) {
                    cols = detail::columns_for_table(sub.ra.get(), table);
                }
                prog_index[table].push_back(
                    {id, std::move(cr.program), std::move(cols),
                     cr.needs_preload});
            }
        }
    }

    /// Analyze a subscription query using the RA transform.
    /// Returns table dependencies and extracts predicates.
    std::set<std::string> analyze_query(const std::string& sql,
                                        detail::RAPtr& ra_out,
                                        std::vector<detail::ResolvedPredicate>& preds_out) {
        // Ensure schema is current.
        schema = detail::build_schema_map(db);

        ra_out = detail::sql_to_ra(sql, schema);
        std::set<std::string> tables;

        if (ra_out) {
            ra_out->collect_tables(tables);
            // After optimization, predicates are pushed to leaf Filters.
            // Collect them all for changeset checking.
            detail::collect_predicates(ra_out.get(), preds_out);
        } else {
            // RA transform failed (non-SELECT or parse error).
            // Fall back to authorizer-based table discovery.
            sqlite3_set_authorizer(db, [](void* ctx, int action,
                    const char* a1, const char*, const char*, const char*) -> int {
                if (action == SQLITE_READ && a1) {
                    std::string name(a1);
                    if (name.compare(0, 9, "_sqlpipe_") != 0 &&
                        name.compare(0, 7, "sqlite_") != 0) {
                        static_cast<std::set<std::string>*>(ctx)->insert(name);
                    }
                }
                return SQLITE_OK;
            }, &tables);

            sqlite3_stmt* stmt = nullptr;
            sqlite3_prepare_v2(db, sql.c_str(), -1, &stmt, nullptr);
            if (stmt) sqlite3_finalize(stmt);
            sqlite3_set_authorizer(db, nullptr, nullptr);
        }
        // SQLPIPE_NESTED_DEP_UNION_BEGIN
        // Expression and nested subqueries are not always scans in the RA
        // tree. Union every table SQLite reads while preparing so a write
        // that touches only the inner table still invalidates.
        {
            std::set<std::string> extra;
            sqlite3_set_authorizer(db, [](void* ctx, int action,
                    const char* a1, const char*, const char*, const char*) -> int {
                if (action == SQLITE_READ && a1) {
                    std::string name(a1);
                    if (name.compare(0, 9, "_sqlpipe_") != 0 &&
                        name.compare(0, 7, "sqlite_") != 0) {
                        static_cast<std::set<std::string>*>(ctx)->insert(name);
                    }
                }
                return SQLITE_OK;
            }, &extra);
            sqlite3_stmt* probe = nullptr;
            sqlite3_prepare_v2(db, sql.c_str(), -1, &probe, nullptr);
            if (probe) sqlite3_finalize(probe);
            sqlite3_set_authorizer(db, nullptr, nullptr);
            tables.insert(extra.begin(), extra.end());
        }
        // SQLPIPE_NESTED_DEP_UNION_END
        return tables;
    }

#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    std::pair<QueryResult, std::uint64_t> evaluate_query_pg(Subscription& sub) {
        QueryResult result;
        result.id = sub.id;
        PGresult* r = PQexec(pg, sub.sql.c_str());
        if (PQresultStatus(r) != PGRES_TUPLES_OK) {
            std::string msg = PQresultErrorMessage(r);
            PQclear(r);
            throw Error(ErrorCode::PostgresError, "subscribe query: " + msg);
        }
        int ncols = PQnfields(r);
        if (sub.columns.empty()) {
            sub.columns.reserve(static_cast<size_t>(ncols));
            for (int i = 0; i < ncols; ++i) {
                const char* name = PQfname(r, i);
                sub.columns.push_back(name ? name : "");
            }
        }
        result.columns = sub.columns;
        std::uint64_t h = detail::kFnv64Offset;
        int nt = PQntuples(r);
        for (int i = 0; i < nt; ++i) {
            detail::fnv64_byte(h, 0xFF);
            std::vector<Value> row;
            row.reserve(static_cast<size_t>(ncols));
            for (int j = 0; j < ncols; ++j) {
                if (PQgetisnull(r, i, j)) {
                    row.emplace_back(std::monostate{});
                } else {
                    row.emplace_back(std::string(PQgetvalue(r, i, j)));
                }
                detail::hash_value(h, row.back());
            }
            result.rows.push_back(std::move(row));
        }
        PQclear(r);
        return {std::move(result), h};
    }
#endif
#endif

    // Evaluate a subscription query. Returns the result and its hash.
    std::pair<QueryResult, std::uint64_t> evaluate_query(Subscription& sub) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (pg) return evaluate_query_pg(sub);
#endif
#endif
        QueryResult result;
        result.id = sub.id;
        result.columns = sub.columns;

        std::uint64_t h = detail::kFnv64Offset;
        sqlite3_reset(sub.stmt.get());
        int ncols = static_cast<int>(sub.columns.size());
        while (true) {
            int rc = sqlite3_step(sub.stmt.get());
            if (rc == SQLITE_DONE) break;
            if (rc != SQLITE_ROW) {
                // SQLPIPE_EVAL_STEP_CHECK: do not return a partial result.
                // The caller leaves result_hash unchanged because this throws
                // before the hash is stored.
                throw Error(ErrorCode::QueryFailed,
                            std::string("subscription query: ") +
                            sqlite3_errmsg(db));
            }
            detail::fnv64_byte(h, 0xFF);  // row separator
            std::vector<Value> row;
            row.reserve(static_cast<std::size_t>(ncols));
            for (int i = 0; i < ncols; ++i) {
                row.push_back(detail::to_value(
                    sqlite3_column_value(sub.stmt.get(), i)));
                detail::hash_value(h, row.back());
            }
            result.rows.push_back(std::move(row));
        }
        return {std::move(result), h};
    }

    std::vector<QueryResult> evaluate_invalidated(
            const std::set<std::string>& affected,
            const Changeset* changeset = nullptr) {
        // Determine which subscriptions to evaluate.
        std::set<SubscriptionId> ids;

        if (changeset && !changeset->empty() && !prog_index.empty()) {
            // VM evaluation: iterate changeset once, run compiled
            // programs for each subscription.
            ids = detail::evaluate_changeset(
                db, *changeset, prog_index, table_subs);
        } else {
            // No changeset or no predicates — table-level invalidation.
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
            if (pg && affected.empty()) {
                for (const auto& [id, _] : subscriptions) ids.insert(id);
            } else
#endif
#endif
            {
            for (const auto& table : affected) {
                auto it = table_subs.find(table);
                if (it != table_subs.end()) {
                    ids.insert(it->second.begin(), it->second.end());
                }
            }
            // Fail-closed subscriptions (no extractable tables) re-eval always.
            for (const auto& [id, sub] : subscriptions) {
                if (sub.tables.empty()) ids.insert(id);
            }
            }
        }

        // Also include any newly registered subscriptions that haven't
        // been evaluated yet (their initial result delivery).
        ids.insert(pending_new.begin(), pending_new.end());
        pending_new.clear();

        std::vector<QueryResult> results;
        for (auto id : ids) {
            auto it = subscriptions.find(id);
            if (it == subscriptions.end()) continue;
            auto& sub = it->second;

            auto [result, hash] = evaluate_query(sub);
            if (hash != sub.result_hash) {
                sub.result_hash = hash;
                results.push_back(std::move(result));
            }
        }
        return results;
    }
};

QueryWatch::QueryWatch(sqlite3* db)
    : impl_(std::make_unique<Impl>()) {
    impl_->db = db;
}

#ifndef EMSCRIPTEN
QueryWatch::QueryWatch(PGconn* conn)
    : impl_(std::make_unique<Impl>()) {
#ifdef SQLPIPE_HAVE_POSTGRES
    impl_->pg = conn;
#else
    (void)conn;
    throw Error(ErrorCode::PostgresError,
                "sqlpipe built without Postgres support (-DSQLPIPE_HAVE_POSTGRES)");
#endif
}
#endif

QueryWatch::~QueryWatch() = default;
QueryWatch::QueryWatch(QueryWatch&&) noexcept = default;
QueryWatch& QueryWatch::operator=(QueryWatch&&) noexcept = default;

SubscriptionId QueryWatch::subscribe(const std::string& sql) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg && !impl_->db) {
        detail::SchemaMap empty;
        detail::RAPtr ra;
        std::vector<detail::ResolvedPredicate> predicates;
        std::set<std::string> tables;
        ra = detail::sql_to_ra(sql, empty);
        if (ra) ra->collect_tables(tables);
        // empty tables = fail closed (re-evaluate on any notify)
        auto id = impl_->next_sub_id++;
        for (const auto& t : tables)
            impl_->table_subs[t].insert(id);
        impl_->subscriptions[id] = {id, sql, std::move(tables), std::move(ra),
                                    std::move(predicates), {}, {}, 0};
        impl_->pending_new.insert(id);
        return id;
    }
#endif
#endif
    detail::RAPtr ra;
    std::vector<detail::ResolvedPredicate> predicates;
    auto tables = impl_->analyze_query(sql, ra, predicates);
    auto id = impl_->next_sub_id++;

    // Prepare statement once and cache column names.
    detail::StmtGuard stmt;
    try {
        stmt = detail::prepare(impl_->db, sql.c_str());
    } catch (const Error& e) {
        throw Error(ErrorCode::QueryFailed, e.what());
    }
    int ncols = sqlite3_column_count(stmt.get());
    std::vector<std::string> columns;
    columns.reserve(static_cast<std::size_t>(ncols));
    for (int i = 0; i < ncols; ++i) {
        const char* name = sqlite3_column_name(stmt.get(), i);
        columns.push_back(name ? name : "");
    }

    // Build reverse index entries.
    for (const auto& t : tables) {
        impl_->table_subs[t].insert(id);
    }

    // Register with hash=0 (never evaluated). The next notify() will
    // evaluate and deliver the initial result.
    impl_->subscriptions[id] = {id, sql, std::move(tables), std::move(ra),
                                std::move(predicates), std::move(stmt),
                                std::move(columns), 0};
    impl_->pending_new.insert(id);
    impl_->rebuild_prog_index();
    return id;
}

void QueryWatch::unsubscribe(SubscriptionId id) {
    auto it = impl_->subscriptions.find(id);
    if (it != impl_->subscriptions.end()) {
        // Remove reverse index entries.
        for (const auto& t : it->second.tables) {
            auto ts_it = impl_->table_subs.find(t);
            if (ts_it != impl_->table_subs.end()) {
                ts_it->second.erase(id);
                if (ts_it->second.empty()) {
                    impl_->table_subs.erase(ts_it);
                }
            }
        }
        impl_->pending_new.erase(id);
        impl_->subscriptions.erase(it);
        impl_->rebuild_prog_index();
    }
}

std::vector<QueryResult> QueryWatch::notify(
        const std::set<std::string>& affected_tables) {
    return impl_->evaluate_invalidated(affected_tables);
}

std::vector<QueryResult> QueryWatch::notify(
        const std::set<std::string>& affected_tables,
        const Changeset& changeset_data) {
    return impl_->evaluate_invalidated(affected_tables, &changeset_data);
}

bool QueryWatch::empty() const {
    return impl_->subscriptions.empty();
}

std::size_t QueryWatch::subscription_count() const {
    return impl_->subscriptions.size();
}


// ── query (one-shot) ────────────────────────────────────────────

QueryResult query(sqlite3* db, const std::string& sql) {
    auto stmt = detail::prepare(db, sql.c_str());
    int ncols = sqlite3_column_count(stmt.get());

    QueryResult result;
    result.id = 0;
    result.columns.reserve(static_cast<std::size_t>(ncols));
    for (int i = 0; i < ncols; ++i) {
        const char* name = sqlite3_column_name(stmt.get(), i);
        result.columns.push_back(name ? name : "");
    }

    while (true) {
        int rc = sqlite3_step(stmt.get());
        if (rc == SQLITE_DONE) break;
        if (rc != SQLITE_ROW) {
            throw Error(ErrorCode::QueryFailed,
                        std::string("query: ") + sqlite3_errmsg(db));
        }
        std::vector<Value> row;
        row.reserve(static_cast<std::size_t>(ncols));
        for (int i = 0; i < ncols; ++i) {
            row.push_back(detail::to_value(
                sqlite3_column_value(stmt.get(), i)));
        }
        result.rows.push_back(std::move(row));
    }
    return result;
}

} // namespace sqlpipe

// ── database.cpp ────────────────────────────────────────────────

namespace sqlpipe {

struct Database::Impl {
    sqlite3*                    db = nullptr;
    bool                        owns_db = true;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    PGconn*                     pg = nullptr;
    bool                        owns_pg = false;
#endif
#endif
    Engine                      engine = Engine::Sqlite;
    QueryWatch                  watch;
    std::map<SubscriptionId, SubscriptionCallback> callbacks;
    std::set<std::string>       dirty_tables;

    Impl(sqlite3* db_) : db(db_), watch(db_) {}
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    Impl(PGconn* conn, bool own)
        : db(nullptr), owns_db(false), pg(conn), owns_pg(own),
          engine(Engine::Postgres), watch(conn) {}
#endif
#endif

    ~Impl() {
        if (owns_db && db) sqlite3_close(db);
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (owns_pg && pg) PQfinish(pg);
#endif
#endif
    }

    void unsubscribe(SubscriptionId id) {
        watch.unsubscribe(id);
        callbacks.erase(id);
    }

    void dispatch(const std::vector<QueryResult>& results) {
        for (auto& r : results) {
            auto it = callbacks.find(r.id);
            if (it != callbacks.end()) {
                it->second(r);
            }
        }
    }

    /// Transpile SQL through sqldeep. Returns the input unchanged if it
    /// contains no sqldeep syntax (sqldeep returns NULL on plain SQL).
    static std::string transpile(const std::string& sql,
                                Engine engine = Engine::Sqlite) {
        char* err_msg = nullptr;
        int err_line = 0, err_col = 0;
        auto backend = engine == Engine::Postgres ? SQLDEEP_POSTGRES
                                                  : SQLDEEP_SQLITE;
        char* result = sqldeep_transpile_backend(sql.c_str(), backend,
                                         &err_msg, &err_line, &err_col);
        if (result) {
            std::string out(result);
            sqldeep_free(result);
            return out;
        }
        if (err_msg) {
            std::string msg(err_msg);
            sqldeep_free(err_msg);
            throw Error(ErrorCode::SqliteError,
                        msg + " (line " + std::to_string(err_line) +
                        ", col " + std::to_string(err_col) + ")");
        }
        // No result and no error means plain SQL — return as-is.
        return sql;
    }

    static void update_hook(void* ctx, int op, const char* db_name,
                            const char* table, sqlite3_int64 rowid) {
        (void)op; (void)db_name; (void)rowid;
        auto* self = static_cast<Impl*>(ctx);
        self->dirty_tables.insert(table);
    }
};

struct Subscription::Impl {
    std::weak_ptr<Database::Impl> db_impl;
    SubscriptionId id = 0;
};

Subscription::Subscription(std::unique_ptr<Impl> impl)
    : impl_(std::move(impl)) {}

Subscription::~Subscription() {
    if (!impl_) return;
    if (auto db = impl_->db_impl.lock()) {
        db->unsubscribe(impl_->id);
    }
}

Subscription::Subscription(Subscription&&) noexcept = default;
Subscription& Subscription::operator=(Subscription&&) noexcept = default;

namespace {

// Map sqlift_error_type to a stable token for crash reports / log scraping.
const char* sqlift_error_name(int err_type) {
    switch (err_type) {
    case SQLIFT_OK: return "OK";
    case SQLIFT_ERROR: return "ERROR";
    case SQLIFT_PARSE_ERROR: return "PARSE_ERROR";
    case SQLIFT_EXTRACT_ERROR: return "EXTRACT_ERROR";
    case SQLIFT_DIFF_ERROR: return "DIFF_ERROR";
    case SQLIFT_APPLY_ERROR: return "APPLY_ERROR";
    case SQLIFT_DRIFT_ERROR: return "DRIFT_ERROR";
    case SQLIFT_DESTRUCTIVE_ERROR: return "DESTRUCTIVE_ERROR";
    case SQLIFT_BREAKING_CHANGE_ERROR: return "BREAKING_CHANGE_ERROR";
    case SQLIFT_JSON_ERROR: return "JSON_ERROR";
    case SQLIFT_REBUILD_ERROR: return "REBUILD_ERROR";
    default: return "UNKNOWN";
    }
}

// Build a migration-failure message rich enough to diagnose on-device without
// re-diffing offline. Includes sqlift error class and (when provided) the
// migration plan JSON, truncated if huge so crash logs stay usable.
std::string format_sqlift_failure(const char* stage, int err_type,
                                  const char* err_msg,
                                  const char* plan_json = nullptr) {
    std::string msg;
    msg.reserve(256);
    msg.append(stage);
    msg.append(" [sqlift:");
    msg.append(sqlift_error_name(err_type));
    msg.append("] ");
    msg.append(err_msg && err_msg[0] ? err_msg : "unknown error");
    if (plan_json && plan_json[0]) {
        constexpr std::size_t kMaxPlanBytes = 8192;
        const std::size_t n = std::strlen(plan_json);
        msg.append(" | plan=");
        if (n <= kMaxPlanBytes) {
            msg.append(plan_json);
        } else {
            msg.append(plan_json, kMaxPlanBytes);
            msg.append("…[truncated, total ");
            msg.append(std::to_string(n));
            msg.append(" bytes]");
        }
    }
    return msg;
}

} // namespace

Database::Database(const std::string& path, const std::string& schema_ddl)
    : impl_(nullptr) {
    sqlite3* db = nullptr;
    int rc = sqlite3_open_v2(path.c_str(), &db,
        SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE, nullptr);
    if (rc != SQLITE_OK) {
        std::string msg = db ? sqlite3_errmsg(db) : "out of memory";
        if (db) sqlite3_close(db);
        throw Error(ErrorCode::SqliteError, "failed to open database: " + msg);
    }

    impl_ = std::make_shared<Impl>(db);

    // Register update hook to track dirty tables.
    sqlite3_update_hook(db, &Impl::update_hook, impl_.get());

    // Register sqldeep SQLite runtime functions (xml_element, xml_attrs, xml_agg,
    // sqldeep_json*).
    sqldeep_register_sqlite(db);

    // Apply schema migration if DDL provided.
    if (!schema_ddl.empty()) {
        // Current schema for the diff is *app* tables only — the same set
        // get_schema_sql / fingerprint / auto_migrate use (excludes
        // _sqlpipe_%, _sqlift_%, sqlite_%). sqlift is product-agnostic: it
        // never hears about sqlpipe meta tables. sqlpipe "preserves" them by
        // not offering them on the current side of the plan, so DropTable on
        // _sqlpipe_meta is never generated. Desired schema is pure app DDL.
        auto* sdb = sqlift_db_wrap(db);
        int err_type = 0;
        char* err_msg = nullptr;

        auto current_sql = detail::get_schema_sql(db);
        char* current_json = sqlift_parse(
            current_sql.empty() ? "" : current_sql.c_str(),
            &err_type, &err_msg);
        if (!current_json) {
            std::string error = format_sqlift_failure(
                "failed to parse current app schema", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::SqliteError, std::move(error));
        }

        // Parse the desired schema.
        char* desired_json = sqlift_parse(schema_ddl.c_str(),
                                          &err_type, &err_msg);
        if (!desired_json) {
            std::string error = format_sqlift_failure(
                "failed to parse schema DDL", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_free(current_json);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::SqliteError, std::move(error));
        }

        // Diff and apply.
        char* plan_json = sqlift_diff(current_json, desired_json,
                                      &err_type, &err_msg);
        sqlift_free(current_json);
        sqlift_free(desired_json);

        if (!plan_json) {
            std::string error = format_sqlift_failure(
                "failed to diff schemas", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::SqliteError, std::move(error));
        }

        // Allow rebuilds (e.g. column type changes) but not destructive drops.
        rc = sqlift_apply(sdb, plan_json, {.allow = SQLIFT_ALLOW_REBUILD},
                          &err_type, &err_msg);
        if (rc != 0) {
            // Keep plan_json in the exception so mobile crash reports include
            // the full op list / SQL without an offline re-diff.
            std::string error = format_sqlift_failure(
                "failed to apply migration", err_type, err_msg, plan_json);
            sqlift_free(err_msg);
            sqlift_free(plan_json);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::SqliteError, std::move(error));
        }
        sqlift_free(plan_json);
        sqlift_db_close(sdb);
    }

    detail::ensure_meta_table(db);
}

#ifndef EMSCRIPTEN
Database Database::postgres(const std::string& conninfo,
                            const std::string& schema_ddl) {
#ifdef SQLPIPE_HAVE_POSTGRES
    PGconn* conn = PQconnectdb(conninfo.c_str());
    if (!conn || PQstatus(conn) != CONNECTION_OK) {
        std::string msg = conn ? PQerrorMessage(conn) : "out of memory";
        if (conn) PQfinish(conn);
        throw Error(ErrorCode::PostgresError,
                    "failed to connect: " + msg);
    }
    Database db;
    db.impl_ = std::make_shared<Impl>(conn, true);
    {
        PGresult* r = PQexec(conn, "SET search_path TO public");
        if (PQresultStatus(r) != PGRES_COMMAND_OK) {
            std::string msg = PQresultErrorMessage(r);
            PQclear(r);
            throw Error(ErrorCode::PostgresError,
                        "SET search_path: " + msg);
        }
        PQclear(r);
    }

    if (!schema_ddl.empty()) {
        sqlift_exec ex{
            conn,
            [](void* ctx, const char* sql, char** err_msg) -> int {
                auto* c = static_cast<PGconn*>(ctx);
                PGresult* r = PQexec(c, sql);
                auto st = PQresultStatus(r);
                if (st != PGRES_COMMAND_OK && st != PGRES_TUPLES_OK) {
                    if (err_msg) *err_msg = strdup(PQresultErrorMessage(r));
                    PQclear(r);
                    return 1;
                }
                PQclear(r);
                return 0;
            },
            [](void* ctx, const char* sql,
               void (*row)(void*, int, const char* const*),
               void* row_ctx, char** err_msg) -> int {
                auto* c = static_cast<PGconn*>(ctx);
                PGresult* r = PQexec(c, sql);
                if (PQresultStatus(r) != PGRES_TUPLES_OK) {
                    if (err_msg) *err_msg = strdup(PQresultErrorMessage(r));
                    PQclear(r);
                    return 1;
                }
                int nt = PQntuples(r);
                int nc = PQnfields(r);
                std::vector<const char*> cols(static_cast<size_t>(nc));
                for (int i = 0; i < nt; ++i) {
                    for (int j = 0; j < nc; ++j)
                        cols[static_cast<size_t>(j)] =
                            PQgetisnull(r, i, j) ? nullptr : PQgetvalue(r, i, j);
                    row(row_ctx, nc, cols.data());
                }
                PQclear(r);
                return 0;
            }
        };
        int err_type = 0;
        char* err_msg = nullptr;
        sqlift_db* sdb = sqlift_db_wrap_exec(SQLIFT_ENGINE_POSTGRES, ex,
                                             &err_type, &err_msg);
        if (!sdb) {
            std::string msg = err_msg ? err_msg : "wrap_exec failed";
            sqlift_free(err_msg);
            throw Error(ErrorCode::PostgresError,
                        "sqlift_db_wrap_exec: " + msg);
        }
        char* current_json = sqlift_extract(sdb, &err_type, &err_msg);
        if (!current_json) {
            std::string error = format_sqlift_failure(
                "failed to extract current schema", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::PostgresError, std::move(error));
        }
        char* desired_json = sqlift_parse_on(sdb, schema_ddl.c_str(),
                                             &err_type, &err_msg);
        if (!desired_json) {
            std::string error = format_sqlift_failure(
                "failed to parse schema DDL", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_free(current_json);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::PostgresError, std::move(error));
        }
        char* plan_json = sqlift_diff(current_json, desired_json,
                                      &err_type, &err_msg);
        sqlift_free(current_json);
        sqlift_free(desired_json);
        if (!plan_json) {
            std::string error = format_sqlift_failure(
                "failed to diff schemas", err_type, err_msg);
            sqlift_free(err_msg);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::PostgresError, std::move(error));
        }
        int rc = sqlift_apply(sdb, plan_json,
                              {.allow = SQLIFT_ALLOW_REBUILD},
                              &err_type, &err_msg);
        if (rc != 0) {
            std::string error = format_sqlift_failure(
                "failed to apply migration", err_type, err_msg, plan_json);
            sqlift_free(err_msg);
            sqlift_free(plan_json);
            sqlift_db_close(sdb);
            throw Error(ErrorCode::PostgresError, std::move(error));
        }
        sqlift_free(plan_json);
        sqlift_db_close(sdb);
    }
    return db;
#else
    (void)conninfo;
    (void)schema_ddl;
    throw Error(ErrorCode::PostgresError,
                "sqlpipe built without Postgres support (-DSQLPIPE_HAVE_POSTGRES)");
#endif
}
#endif

Database::~Database() = default;
Database::Database(Database&&) noexcept = default;
Database& Database::operator=(Database&&) noexcept = default;

void Database::exec(const std::string& sql) {
    auto tsql = Impl::transpile(sql, impl_->engine);
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->engine == Engine::Postgres) {
        PGresult* r = PQexec(impl_->pg, tsql.c_str());
        auto st = PQresultStatus(r);
        if (st != PGRES_COMMAND_OK && st != PGRES_TUPLES_OK) {
            std::string msg = PQresultErrorMessage(r);
            PQclear(r);
            throw Error(ErrorCode::PostgresError, msg);
        }
        PQclear(r);
        if (!impl_->callbacks.empty()) {
            auto results = impl_->watch.notify(std::set<std::string>{});
            impl_->dispatch(results);
        }
        return;
    }
#endif
#endif
    impl_->dirty_tables.clear();
    auto stmt = detail::prepare(impl_->db, tsql.c_str());
    detail::step_done(impl_->db, stmt.get());

    if (!impl_->dirty_tables.empty() && !impl_->callbacks.empty()) {
        auto results = impl_->watch.notify(impl_->dirty_tables);
        impl_->dispatch(results);
        impl_->dirty_tables.clear();
    }
}

QueryResult Database::query(const std::string& sql) const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->engine == Engine::Postgres) {
        auto tsql = Impl::transpile(sql, Engine::Postgres);
        PGresult* r = PQexec(impl_->pg, tsql.c_str());
        if (PQresultStatus(r) != PGRES_TUPLES_OK) {
            std::string msg = PQresultErrorMessage(r);
            PQclear(r);
            throw Error(ErrorCode::PostgresError, msg);
        }
        QueryResult out;
        int ncols = PQnfields(r);
        for (int i = 0; i < ncols; ++i) {
            const char* name = PQfname(r, i);
            out.columns.push_back(name ? name : "");
        }
        int nt = PQntuples(r);
        for (int i = 0; i < nt; ++i) {
            std::vector<Value> row;
            for (int j = 0; j < ncols; ++j) {
                if (PQgetisnull(r, i, j))
                    row.emplace_back(std::monostate{});
                else
                    row.emplace_back(std::string(PQgetvalue(r, i, j)));
            }
            out.rows.push_back(std::move(row));
        }
        PQclear(r);
        return out;
    }
#endif
#endif
    return sqlpipe::query(impl_->db, Impl::transpile(sql));
}

Subscription Database::subscribe(const std::string& sql,
                                  SubscriptionCallback cb) {
    auto tsql = Impl::transpile(sql, impl_->engine);
    auto id = impl_->watch.subscribe(tsql);
    impl_->callbacks[id] = std::move(cb);

    std::vector<QueryResult> results;
    try {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (impl_->engine == Engine::Postgres) {
            results = impl_->watch.notify(std::set<std::string>{});
        } else
#endif
#endif
        {
            // Fire immediately with the initial result.
            auto tables = detail::get_tracked_tables(impl_->db);
            std::set<std::string> table_set(tables.begin(), tables.end());
            results = impl_->watch.notify(table_set);
        }
    } catch (...) {
        impl_->unsubscribe(id);
        throw;
    }
    for (auto& r : results) {
        if (r.id == id) {
            impl_->callbacks[id](r);
            break;
        }
    }

    auto sub_impl = std::make_unique<Subscription::Impl>();
    sub_impl->db_impl = impl_;
    sub_impl->id = id;
    return Subscription(std::move(sub_impl));
}

void Database::notify(const std::set<std::string>& affected_tables) {
    if (impl_->callbacks.empty()) return;
    auto results = impl_->watch.notify(affected_tables);
    impl_->dispatch(results);
}

void Database::notify() {
    if (impl_->callbacks.empty()) return;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->engine == Engine::Postgres) {
        notify(std::set<std::string>{});
        return;
    }
#endif
#endif
    auto tables = detail::get_tracked_tables(impl_->db);
    std::set<std::string> table_set(tables.begin(), tables.end());
    notify(table_set);
}

std::size_t Database::subscription_count() const {
    return impl_->callbacks.size();
}

sqlite3* Database::handle() const {
    return impl_->db;
}

Engine Database::engine() const {
    return impl_->engine;
}

sqlite3* Database::sqlite_handle() const {
    return impl_->db;
}

#ifndef EMSCRIPTEN
PGconn* Database::postgres_handle() const {
#ifdef SQLPIPE_HAVE_POSTGRES
    return impl_->pg;
#else
    return nullptr;
#endif
}
#endif

std::string Database::migration(const std::string& from_ddl,
                                const std::string& to_ddl) {
    int err_type;
    char* err_msg = nullptr;

    char* from_json = sqlift_parse(from_ddl.c_str(), &err_type, &err_msg);
    if (!from_json) {
        std::string error = err_msg ? err_msg : "unknown error";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError,
                    "failed to parse from_ddl: " + error);
    }

    char* to_json = sqlift_parse(to_ddl.c_str(), &err_type, &err_msg);
    if (!to_json) {
        std::string error = err_msg ? err_msg : "unknown error";
        sqlift_free(err_msg);
        sqlift_free(from_json);
        throw Error(ErrorCode::SqliteError,
                    "failed to parse to_ddl: " + error);
    }

    char* plan = sqlift_diff(from_json, to_json, &err_type, &err_msg);
    sqlift_free(from_json);
    sqlift_free(to_json);

    if (!plan) {
        std::string error = err_msg ? err_msg : "unknown error";
        sqlift_free(err_msg);
        throw Error(ErrorCode::SqliteError,
                    "failed to diff schemas: " + error);
    }

    std::string result(plan);
    sqlift_free(plan);
    return result;
}

} // namespace sqlpipe

// ── replica.cpp ─────────────────────────────────────────────────

namespace sqlpipe {

struct Replica::Impl {
    sqlite3*       db = nullptr;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    std::unique_ptr<PgReplicaState> pg;
#endif
#endif
    ReplicaConfig  config;
    Seq            seq = 0;
    Replica::State state = Replica::State::Init;
    QueryWatch     watch;
    /// Remote sequence last supplied by the master. Empty until known.
    std::optional<Seq> observed_remote_seq;

    // Prediction state.
    enum class PredictionState : uint8_t { None, Drafting, Committed };
    PredictionState prediction = PredictionState::None;
    /// Inbound messages deferred while prediction is active and
    /// config.queue_while_predicting is set.
    std::vector<Message> prediction_queue;

    Impl(sqlite3* db_, ReplicaConfig cfg)
        : db(db_), config(std::move(cfg)), watch(db_) {}
#ifndef EMSCRIPTEN
#ifdef SQLPIPE_HAVE_POSTGRES
    Impl(PGconn* conn, ReplicaConfig cfg)
        : db(nullptr), config(cfg), watch(conn) {
        pg = std::make_unique<PgReplicaState>(conn, std::move(cfg));
        config = pg->config;
    }
#endif
#endif

    const std::set<std::string>* filter() const {
        return config.table_filter ? &*config.table_filter : nullptr;
    }

    void report(DiffPhase phase, const std::string& table,
                std::int64_t done, std::int64_t total) {
        if (config.on_progress) {
            config.on_progress(DiffProgress{phase, table, done, total});
        }
    }

    void init() {
        detail::ensure_meta_table(db);
        seq = detail::read_seq(db, config.seq_key);
        // Validate tracked tables up front so an unsupported table (WITHOUT
        // ROWID, or a non-INTEGER-PRIMARY-KEY that rowid-keyed diff sync cannot
        // handle safely) is rejected at construction rather than mid-handshake
        // (F1).
        detail::get_tracked_tables(db, filter(),
            config.on_log ? &config.on_log : nullptr);
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "replica initialized at seq={}", seq);
    }

    std::vector<ChangeEvent> apply_changeset(const Changeset& data, Seq new_seq) {
        if (new_seq != seq + 1) {
            throw Error(ErrorCode::ProtocolError,
                        "changeset seq gap: expected " +
                        std::to_string(seq + 1) + ", got " +
                        std::to_string(new_seq));
        }
        // One transaction for the rows and the sequence. A failure rolls
        // both back so a partial apply is not committed and the persisted
        // sequence does not move past unapplied data.
        detail::exec(db, "BEGIN");
        bool committed = false;
        struct TxGuard {
            sqlite3* db;
            bool* committed;
            ~TxGuard() {
                if (*committed) return;
                sqlite3_exec(db, "ROLLBACK", nullptr, nullptr, nullptr);
            }
        } tx_guard{db, &committed};

        int rc = sqlite3changeset_apply(
            db,
            static_cast<int>(data.size()),
            const_cast<void*>(static_cast<const void*>(data.data())),
            nullptr,
            [](void* ctx, int conflict_type, sqlite3_changeset_iter* iter)
                -> int {
                auto* cfg = static_cast<ReplicaConfig*>(ctx);
                if (!cfg->on_conflict) {
                    return SQLITE_CHANGESET_ABORT;
                }

                auto event = detail::extract_event(iter);

                ConflictType ct;
                switch (conflict_type) {
                case SQLITE_CHANGESET_DATA:        ct = ConflictType::Data; break;
                case SQLITE_CHANGESET_NOTFOUND:    ct = ConflictType::NotFound; break;
                case SQLITE_CHANGESET_CONFLICT:    ct = ConflictType::Conflict; break;
                case SQLITE_CHANGESET_CONSTRAINT:  ct = ConflictType::Constraint; break;
                case SQLITE_CHANGESET_FOREIGN_KEY: ct = ConflictType::ForeignKey; break;
                default:                           ct = ConflictType::Data; break;
                }

                auto action = cfg->on_conflict(ct, event);
                switch (action) {
                case ConflictAction::Omit:    return SQLITE_CHANGESET_OMIT;
                case ConflictAction::Replace: return SQLITE_CHANGESET_REPLACE;
                case ConflictAction::Abort:   return SQLITE_CHANGESET_ABORT;
                default:                      return SQLITE_CHANGESET_ABORT;
                }
            },
            &config);

        if (rc != SQLITE_OK) {
            throw Error(ErrorCode::SqliteError,
                        std::string("changeset_apply: ") + sqlite3_errmsg(db));
        }

        auto events = detail::collect_events(data);

        // SQLPIPE_SEQ_AFTER_APPLY: persisted seq moves only with the
        // committed rows, never before apply succeeds.
        detail::write_seq(db, new_seq, config.seq_key);
        detail::exec(db, "COMMIT");
        committed = true;
        seq = new_seq;
        observed_remote_seq = new_seq;
        return events;
    }

    HandleResult handle_hello_from_master(const HelloMsg& m) {
        if (state != Replica::State::Handshake) {
            state = Replica::State::Error;
            return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "received HelloMsg in unexpected state"}})}, {}, {}};
        }
        if (m.protocol_version != kProtocolVersion) {
            state = Replica::State::Error;
            return {{tagged(Message{ErrorMsg{ErrorCode::ProtocolError,
                "unsupported protocol version"}})}, {}, {}};
        }

        // -1 means the master did not report a sequence (unknown).
        if (m.last_seq >= 0) observed_remote_seq = m.last_seq;

        // Fast reconnect: master confirmed seq match — skip to Live.
        if (m.last_seq > 0 && m.last_seq == seq) {
            state = Replica::State::Live;
            SQLPIPE_LOG(config.on_log, LogLevel::Info,
                        "fast reconnect: seq match ({}), skipping diff sync", seq);
            return {{tagged(Message{AckMsg{seq}})}, {}, {}};
        }

        // Compute bucket hashes and send to master.
        report(DiffPhase::ComputingBuckets, {}, 0, 0);
        auto buckets = detail::compute_all_buckets(
            db, filter(), config.bucket_size);
        report(DiffPhase::ComputingBuckets, {},
               static_cast<std::int64_t>(buckets.size()),
               static_cast<std::int64_t>(buckets.size()));
        state = Replica::State::DiffBuckets;
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "sending {} bucket hashes (seq={})", buckets.size(), seq);
        return {{tagged(Message{BucketHashesMsg{
            std::move(buckets), seq, kProtocolVersion,
            detail::compute_schema_fingerprint(db, filter())}})}, {}, {}};
    }

    HandleResult handle_need_buckets(const NeedBucketsMsg& m) {
        // Accept in DiffBuckets (normal handshake) or Live (re-convergence).
        if (state != Replica::State::DiffBuckets &&
            state != Replica::State::Live) {
            state = Replica::State::Error;
            return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "received NeedBucketsMsg in unexpected state"}})}, {}, {}};
        }

        state = Replica::State::DiffRows;

        if (m.ranges.empty()) {
            // All buckets match; waiting for DiffReadyMsg.
            SQLPIPE_LOG(config.on_log, LogLevel::Info, "all buckets match, waiting for DiffReady");
            return {};
        }

        // Compute row hashes for requested ranges.
        RowHashesMsg rh;
        std::int64_t ranges_done = 0;
        auto ranges_total = static_cast<std::int64_t>(m.ranges.size());
        for (const auto& range : m.ranges) {
            report(DiffPhase::ComputingRowHashes, range.table,
                   ranges_done, ranges_total);
            auto rows = detail::compute_row_hashes(
                db, range.table, range.lo, range.hi);

            RowHashesEntry entry;
            entry.table = range.table;
            entry.lo = range.lo;
            entry.hi = range.hi;

            // Run-length encode: group contiguous rowids.
            if (!rows.empty()) {
                RowHashRun current_run;
                current_run.start_rowid = rows[0].rowid;
                current_run.count = 1;
                current_run.hashes.push_back(rows[0].hash);

                for (std::size_t i = 1; i < rows.size(); ++i) {
                    if (rows[i].rowid ==
                        current_run.start_rowid + current_run.count) {
                        // Contiguous.
                        current_run.count++;
                        current_run.hashes.push_back(rows[i].hash);
                    } else {
                        // Gap — start a new run.
                        entry.runs.push_back(std::move(current_run));
                        current_run = RowHashRun{};
                        current_run.start_rowid = rows[i].rowid;
                        current_run.count = 1;
                        current_run.hashes.push_back(rows[i].hash);
                    }
                }
                entry.runs.push_back(std::move(current_run));
            }

            rh.entries.push_back(std::move(entry));
            ++ranges_done;
        }

        SQLPIPE_LOG(config.on_log, LogLevel::Info, "sending row hashes for {} ranges", m.ranges.size());
        return {{tagged(Message{std::move(rh)})}, {}, {}};
    }

    HandleResult handle_diff_ready(const DiffReadyMsg& m) {
        // Accept in DiffRows, DiffBuckets (normal handshake), or Live (re-convergence).
        if (state != Replica::State::DiffRows &&
            state != Replica::State::DiffBuckets &&
            state != Replica::State::Live) {
            state = Replica::State::Error;
            return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "received DiffReadyMsg in unexpected state"}})}, {}, {}};
        }

        std::vector<ChangeEvent> events;

        // Save and disable foreign keys.
        auto fk_stmt = detail::prepare(db, "PRAGMA foreign_keys");
        bool fk_was_on = false;
        if (sqlite3_step(fk_stmt.get()) == SQLITE_ROW) {
            fk_was_on = sqlite3_column_int(fk_stmt.get(), 0) != 0;
        }
        fk_stmt = detail::StmtGuard{};
        if (fk_was_on) {
            detail::exec(db, "PRAGMA foreign_keys = OFF");
        }

        // Roll back the transaction and restore foreign_keys on any failure
        // between BEGIN and COMMIT (delete loop, write_seq, etc.) so an
        // exception never leaves the caller's connection with an open
        // transaction and FK enforcement disabled (F7).
        bool committed = false;
        struct TxGuard {
            sqlite3* db;
            bool fk_was_on;
            bool* committed;
            ~TxGuard() {
                if (*committed) return;
                sqlite3_exec(db, "ROLLBACK", nullptr, nullptr, nullptr);
                if (fk_was_on) {
                    sqlite3_exec(db, "PRAGMA foreign_keys = ON",
                                 nullptr, nullptr, nullptr);
                }
            }
        } tx_guard{db, fk_was_on, &committed};

        detail::exec(db, "BEGIN");

        report(DiffPhase::ApplyingPatchset, {},
               0, static_cast<std::int64_t>(m.deletes.size()) + 1);

        // Apply INSERT patchset (handles both inserts and updates via REPLACE).
        if (!m.patchset.empty()) {
            int rc = sqlite3changeset_apply(
                db,
                static_cast<int>(m.patchset.size()),
                const_cast<void*>(
                    static_cast<const void*>(m.patchset.data())),
                nullptr,
                [](void*, int, sqlite3_changeset_iter*) -> int {
                    return SQLITE_CHANGESET_REPLACE;
                },
                nullptr);

            if (rc != SQLITE_OK) {
                // TxGuard rolls back and restores foreign_keys.
                throw Error(ErrorCode::SqliteError,
                            std::string("diff patchset apply: ") +
                            sqlite3_errmsg(db));
            }

            events = detail::collect_events(m.patchset);
        }

        // Delete rows by rowid.
        for (const auto& td : m.deletes) {
            for (auto rid : td.rowids) {
                // Query the row before deleting to collect change events.
                std::string sel_sql =
                    "SELECT * FROM \"" + td.table + "\" WHERE rowid = ?";
                auto sel_stmt = detail::prepare(db, sel_sql.c_str());
                sqlite3_bind_int64(sel_stmt.get(), 1, rid);
                if (sqlite3_step(sel_stmt.get()) == SQLITE_ROW) {
                    ChangeEvent ev;
                    ev.table = td.table;
                    ev.op = OpType::Delete;
                    int ncol = sqlite3_column_count(sel_stmt.get());
                    ev.old_values.resize(static_cast<std::size_t>(ncol));
                    for (int c = 0; c < ncol; ++c) {
                        ev.old_values[static_cast<std::size_t>(c)] =
                            detail::to_value(
                                sqlite3_column_value(sel_stmt.get(), c));
                    }
                    events.push_back(std::move(ev));
                }
                sel_stmt = detail::StmtGuard{};

                std::string del_sql =
                    "DELETE FROM \"" + td.table + "\" WHERE rowid = ?";
                auto del_stmt = detail::prepare(db, del_sql.c_str());
                sqlite3_bind_int64(del_stmt.get(), 1, rid);
                detail::step_done(db, del_stmt.get());
            }
        }

        // Update seq inside the same transaction as the patch. The
        // in-memory sequence moves only after COMMIT.
        detail::write_seq(db, m.seq, config.seq_key);

        detail::exec(db, "COMMIT");
        committed = true;
        seq = m.seq;
        observed_remote_seq = m.seq;

        if (fk_was_on) {
            detail::exec(db, "PRAGMA foreign_keys = ON");
        }

        state = Replica::State::Live;
        SQLPIPE_LOG(config.on_log, LogLevel::Info, "diff applied, entering live at seq={}", seq);

        return {{tagged(Message{AckMsg{m.seq}})}, std::move(events), {}};
    }
};

// ── Public API ──────────────────────────────────────────────────────

#ifndef EMSCRIPTEN
Replica::Replica(PGconn* conn, ReplicaConfig config) {
#ifdef SQLPIPE_HAVE_POSTGRES
    impl_ = std::make_unique<Impl>(conn, std::move(config));
#else
    (void)conn;
    (void)config;
    throw Error(ErrorCode::PostgresError,
                "sqlpipe built without Postgres support (-DSQLPIPE_HAVE_POSTGRES)");
#endif
}
#endif

Replica::Replica(sqlite3* db, ReplicaConfig config)
    : impl_(std::make_unique<Impl>(db, std::move(config))) {
    impl_->init();
}

Replica::~Replica() = default;
Replica::Replica(Replica&&) noexcept = default;
Replica& Replica::operator=(Replica&&) noexcept = default;

OutMessage Replica::hello() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->hello();
#endif
#endif
    impl_->state = State::Handshake;
    const auto* f = impl_->config.table_filter
        ? &*impl_->config.table_filter : nullptr;
    return tagged(Message{HelloMsg{kProtocolVersion,
                    detail::compute_schema_fingerprint(impl_->db, f), {},
                    impl_->seq}});
}

std::vector<OutMessage> Replica::converge() {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->converge();
#endif
#endif
    // Compute bucket hashes and transition to DiffBuckets, waiting for
    // the master's NeedBuckets response. Can be called in any state.
    const auto* f = impl_->config.table_filter
        ? &*impl_->config.table_filter : nullptr;

    impl_->report(DiffPhase::ComputingBuckets, {}, 0, 0);
    auto buckets = detail::compute_all_buckets(
        impl_->db, f, impl_->config.bucket_size);
    impl_->report(DiffPhase::ComputingBuckets, {},
               static_cast<std::int64_t>(buckets.size()),
               static_cast<std::int64_t>(buckets.size()));

    auto sv = detail::compute_schema_fingerprint(impl_->db, f);

    impl_->state = State::DiffBuckets;
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info,
                "converge: sending {} bucket hashes (seq={})",
                buckets.size(), impl_->seq);
    return {tagged(Message{BucketHashesMsg{
        std::move(buckets), impl_->seq, kProtocolVersion, sv}})};
}

HandleResult Replica::handle_message(const Message& msg) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        if (impl_->config.queue_while_predicting &&
            impl_->prediction != Impl::PredictionState::None) {
            impl_->prediction_queue.push_back(msg);
            return {};
        }
        if (impl_->prediction == Impl::PredictionState::Committed) {
            impl_->pg->rollback_prediction();
            impl_->prediction = Impl::PredictionState::None;
        }
        auto result = impl_->pg->handle_message(msg);
        impl_->state = impl_->pg->state;
        impl_->seq = impl_->pg->seq;
        if (impl_->state == State::Live && !impl_->watch.empty() &&
            !result.changes.empty()) {
            std::set<std::string> affected;
            for (const auto& ev : result.changes) {
                if (!ev.table.empty()) affected.insert(ev.table);
            }
            result.subscriptions = impl_->watch.notify(affected);
        }
        return result;
    }
#endif
#endif
    // Optional: hold inbound truth while a prediction sandbox is open.
    if (impl_->config.queue_while_predicting &&
        impl_->prediction != Impl::PredictionState::None) {
        impl_->prediction_queue.push_back(msg);
        SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug,
                    "prediction queue: deferred inbound message (depth={})",
                    impl_->prediction_queue.size());
        return {};
    }

    // Default: auto-rollback a committed prediction before applying.
    if (impl_->prediction == Impl::PredictionState::Committed) {
        detail::exec(impl_->db, "ROLLBACK TO _sqlpipe_prediction");
        detail::exec(impl_->db, "RELEASE _sqlpipe_prediction");
        impl_->prediction = Impl::PredictionState::None;
        SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug,
                    "prediction auto-rolled back (server response)");
    }

    auto prev_state = impl_->state;
    const Changeset* changeset_data = nullptr;  // for predicate-aware notify
    auto result = std::visit([&](const auto& m) -> HandleResult {
        using T = std::decay_t<decltype(m)>;

        if constexpr (std::is_same_v<T, HelloMsg>) {
            return impl_->handle_hello_from_master(m);
        }
        else if constexpr (std::is_same_v<T, NeedBucketsMsg>) {
            return impl_->handle_need_buckets(m);
        }
        else if constexpr (std::is_same_v<T, DiffReadyMsg>) {
            return impl_->handle_diff_ready(m);
        }
        else if constexpr (std::is_same_v<T, ChangesetMsg>) {
            if (impl_->state != State::Live) {
                impl_->state = State::Error;
                return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                    "received ChangesetMsg in unexpected state"}})}, {}, {}};
            }
            changeset_data = &m.data;
            auto events = impl_->apply_changeset(m.data, m.seq);
            SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug, "applied changeset seq={}", m.seq);
            return {{tagged(Message{AckMsg{m.seq}})}, std::move(events), {}};
        }
        else if constexpr (std::is_same_v<T, ErrorMsg>) {
            if (m.code == ErrorCode::SchemaMismatch) {
                bool resolved = false;
                if (impl_->config.on_schema_mismatch) {
                    // User callback gets first shot.
                    auto my_sv = schema_version();
                    resolved = impl_->config.on_schema_mismatch(
                        m.remote_schema_version, my_sv,
                        m.remote_schema_sql);
                } else if (!m.remote_schema_sql.empty()) {
                    // Auto-migrate: use sqlift to adopt master's schema.
                    resolved = detail::auto_migrate_schema(
                        impl_->db, m.remote_schema_sql,
                        impl_->config.on_log);
                }
                if (resolved) {
                    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info,
                                "schema mismatch resolved, "
                                "resetting to Init");
                    impl_->state = State::Init;
                    impl_->seq = detail::read_seq(
                        impl_->db, impl_->config.seq_key);
                    return {};
                }
            }
            impl_->state = State::Error;
            SQLPIPE_LOG(impl_->config.on_log, LogLevel::Error, "received error from master: {}", m.detail);
            return {};
        }
        else {
            return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                "unexpected message type from master"}})}, {}, {}};
        }
    }, msg);

    // Evaluate subscriptions only when Live — never during handshake or
    // diff sync, where schema may be incomplete or data in flux.
    if (impl_->state == State::Live && !impl_->watch.empty()) {
        if (!result.changes.empty()) {
            std::set<std::string> affected;
            for (const auto& ev : result.changes) {
                if (!ev.table.empty()) affected.insert(ev.table);
            }
            if (changeset_data) {
                result.subscriptions = impl_->watch.notify(
                    affected, *changeset_data);
            } else {
                result.subscriptions = impl_->watch.notify(affected);
            }
        } else if (prev_state != State::Live) {
            // Just entered Live (e.g., diff sync found no differences).
            // Force-evaluate all subscriptions so clients that subscribed
            // before sync get current data.
            auto all_tables = detail::get_tracked_tables(
                impl_->db, impl_->filter());
            std::set<std::string> all(all_tables.begin(), all_tables.end());
            result.subscriptions = impl_->watch.notify(all);
        }
    }

    return result;
}

HandleResult Replica::handle_messages(std::span<const Message> msgs) {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        HandleResult combined;
        for (const auto& msg : msgs) {
            auto r = handle_message(msg);
            combined.messages.insert(combined.messages.end(),
                r.messages.begin(), r.messages.end());
            combined.changes.insert(combined.changes.end(),
                r.changes.begin(), r.changes.end());
            combined.subscriptions.insert(combined.subscriptions.end(),
                r.subscriptions.begin(), r.subscriptions.end());
        }
        return combined;
    }
#endif
#endif
    if (impl_->config.queue_while_predicting &&
        impl_->prediction != Impl::PredictionState::None) {
        impl_->prediction_queue.insert(impl_->prediction_queue.end(),
                                       msgs.begin(), msgs.end());
        SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug,
                    "prediction queue: deferred {} inbound messages (depth={})",
                    msgs.size(), impl_->prediction_queue.size());
        return {};
    }

    if (impl_->prediction == Impl::PredictionState::Committed) {
        detail::exec(impl_->db, "ROLLBACK TO _sqlpipe_prediction");
        detail::exec(impl_->db, "RELEASE _sqlpipe_prediction");
        impl_->prediction = Impl::PredictionState::None;
    }
    auto prev_state = impl_->state;
    HandleResult combined;
    std::set<std::string> affected;

    for (const auto& msg : msgs) {
        auto result = std::visit([&](const auto& m) -> HandleResult {
            using T = std::decay_t<decltype(m)>;

            if constexpr (std::is_same_v<T, HelloMsg>) {
                return impl_->handle_hello_from_master(m);
            }
            else if constexpr (std::is_same_v<T, NeedBucketsMsg>) {
                return impl_->handle_need_buckets(m);
            }
            else if constexpr (std::is_same_v<T, DiffReadyMsg>) {
                return impl_->handle_diff_ready(m);
            }
            else if constexpr (std::is_same_v<T, ChangesetMsg>) {
                if (impl_->state != State::Live) {
                    impl_->state = State::Error;
                    return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                        "received ChangesetMsg in unexpected state"}})}, {}, {}};
                }
                auto events = impl_->apply_changeset(m.data, m.seq);
                SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug, "applied changeset seq={}", m.seq);
                return {{tagged(Message{AckMsg{m.seq}})}, std::move(events), {}};
            }
            else if constexpr (std::is_same_v<T, ErrorMsg>) {
                if (m.code == ErrorCode::SchemaMismatch) {
                    bool resolved = false;
                    if (impl_->config.on_schema_mismatch) {
                        auto my_sv = schema_version();
                        resolved = impl_->config.on_schema_mismatch(
                            m.remote_schema_version, my_sv,
                            m.remote_schema_sql);
                    } else if (!m.remote_schema_sql.empty()) {
                        resolved = detail::auto_migrate_schema(
                            impl_->db, m.remote_schema_sql,
                            impl_->config.on_log);
                    }
                    if (resolved) {
                        SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info,
                                    "schema mismatch resolved, "
                                    "resetting to Init");
                        impl_->state = State::Init;
                        impl_->seq = detail::read_seq(
                            impl_->db, impl_->config.seq_key);
                        return {};
                    }
                }
                impl_->state = State::Error;
                SQLPIPE_LOG(impl_->config.on_log, LogLevel::Error, "received error from master: {}", m.detail);
                return {};
            }
            else {
                return {{tagged(Message{ErrorMsg{ErrorCode::InvalidState,
                    "unexpected message type from master"}})}, {}, {}};
            }
        }, msg);

        combined.messages.insert(combined.messages.end(),
                                 result.messages.begin(),
                                 result.messages.end());
        for (const auto& ev : result.changes) {
            if (!ev.table.empty()) affected.insert(ev.table);
        }
        combined.changes.insert(combined.changes.end(),
                                result.changes.begin(),
                                result.changes.end());
    }

    // Evaluate subscriptions only when Live.
    if (impl_->state == State::Live && !impl_->watch.empty()) {
        if (!affected.empty()) {
            combined.subscriptions = impl_->watch.notify(affected);
        } else if (prev_state != State::Live) {
            auto all_tables = detail::get_tracked_tables(
                impl_->db, impl_->filter());
            std::set<std::string> all(all_tables.begin(), all_tables.end());
            combined.subscriptions = impl_->watch.notify(all);
        }
    }

    return combined;
}

SubscriptionId Replica::subscribe(const std::string& sql) {
    return impl_->watch.subscribe(sql);
}

void Replica::unsubscribe(SubscriptionId id) {
    impl_->watch.unsubscribe(id);
}

void Replica::begin_prediction() {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        using PS = Impl::PredictionState;
        if (impl_->prediction != PS::None) {
            throw Error(ErrorCode::InvalidState, "prediction already active");
        }
        impl_->pg->begin_prediction();
        impl_->prediction = PS::Drafting;
        return;
    }
#endif
#endif
    using PS = Impl::PredictionState;
    if (impl_->prediction != PS::None) {
        throw Error(ErrorCode::InvalidState,
                    "prediction already active");
    }
    if (!impl_->prediction_queue.empty()) {
        throw Error(ErrorCode::InvalidState,
                    "prediction queue not empty; call end_prediction first");
    }
    detail::exec(impl_->db, "SAVEPOINT _sqlpipe_prediction");
    impl_->prediction = PS::Drafting;
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug, "prediction begun");
}

void Replica::commit_prediction() {
    using PS = Impl::PredictionState;
    if (impl_->prediction != PS::Drafting) {
        throw Error(ErrorCode::InvalidState,
                    "no drafting prediction to commit");
    }
    impl_->prediction = PS::Committed;
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug,
                "prediction committed (awaiting end or server apply)");
}

HandleResult Replica::end_prediction() {
    using PS = Impl::PredictionState;
    if (impl_->prediction == PS::None) {
        throw Error(ErrorCode::InvalidState,
                    "no active prediction to end");
    }
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        impl_->pg->rollback_prediction();
        impl_->prediction = PS::None;
        if (impl_->prediction_queue.empty()) return {};
        auto queued = std::move(impl_->prediction_queue);
        impl_->prediction_queue.clear();
        return handle_messages(queued);
    }
#endif
#endif
    detail::exec(impl_->db, "ROLLBACK TO _sqlpipe_prediction");
    detail::exec(impl_->db, "RELEASE _sqlpipe_prediction");
    impl_->prediction = PS::None;
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Debug,
                "prediction ended (queue depth={})",
                impl_->prediction_queue.size());

    if (impl_->prediction_queue.empty()) {
        return {};
    }
    auto queued = std::move(impl_->prediction_queue);
    impl_->prediction_queue.clear();
    return handle_messages(queued);
}

void Replica::rollback_prediction() {
    // Discard overlay; still drain any deferred truth so the replica
    // does not stay frozen. Callers that need the drain's HandleResult
    // should use end_prediction() instead.
    (void)end_prediction();
}

std::size_t Replica::prediction_queue_size() const {
    return impl_->prediction_queue.size();
}

bool Replica::queues_while_predicting() const {
    return impl_->config.queue_while_predicting;
}

void Replica::reset() {
    impl_->observed_remote_seq.reset();
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) {
        if (impl_->prediction != Impl::PredictionState::None) {
            impl_->pg->rollback_prediction();
            impl_->prediction = Impl::PredictionState::None;
        }
        impl_->prediction_queue.clear();
        impl_->pg->reset();
        impl_->state = impl_->pg->state;
        impl_->seq = impl_->pg->seq;
        return;
    }
#endif
#endif
    // Rollback any active prediction before resetting.
    if (impl_->prediction != Impl::PredictionState::None) {
        detail::exec(impl_->db, "ROLLBACK TO _sqlpipe_prediction");
        detail::exec(impl_->db, "RELEASE _sqlpipe_prediction");
        impl_->prediction = Impl::PredictionState::None;
    }
    // Drop deferred messages — reconnect/diff will resync truth.
    impl_->prediction_queue.clear();
    impl_->state = State::Init;
    impl_->seq = detail::read_seq(impl_->db, impl_->config.seq_key);
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info, "replica reset to Init at seq={}", impl_->seq);
}

Seq Replica::current_seq() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->seq;
#endif
#endif
    return impl_->seq;
}

std::optional<Seq> Replica::observed_remote_seq() const {
    return impl_->observed_remote_seq;
}

SyncStatus Replica::sync_status() const {
    switch (state()) {
    case State::Init: return SyncStatus::Unsynchronized;
    case State::Handshake:
    case State::DiffBuckets:
    case State::DiffRows: return SyncStatus::Synchronizing;
    case State::Live: return SyncStatus::Live;
    case State::Error: return SyncStatus::Failed;
    }
    return SyncStatus::Failed;
}

std::size_t Replica::subscription_count() const {
    return impl_->watch.subscription_count();
}

SchemaVersion Replica::schema_version() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->fingerprint();
#endif
#endif
    const auto* f = impl_->config.table_filter
        ? &*impl_->config.table_filter : nullptr;
    return detail::compute_schema_fingerprint(impl_->db, f);
}

Replica::State Replica::state() const {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    if (impl_->pg) return impl_->pg->state;
#endif
#endif
    return impl_->state;
}

} // namespace sqlpipe

// ── peer_protocol.cpp ───────────────────────────────────────────

namespace sqlpipe {

std::vector<std::uint8_t> serialize(const PeerMessage& msg) {
    auto inner = serialize(msg.payload);  // [4B len][tag][payload]

    // Read inner length from bytes 0..3.
    std::uint32_t inner_len =
        static_cast<std::uint32_t>(inner[0]) |
        (static_cast<std::uint32_t>(inner[1]) << 8) |
        (static_cast<std::uint32_t>(inner[2]) << 16) |
        (static_cast<std::uint32_t>(inner[3]) << 24);

    // New total = inner_len + 1 (for sender_role byte).
    std::uint32_t total = inner_len + 1;

    std::vector<std::uint8_t> buf;
    buf.reserve(4 + 1 + inner_len);

    // Length prefix.
    buf.push_back(static_cast<std::uint8_t>(total));
    buf.push_back(static_cast<std::uint8_t>(total >> 8));
    buf.push_back(static_cast<std::uint8_t>(total >> 16));
    buf.push_back(static_cast<std::uint8_t>(total >> 24));

    // Sender role.
    buf.push_back(static_cast<std::uint8_t>(msg.sender_role));

    // Tag + payload (skip the inner's 4-byte length prefix).
    buf.insert(buf.end(), inner.begin() + 4, inner.end());

    return buf;
}

PeerMessage deserialize_peer(std::span<const std::uint8_t> buf) {
    if (buf.size() < 6) {
        throw Error(ErrorCode::ProtocolError, "peer message too short");
    }

    // Read total length (bytes 0..3).
    std::uint32_t total =
        static_cast<std::uint32_t>(buf[0]) |
        (static_cast<std::uint32_t>(buf[1]) << 8) |
        (static_cast<std::uint32_t>(buf[2]) << 16) |
        (static_cast<std::uint32_t>(buf[3]) << 24);

    auto role = static_cast<SenderRole>(buf[4]);
    if (role != SenderRole::AsMaster && role != SenderRole::AsReplica) {
        throw Error(ErrorCode::ProtocolError,
                    "invalid sender role: " +
                    std::to_string(static_cast<int>(buf[4])));
    }

    // Reconstruct inner Message buffer: [4B len][tag+payload].
    std::uint32_t msg_len = total - 1;
    std::vector<std::uint8_t> msg_buf;
    msg_buf.reserve(4 + msg_len);
    msg_buf.push_back(static_cast<std::uint8_t>(msg_len));
    msg_buf.push_back(static_cast<std::uint8_t>(msg_len >> 8));
    msg_buf.push_back(static_cast<std::uint8_t>(msg_len >> 16));
    msg_buf.push_back(static_cast<std::uint8_t>(msg_len >> 24));
    msg_buf.insert(msg_buf.end(), buf.begin() + 5, buf.end());

    return PeerMessage{role, deserialize(msg_buf)};
}

} // namespace sqlpipe

// ── peer.cpp ────────────────────────────────────────────────────

namespace sqlpipe {

struct Peer::Impl {
    sqlite3*    db = nullptr;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
    PGconn*     pg = nullptr;
#endif
#endif
    PeerConfig  config;
    Peer::State state = Peer::State::Init;

    std::set<std::string> my_tables;
    std::set<std::string> their_tables;

    std::unique_ptr<Master>  master;
    std::unique_ptr<Replica> replica;

    bool master_handshake_done = false;
    bool replica_handshake_done = false;

    bool is_server() const {
        return config.role == PeerRole::Server;
    }

    /// Resolve owned_tables glob patterns against tracked tables in the db.
    std::set<std::string> resolve_owned_patterns() {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (pg) {
            auto all = pg_list_user_tables(
                pg, config.table_filter ? &*config.table_filter : nullptr);
            std::set<std::string> resolved;
            for (const auto& pattern : config.owned_tables) {
                for (const auto& table : all) {
                    if (sqlite3_strglob(pattern.c_str(), table.c_str()) == 0)
                        resolved.insert(table);
                }
            }
            return resolved;
        }
#endif
#endif
        auto all = detail::get_tracked_tables(db,
            config.table_filter ? &*config.table_filter : nullptr);
        std::set<std::string> resolved;
        for (const auto& pattern : config.owned_tables) {
            for (const auto& table : all) {
                if (sqlite3_strglob(pattern.c_str(), table.c_str()) == 0) {
                    resolved.insert(table);
                }
            }
        }
        return resolved;
    }

    void create_master() {
        MasterConfig mc;
        mc.table_filter = my_tables;
        mc.seq_key = "master_seq";
        mc.on_progress = config.on_progress;
        mc.on_schema_mismatch = config.on_schema_mismatch;
        mc.on_log = config.on_log;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (pg) {
            master = std::make_unique<Master>(pg, mc);
            return;
        }
#endif
#endif
        master = std::make_unique<Master>(db, mc);
    }

    void create_replica() {
        ReplicaConfig rc;
        rc.on_conflict = config.on_conflict;
        rc.table_filter = their_tables;
        rc.seq_key = "replica_seq";
        rc.on_progress = config.on_progress;
        rc.on_schema_mismatch = config.on_schema_mismatch;
        rc.on_log = config.on_log;
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
        if (pg) {
            replica = std::make_unique<Replica>(pg, rc);
            return;
        }
#endif
#endif
        replica = std::make_unique<Replica>(db, rc);
    }

    void check_live() {
        if (master_handshake_done && replica_handshake_done) {
            state = Peer::State::Live;
            SQLPIPE_LOG(config.on_log, LogLevel::Info,
                        "peer is live: owning {} tables, replicating {} tables",
                        my_tables.size(), their_tables.size());
        }
    }

    std::set<std::string> complement_tables(
            const std::set<std::string>& subset) {
        std::vector<std::string> all;
        if (config.table_filter) {
            all.assign(config.table_filter->begin(),
                       config.table_filter->end());
        } else {
#ifdef SQLPIPE_HAVE_POSTGRES
#ifndef EMSCRIPTEN
            if (pg) {
                all = pg_list_user_tables(pg, nullptr);
            } else
#endif
#endif
            all = detail::get_tracked_tables(db);
        }
        std::set<std::string> result;
        for (auto& t : all) {
            if (subset.find(t) == subset.end()) {
                result.insert(std::move(t));
            }
        }
        return result;
    }

    PeerHandleResult handle_as_replica(const PeerMessage& msg) {
        PeerHandleResult result;

        // First AsReplica HelloMsg triggers ownership negotiation.
        if (auto* hello = std::get_if<HelloMsg>(&msg.payload)) {
            if (state == Peer::State::Init ||
                (state == Peer::State::Negotiating && !master)) {
                // Ownership negotiation.
                if (hello->owned_tables.empty()) {
                    state = Peer::State::Error;
                    result.messages.push_back(tagged(PeerMessage{
                        SenderRole::AsMaster,
                        ErrorMsg{ErrorCode::ProtocolError,
                                 "peer hello must include owned_tables"}}));
                    return result;
                }

                // Reject if client claims tables outside our table_filter.
                if (config.table_filter) {
                    for (const auto& t : hello->owned_tables) {
                        if (config.table_filter->find(t) ==
                                config.table_filter->end()) {
                            state = Peer::State::Error;
                            result.messages.push_back(tagged(PeerMessage{
                                SenderRole::AsMaster,
                                ErrorMsg{ErrorCode::OwnershipRejected,
                                    "table '" + t +
                                    "' is not in table_filter"}}));
                            return result;
                        }
                    }
                }

                if (config.approve_ownership) {
                    if (!config.approve_ownership(hello->owned_tables)) {
                        state = Peer::State::Error;
                        result.messages.push_back(tagged(PeerMessage{
                            SenderRole::AsMaster,
                            ErrorMsg{ErrorCode::OwnershipRejected,
                                     "ownership request rejected"}}));
                        return result;
                    }
                }

                // Accept: their tables = what they claimed, ours = complement.
                their_tables = hello->owned_tables;
                my_tables = complement_tables(their_tables);
                state = Peer::State::Diffing;

                create_master();
                create_replica();

                // Patch hello's schema_version to match our Master's so
                // the Master doesn't trigger a spurious schema mismatch.
                HelloMsg patched = *hello;
                patched.schema_version = master->schema_version();
                patched.owned_tables = {};
                patched.last_seq = -1;  // Peer directions use different seq keys

                auto master_resp = master->handle_message(patched);
                for (auto& om : master_resp) {
                    if (std::holds_alternative<DiffReadyMsg>(om.msg)) {
                        master_handshake_done = true;
                    }
                    result.messages.push_back(PeerOutMessage{
                        PeerMessage{SenderRole::AsMaster, std::move(om.msg)},
                        om.delivery});
                }

                // Also initiate our Replica's hello (to sync their tables).
                auto our_hello = replica->hello();
                auto& h = std::get<HelloMsg>(our_hello.msg);
                h.owned_tables = my_tables;
                h.last_seq = -1;  // Peer directions use different seq keys
                result.messages.push_back(PeerOutMessage{
                    PeerMessage{SenderRole::AsReplica, std::move(our_hello.msg)},
                    our_hello.delivery});

                check_live();
                return result;
            }
        }

        // Subsequent AsReplica messages → forward to Master.
        if (!master) {
            result.messages.push_back(tagged(PeerMessage{
                SenderRole::AsMaster,
                ErrorMsg{ErrorCode::InvalidState,
                         "master not initialized"}}));
            return result;
        }

        auto master_resp = master->handle_message(msg.payload);
        for (auto& om : master_resp) {
            if (std::holds_alternative<DiffReadyMsg>(om.msg)) {
                master_handshake_done = true;
            }
            result.messages.push_back(PeerOutMessage{
                PeerMessage{SenderRole::AsMaster, std::move(om.msg)},
                om.delivery});
        }

        check_live();
        return result;
    }

    PeerHandleResult handle_as_master(const PeerMessage& msg) {
        PeerHandleResult result;

        if (!replica) {
            result.messages.push_back(tagged(PeerMessage{
                SenderRole::AsReplica,
                ErrorMsg{ErrorCode::InvalidState,
                         "replica not initialized"}}));
            return result;
        }

        // If this is a HelloMsg, patch schema_version and transition state.
        Message forwarded = msg.payload;
        if (auto* hello = std::get_if<HelloMsg>(&forwarded)) {
            if (state == Peer::State::Negotiating) {
                state = Peer::State::Diffing;
            }
            hello->schema_version = replica->schema_version();
            hello->owned_tables = {};
            hello->last_seq = -1;  // Peer directions use different seq keys
        }

        auto hr = replica->handle_message(forwarded);

        for (auto& om : hr.messages) {
            result.messages.push_back(PeerOutMessage{
                PeerMessage{SenderRole::AsReplica, std::move(om.msg)},
                om.delivery});
        }
        result.changes = std::move(hr.changes);
        result.subscriptions = std::move(hr.subscriptions);

        // Check if replica just went live.
        if (replica->state() == Replica::State::Live) {
            replica_handshake_done = true;
            check_live();
        }

        return result;
    }
};

// ── Peer public API ─────────────────────────────────────────────────

#ifndef EMSCRIPTEN
Peer::Peer(PGconn* conn, PeerConfig config)
    : impl_(std::make_unique<Impl>()) {
#ifdef SQLPIPE_HAVE_POSTGRES
    impl_->pg = conn;
    impl_->config = std::move(config);
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info, "pg peer created ({})",
                impl_->is_server() ? "server" : "client");
#else
    (void)conn;
    (void)config;
    throw Error(ErrorCode::PostgresError,
                "sqlpipe built without Postgres support (-DSQLPIPE_HAVE_POSTGRES)");
#endif
}
#endif

Peer::Peer(sqlite3* db, PeerConfig config)
    : impl_(std::make_unique<Impl>()) {
    impl_->db = db;
    impl_->config = std::move(config);
    detail::ensure_meta_table(db);
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info, "peer created ({})",
                impl_->is_server() ? "server" : "client");
}

Peer::~Peer() = default;
Peer::Peer(Peer&&) noexcept = default;
Peer& Peer::operator=(Peer&&) noexcept = default;

std::vector<PeerOutMessage> Peer::start() {
    if (impl_->state != State::Init) {
        throw Error(ErrorCode::InvalidState, "start() already called");
    }
    if (impl_->is_server()) {
        throw Error(ErrorCode::InvalidState,
                    "server peer must not call start()");
    }

    impl_->state = State::Negotiating;
    impl_->my_tables = impl_->resolve_owned_patterns();
    if (impl_->my_tables.empty()) {
        throw Error(ErrorCode::InvalidState,
            "owned_tables patterns matched no tables");
    }
    impl_->their_tables = impl_->complement_tables(impl_->my_tables);

    impl_->create_master();
    impl_->create_replica();

    auto hello_out = impl_->replica->hello();
    auto& h = std::get<HelloMsg>(hello_out.msg);
    h.owned_tables = impl_->my_tables;
    h.last_seq = -1;  // Peer directions use different seq keys

    return {PeerOutMessage{
        PeerMessage{SenderRole::AsReplica, std::move(hello_out.msg)},
        hello_out.delivery}};
}

std::vector<PeerOutMessage> Peer::flush() {
    if (!impl_->master) return {};
    if (impl_->state != State::Live && impl_->state != State::Diffing) return {};

    auto msgs = impl_->master->flush();
    std::vector<PeerOutMessage> result;
    result.reserve(msgs.size());
    for (auto& om : msgs) {
        result.push_back(PeerOutMessage{
            PeerMessage{SenderRole::AsMaster, std::move(om.msg)},
            om.delivery});
    }
    return result;
}

PeerHandleResult Peer::handle_message(const PeerMessage& msg) {
    if (msg.sender_role == SenderRole::AsReplica) {
        return impl_->handle_as_replica(msg);
    } else {
        return impl_->handle_as_master(msg);
    }
}

SubscriptionId Peer::subscribe(const std::string& sql) {
    if (!impl_->replica) {
        throw Error(ErrorCode::InvalidState,
                    "subscribe() requires replica to be initialized");
    }
    return impl_->replica->subscribe(sql);
}

void Peer::unsubscribe(SubscriptionId id) {
    if (!impl_->replica) {
        throw Error(ErrorCode::InvalidState,
                    "unsubscribe() requires replica to be initialized");
    }
    impl_->replica->unsubscribe(id);
}

void Peer::reset() {
    impl_->state = State::Init;
    impl_->master.reset();
    impl_->replica.reset();
    impl_->master_handshake_done = false;
    impl_->replica_handshake_done = false;
    SQLPIPE_LOG(impl_->config.on_log, LogLevel::Info, "peer reset to Init");
}

Peer::State Peer::state() const { return impl_->state; }

SyncStatus Peer::sync_status() const {
    switch (impl_->state) {
    case State::Init: return SyncStatus::Unsynchronized;
    case State::Negotiating:
    case State::Diffing: return SyncStatus::Synchronizing;
    case State::Live: return SyncStatus::Live;
    case State::Error: return SyncStatus::Failed;
    }
    return SyncStatus::Failed;
}

std::size_t Peer::subscription_count() const {
    if (!impl_->replica) return 0;
    return impl_->replica->subscription_count();
}

const std::set<std::string>& Peer::owned_tables() const {
    return impl_->my_tables;
}

const std::set<std::string>& Peer::remote_tables() const {
    return impl_->their_tables;
}

// ── Convenience utilities ────────────────────────────────────────

void sync_handshake(Master& master, Replica& replica) {
    auto pending = master.handle_message(replica.hello().msg);
    while (!pending.empty()) {
        std::vector<OutMessage> for_master;
        for (const auto& out : pending) {
            auto hr = replica.handle_message(out.msg);
            for_master.insert(for_master.end(),
                              hr.messages.begin(), hr.messages.end());
        }
        pending.clear();
        for (const auto& out : for_master) {
            auto resp = master.handle_message(out.msg);
            pending.insert(pending.end(), resp.begin(), resp.end());
        }
    }
}

// ── Relay ───────────────────────────────────────────────────────────

struct Relay::Impl {
    sqlite3*     db;
    RelayConfig  config;
    Master       master;
    Replica      replica;
    std::map<std::size_t, SinkCallback> sinks;
    std::size_t  next_sink_id = 1;

    Impl(sqlite3* db_, RelayConfig cfg)
        : db(db_),
          config(std::move(cfg)),
          master(db_, make_master_config()),
          replica(db_, make_replica_config()) {}

    MasterConfig make_master_config() {
        MasterConfig mc;
        mc.table_filter = config.table_filter;
        mc.on_log = config.on_log;
        return mc;
    }

    ReplicaConfig make_replica_config() {
        ReplicaConfig rc;
        rc.table_filter = config.table_filter;
        rc.on_conflict = config.on_conflict;
        rc.on_schema_mismatch = config.on_schema_mismatch;
        rc.on_log = config.on_log;
        return rc;
    }

    void broadcast() {
        auto msgs = master.flush();
        for (auto& out : msgs) {
            for (auto& [_, cb] : sinks) {
                cb(out);
            }
        }
    }
};

Relay::Relay(sqlite3* db, RelayConfig config)
    : impl_(std::make_unique<Impl>(db, std::move(config))) {}

Relay::~Relay() = default;
Relay::Relay(Relay&&) noexcept = default;
Relay& Relay::operator=(Relay&&) noexcept = default;

std::size_t Relay::add_sink(SinkCallback cb) {
    auto id = impl_->next_sink_id++;
    impl_->sinks[id] = std::move(cb);
    return id;
}

void Relay::remove_sink(std::size_t id) {
    impl_->sinks.erase(id);
}

OutMessage Relay::hello() {
    return impl_->replica.hello();
}

std::vector<OutMessage> Relay::handle_upstream(const Message& msg) {
    auto hr = impl_->replica.handle_message(msg);
    impl_->broadcast();
    return std::move(hr.messages);
}

std::vector<OutMessage> Relay::handle_downstream(const Message& msg) {
    return impl_->master.handle_message(msg);
}

SubscriptionId Relay::subscribe(const std::string& sql) {
    return impl_->replica.subscribe(sql);
}

void Relay::unsubscribe(SubscriptionId id) {
    impl_->replica.unsubscribe(id);
}

void Relay::reset() {
    impl_->replica.reset();
}

// ── Convenience utilities ────────────────────────────────────────

void sync_handshake(Peer& client, Peer& server) {
    auto pending_for_server = client.start();
    while (!pending_for_server.empty() ||
           client.state() != Peer::State::Live ||
           server.state() != Peer::State::Live) {
        std::vector<PeerOutMessage> pending_for_client;
        for (const auto& pout : pending_for_server) {
            auto hr = server.handle_message(pout.msg);
            pending_for_client.insert(pending_for_client.end(),
                                      hr.messages.begin(), hr.messages.end());
        }
        pending_for_server.clear();
        for (const auto& pout : pending_for_client) {
            auto hr = client.handle_message(pout.msg);
            pending_for_server.insert(pending_for_server.end(),
                                      hr.messages.begin(), hr.messages.end());
        }
        if (pending_for_server.empty() &&
            (client.state() != Peer::State::Live ||
             server.state() != Peer::State::Live)) {
            break;
        }
    }
}

void sync_handshake(Master& master, Relay& relay) {
    auto pending = master.handle_message(relay.hello().msg);
    while (!pending.empty()) {
        std::vector<OutMessage> for_master;
        for (const auto& out : pending) {
            auto resp = relay.handle_upstream(out.msg);
            for_master.insert(for_master.end(), resp.begin(), resp.end());
        }
        pending.clear();
        for (const auto& out : for_master) {
            auto resp = master.handle_message(out.msg);
            pending.insert(pending.end(), resp.begin(), resp.end());
        }
    }
}

} // namespace sqlpipe

// ── BUNDLED DEPENDENCIES (auto-generated by scripts/bundle-deps.sh) ──
// Do not edit below this line. Regenerate with: scripts/bundle-deps.sh

// ── sqlift (schema migration) ────────────────────────────────────
// Source: vendor/src/sqlift.cpp

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// sqlift - Declarative SQLite schema migration library


#include <cstdint>
#include <map>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>
#include <sqlite3.h>

namespace sqlift {

// --- error.h ---

class Error : public std::runtime_error {
    using std::runtime_error::runtime_error;
};

class ParseError : public Error {
    using Error::Error;
};

class ExtractError : public Error {
    using Error::Error;
};

class DiffError : public Error {
    using Error::Error;
};

class ApplyError : public Error {
    using Error::Error;
};

class DriftError : public Error {
    using Error::Error;
};

class DestructiveError : public Error {
    using Error::Error;
};

class BreakingChangeError : public Error {
    using Error::Error;
};

class JsonError : public Error {
    using Error::Error;
};

class RebuildError : public Error {
    using Error::Error;
};

// --- sqlite_util.h ---

// RAII wrapper for sqlite3*.
class Database {
public:
    explicit Database(const std::string& path,
                      int flags = SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE);
    explicit Database(sqlite3* borrowed);
    ~Database();

    Database(Database&& other) noexcept;
    Database& operator=(Database&& other) noexcept;
    Database(const Database&) = delete;
    Database& operator=(const Database&) = delete;

    sqlite3* get() const { return db_; }
    operator sqlite3*() const { return db_; }

    void exec(const std::string& sql);

private:
    sqlite3* db_ = nullptr;
    bool owns_ = true;
};

// RAII wrapper for sqlite3_stmt*.
class Statement {
public:
    Statement(sqlite3* db, const std::string& sql);
    ~Statement();

    Statement(Statement&& other) noexcept;
    Statement& operator=(Statement&& other) noexcept;
    Statement(const Statement&) = delete;
    Statement& operator=(const Statement&) = delete;

    bool step();

    int64_t column_int(int col) const;
    std::string column_text(int col) const;

    void bind_text(int param, const std::string& value);
    void bind_int(int param, int64_t value);

    sqlite3_stmt* get() const { return stmt_; }

private:
    sqlite3_stmt* stmt_ = nullptr;
};

// --- schema.h ---

enum class GeneratedType {
    Normal  = 0,
    Virtual = 2,
    Stored  = 3,
};

struct Column {
    std::string name;
    std::string type;
    bool notnull = false;
    std::string default_value;
    int pk = 0;
    std::string collation;
    GeneratedType generated = GeneratedType::Normal;
    std::string generated_expr;

    bool operator==(const Column&) const = default;
};

struct CheckConstraint {
    std::string name;
    std::string expression;
    bool operator==(const CheckConstraint&) const = default;
};

struct ForeignKey {
    std::string constraint_name;
    std::vector<std::string> from_columns;
    std::string to_table;
    std::vector<std::string> to_columns;
    std::string on_update = "NO ACTION";
    std::string on_delete = "NO ACTION";

    bool operator==(const ForeignKey& o) const {
        return from_columns == o.from_columns && to_table == o.to_table &&
               to_columns == o.to_columns && on_update == o.on_update &&
               on_delete == o.on_delete;
    }
};

struct Table {
    std::string name;
    std::vector<Column> columns;
    std::vector<ForeignKey> foreign_keys;
    std::vector<CheckConstraint> check_constraints;
    std::string pk_constraint_name;
    bool without_rowid = false;
    bool strict = false;
    std::string raw_sql;

    bool operator==(const Table& o) const {
        return name == o.name && columns == o.columns &&
               foreign_keys == o.foreign_keys &&
               check_constraints == o.check_constraints &&
               without_rowid == o.without_rowid &&
               strict == o.strict;
    }
};

struct Index {
    std::string name;
    std::string table_name;
    std::vector<std::string> columns;
    bool unique = false;
    std::string where_clause;
    std::string raw_sql;

    bool operator==(const Index& o) const {
        return name == o.name && table_name == o.table_name &&
               columns == o.columns && unique == o.unique &&
               where_clause == o.where_clause;
    }
};

struct View {
    std::string name;
    std::string sql;

    bool operator==(const View&) const = default;
};

struct Trigger {
    std::string name;
    std::string table_name;
    std::string sql;

    bool operator==(const Trigger&) const = default;
};

// A virtual table created via CREATE VIRTUAL TABLE <name> USING <module>(<args>).
// Diff treats any change as drop+recreate (destructive) — there is no in-place
// modification for virtual tables. raw_sql holds the full statement;
// equality compares only (name, module, args) so whitespace differences in
// the raw text don't trigger spurious recreates.
struct VirtualTable {
    std::string name;
    std::string module;
    std::string args;
    std::string raw_sql;

    bool operator==(const VirtualTable& o) const {
        return name == o.name && module == o.module && args == o.args;
    }
};

struct Schema {
    std::map<std::string, Table>         tables;
    std::map<std::string, Index>         indexes;
    std::map<std::string, View>          views;
    std::map<std::string, Trigger>       triggers;
    std::map<std::string, VirtualTable>  virtual_tables;
    std::string dialect = "sqlite"; // "sqlite" | "postgres"
    std::string schema_name;        // postgres search-path schema (v1: public)

    bool operator==(const Schema&) const = default;

    std::string hash() const;
};

// --- parse.h ---

Schema parse(const std::string& sql);

// --- extract.h ---

Schema extract(sqlite3* db);

// --- diff.h ---

enum class WarningType {
    RedundantIndex,
};

struct Warning {
    WarningType type;
    std::string message;
    std::string index_name;
    std::string covered_by;
    std::string table_name;
};

enum class OpType {
    CreateTable,
    DropTable,
    RebuildTable,
    AddColumn,
    CreateIndex,
    DropIndex,
    CreateView,
    DropView,
    CreateTrigger,
    DropTrigger,
    CreateVirtualTable,
    DropVirtualTable,
    DropColumn,
    AlterColumnType,
    SetNotNull,
    DropNotNull,
    SetDefault,
    DropDefault,
    AddConstraint,
    DropConstraint,
};

struct Operation {
    OpType type;
    std::string object_name;
    std::string description;
    std::vector<std::string> sql;
    bool destructive = false;
    bool requires_rebuild = false;
    // True when every change in this op is a strict relaxation of an
    // existing constraint (drop CHECK/FK, NOT NULL becomes nullable). Only
    // ever set on RebuildTable ops; never on additive or destructive ops.
    bool loosens_only = false;
    // True when this op's success depends on existing data: nullable to
    // NOT NULL, new FK or CHECK on an existing table, new NOT NULL column
    // without DEFAULT. Always tied to a RebuildTable op.
    bool data_dependent = false;
};

class MigrationPlan {
public:
    const std::vector<Operation>& operations() const { return ops_; }
    const std::vector<Warning>& warnings() const { return warnings_; }
    bool has_destructive_operations() const;
    bool has_rebuild_operations() const;
    bool empty() const { return ops_.empty(); }

private:
    friend MigrationPlan diff(const Schema& current, const Schema& desired);
    friend MigrationPlan from_json(const std::string& json_str);
    std::vector<Operation> ops_;
    std::vector<Warning> warnings_;
};

MigrationPlan diff(const Schema& current, const Schema& desired);

std::vector<Warning> detect_redundant_indexes(const Schema& schema);

// --- apply.h ---

struct ApplyOptions {
    bool allow_destructive = false;
    bool allow_rebuild = false;
    bool allow_loosen = false;
    bool allow_data_dependent = false;
};

void apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts = {});

int64_t migration_version(sqlite3* db);

Schema extract_exec(const sqlift_exec& ex, const std::string& schema_name);
Schema parse_on_exec(const sqlift_exec& ex, const std::string& ddl);
void apply_exec(const sqlift_exec& ex, const MigrationPlan& plan,
                const ApplyOptions& opts);
int64_t migration_version_exec(const sqlift_exec& ex);

// --- json.h ---

std::string to_string(OpType type);
OpType op_type_from_string(const std::string& s);
std::string to_json(const MigrationPlan& plan);
MigrationPlan from_json(const std::string& json_str);
std::string schema_to_json(const Schema& schema);
Schema schema_from_json(const std::string& json_str);

} // namespace sqlift

#include <algorithm>
#include <array>
#include <cctype>
#include <cstdlib>
#include <cstring>
#include <iomanip>
#include <set>
#include <sstream>
#include <utility>

#include <nlohmann/json.hpp>

namespace sqlift {

// --- sqlite_util.cpp ---




// --- Database ---

Database::Database(const std::string& path, int flags) {
    int rc = sqlite3_open_v2(path.c_str(), &db_, flags, nullptr);
    if (rc != SQLITE_OK) {
        std::string msg = db_ ? sqlite3_errmsg(db_) : "failed to allocate memory";
        sqlite3_close(db_);
        db_ = nullptr;
        throw Error("sqlite3_open_v2: " + msg);
    }
}

Database::Database(sqlite3* borrowed) : db_(borrowed), owns_(false) {
    if (!db_) throw Error("sqlift_db_wrap_sqlite: null handle");
}

Database::~Database() {
    if (owns_ && db_) sqlite3_close(db_);
}

Database::Database(Database&& other) noexcept
    : db_(other.db_), owns_(other.owns_) {
    other.db_ = nullptr;
    other.owns_ = false;
}

Database& Database::operator=(Database&& other) noexcept {
    if (this != &other) {
        if (owns_ && db_) sqlite3_close(db_);
        db_ = other.db_;
        owns_ = other.owns_;
        other.db_ = nullptr;
        other.owns_ = false;
    }
    return *this;
}

void Database::exec(const std::string& sql) {
    char* errmsg = nullptr;
    int rc = sqlite3_exec(db_, sql.c_str(), nullptr, nullptr, &errmsg);
    if (rc != SQLITE_OK) {
        std::string msg = errmsg ? errmsg : "unknown error";
        sqlite3_free(errmsg);
        throw Error("sqlite3_exec: " + msg);
    }
}

// --- Statement ---

Statement::Statement(sqlite3* db, const std::string& sql) {
    int rc = sqlite3_prepare_v2(db, sql.c_str(), -1, &stmt_, nullptr);
    if (rc != SQLITE_OK) {
        throw Error(std::string("sqlite3_prepare_v2: ") + sqlite3_errmsg(db));
    }
}

Statement::~Statement() {
    if (stmt_) sqlite3_finalize(stmt_);
}

Statement::Statement(Statement&& other) noexcept : stmt_(other.stmt_) {
    other.stmt_ = nullptr;
}

Statement& Statement::operator=(Statement&& other) noexcept {
    if (this != &other) {
        if (stmt_) sqlite3_finalize(stmt_);
        stmt_ = other.stmt_;
        other.stmt_ = nullptr;
    }
    return *this;
}

bool Statement::step() {
    int rc = sqlite3_step(stmt_);
    if (rc == SQLITE_ROW) return true;
    if (rc == SQLITE_DONE) return false;
    throw Error(std::string("sqlite3_step: ") +
                sqlite3_errmsg(sqlite3_db_handle(stmt_)));
}

int64_t Statement::column_int(int col) const {
    return sqlite3_column_int64(stmt_, col);
}

std::string Statement::column_text(int col) const {
    const unsigned char* text = sqlite3_column_text(stmt_, col);
    if (!text) return {};
    return reinterpret_cast<const char*>(text);
}

void Statement::bind_text(int param, const std::string& value) {
    int rc = sqlite3_bind_text(stmt_, param, value.c_str(), -1, SQLITE_TRANSIENT);
    if (rc != SQLITE_OK) {
        throw Error(std::string("sqlite3_bind_text: ") +
                    sqlite3_errmsg(sqlite3_db_handle(stmt_)));
    }
}

void Statement::bind_int(int param, int64_t value) {
    int rc = sqlite3_bind_int64(stmt_, param, value);
    if (rc != SQLITE_OK) {
        throw Error(std::string("sqlite3_bind_int: ") +
                    sqlite3_errmsg(sqlite3_db_handle(stmt_)));
    }
}


// --- hash.cpp ---



namespace {

constexpr std::array<uint32_t, 64> K = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
};

inline uint32_t rotr(uint32_t x, int n) { return (x >> n) | (x << (32 - n)); }
inline uint32_t ch(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ (~x & z); }
inline uint32_t maj(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ (x & z) ^ (y & z); }
inline uint32_t sigma0(uint32_t x) { return rotr(x, 2) ^ rotr(x, 13) ^ rotr(x, 22); }
inline uint32_t sigma1(uint32_t x) { return rotr(x, 6) ^ rotr(x, 11) ^ rotr(x, 25); }
inline uint32_t gamma0(uint32_t x) { return rotr(x, 7) ^ rotr(x, 18) ^ (x >> 3); }
inline uint32_t gamma1(uint32_t x) { return rotr(x, 17) ^ rotr(x, 19) ^ (x >> 10); }

std::string sha256(const std::string& input) {
    // Pre-processing: pad message
    uint64_t bit_len = input.size() * 8;
    std::vector<uint8_t> msg(input.begin(), input.end());
    msg.push_back(0x80);
    while ((msg.size() % 64) != 56)
        msg.push_back(0x00);
    for (int i = 7; i >= 0; --i)
        msg.push_back(static_cast<uint8_t>(bit_len >> (i * 8)));

    // Initial hash values
    uint32_t h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a;
    uint32_t h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19;

    // Process each 512-bit block
    for (size_t offset = 0; offset < msg.size(); offset += 64) {
        std::array<uint32_t, 64> w{};
        for (int i = 0; i < 16; ++i) {
            w[i] = (uint32_t(msg[offset + i * 4]) << 24) |
                   (uint32_t(msg[offset + i * 4 + 1]) << 16) |
                   (uint32_t(msg[offset + i * 4 + 2]) << 8) |
                   uint32_t(msg[offset + i * 4 + 3]);
        }
        for (int i = 16; i < 64; ++i)
            w[i] = gamma1(w[i - 2]) + w[i - 7] + gamma0(w[i - 15]) + w[i - 16];

        uint32_t a = h0, b = h1, c = h2, d = h3;
        uint32_t e = h4, f = h5, g = h6, h = h7;

        for (int i = 0; i < 64; ++i) {
            uint32_t t1 = h + sigma1(e) + ch(e, f, g) + K[i] + w[i];
            uint32_t t2 = sigma0(a) + maj(a, b, c);
            h = g; g = f; f = e; e = d + t1;
            d = c; c = b; b = a; a = t1 + t2;
        }

        h0 += a; h1 += b; h2 += c; h3 += d;
        h4 += e; h5 += f; h6 += g; h7 += h;
    }

    std::ostringstream oss;
    oss << std::hex << std::setfill('0');
    for (uint32_t v : {h0, h1, h2, h3, h4, h5, h6, h7})
        oss << std::setw(8) << v;
    return oss.str();
}

} // namespace


// --- schema.cpp ---




std::string Schema::hash() const {
    std::ostringstream oss;
    if (dialect == "postgres")
        oss << "DIALECT postgres\n";

    for (const auto& [name, table] : tables) {
        oss << "TABLE " << name << '\n';
        for (const auto& col : table.columns) {
            oss << "  COL " << col.name
                << ' ' << col.type
                << (col.notnull ? " NOTNULL" : "")
                << " DEFAULT=" << col.default_value
                << " PK=" << col.pk;
            if (!col.collation.empty())
                oss << " COLLATE=" << col.collation;
            if (col.generated != GeneratedType::Normal)
                oss << " GENERATED=" << static_cast<int>(col.generated);
            if (!col.generated_expr.empty())
                oss << " EXPR=" << col.generated_expr;
            oss << '\n';
        }
        for (const auto& fk : table.foreign_keys) {
            oss << "  FK";
            for (const auto& c : fk.from_columns) oss << ' ' << c;
            oss << " -> " << fk.to_table << '(';
            for (size_t i = 0; i < fk.to_columns.size(); ++i) {
                if (i > 0) oss << ',';
                oss << fk.to_columns[i];
            }
            oss << ") UPDATE=" << fk.on_update
                << " DELETE=" << fk.on_delete << '\n';
        }
        for (const auto& chk : table.check_constraints) {
            oss << "  CHECK";
            if (!chk.name.empty()) oss << " NAME=" << chk.name;
            oss << " EXPR=" << chk.expression << '\n';
        }
        oss << "  ROWID=" << (table.without_rowid ? "no" : "yes") << '\n';
        if (table.strict)
            oss << "  STRICT=yes\n";
    }

    for (const auto& [name, idx] : indexes) {
        oss << "INDEX " << name << " ON " << idx.table_name;
        oss << (idx.unique ? " UNIQUE" : "");
        for (const auto& c : idx.columns) oss << ' ' << c;
        if (!idx.where_clause.empty()) oss << " WHERE " << idx.where_clause;
        oss << '\n';
    }

    for (const auto& [name, view] : views)
        oss << "VIEW " << name << ' ' << view.sql << '\n';

    for (const auto& [name, trigger] : triggers)
        oss << "TRIGGER " << name << ' ' << trigger.sql << '\n';

    // Virtual tables: emit only when non-empty so existing schemas without
    // virtual tables keep their pre-existing hash.
    if (!virtual_tables.empty()) {
        for (const auto& [name, vt] : virtual_tables)
            oss << "VTABLE " << name << " USING " << vt.module
                << '(' << vt.args << ")\n";
    }

    return sha256(oss.str());
}


// --- extract.cpp ---




namespace {

bool starts_with(const std::string& s, const std::string& prefix) {
    return s.size() >= prefix.size() && s.compare(0, prefix.size(), prefix) == 0;
}

std::string to_upper(const std::string& s) {
    std::string result = s;
    std::transform(result.begin(), result.end(), result.begin(),
                   [](unsigned char c) { return std::toupper(c); });
    return result;
}

// Quote an identifier for use in SQL.
std::string quote_lit(const std::string& s) {
    std::string result = "'";
    for (char c : s) {
        if (c == '\'') result += "''";
        else result += c;
    }
    result += "'";
    return result;
}

std::string quote_id(const std::string& name) {
    // Use double quotes, escaping embedded double quotes.
    std::string result = "\"";
    for (char c : name) {
        if (c == '"') result += "\"\"";
        else result += c;
    }
    result += '"';
    return result;
}

std::string trim(const std::string& s) {
    auto start = s.find_first_not_of(" \t\n\r");
    if (start == std::string::npos) return {};
    auto end = s.find_last_not_of(" \t\n\r");
    return s.substr(start, end - start + 1);
}

std::string strip_quotes(const std::string& s) {
    if (s.size() >= 2 &&
        ((s.front() == '"' && s.back() == '"') ||
         (s.front() == '[' && s.back() == ']') ||
         (s.front() == '`' && s.back() == '`'))) {
        return s.substr(1, s.size() - 2);
    }
    return s;
}

// Parse the body of a CREATE TABLE statement to extract CHECK constraints
// and GENERATED ALWAYS AS expressions.
// Returns a pair: (check_constraints, column_name -> generated_expr map).
struct ParsedTableBody {
    std::vector<CheckConstraint> checks;
    std::map<std::string, std::string> generated_exprs;
    std::string pk_constraint_name;
    std::map<std::string, std::string> fk_constraint_names;  // key: comma-joined from_columns
};

ParsedTableBody parse_create_table_body(const std::string& raw_sql) {
    ParsedTableBody result;

    // Find the outer '(' of CREATE TABLE ... (...)
    int depth = 0;
    size_t body_start = std::string::npos;
    size_t body_end = std::string::npos;
    for (size_t i = 0; i < raw_sql.size(); ++i) {
        if (raw_sql[i] == '\'') {
            ++i; // skip opening quote
            while (i < raw_sql.size()) {
                if (raw_sql[i] == '\'' && i + 1 < raw_sql.size() && raw_sql[i + 1] == '\'')
                    i += 2; // skip escaped quote
                else if (raw_sql[i] == '\'')
                    break;
                else
                    ++i;
            }
            continue; // i now points at closing quote; loop ++i advances past it
        } else if (raw_sql[i] == '(') {
            if (depth == 0) body_start = i + 1;
            ++depth;
        } else if (raw_sql[i] == ')') {
            --depth;
            if (depth == 0) {
                body_end = i;
                break;
            }
        }
    }
    if (body_start == std::string::npos || body_end == std::string::npos)
        return result;

    // Split inner content by ',' at depth 0
    std::vector<std::string> defs;
    depth = 0;
    size_t seg_start = body_start;
    for (size_t i = body_start; i < body_end; ++i) {
        if (raw_sql[i] == '\'') {
            ++i; // skip opening quote
            while (i < body_end) {
                if (raw_sql[i] == '\'' && i + 1 < body_end && raw_sql[i + 1] == '\'')
                    i += 2; // skip escaped quote
                else if (raw_sql[i] == '\'')
                    break;
                else
                    ++i;
            }
            continue; // i now points at closing quote; loop ++i advances past it
        } else if (raw_sql[i] == '(') ++depth;
        else if (raw_sql[i] == ')') --depth;
        else if (raw_sql[i] == ',' && depth == 0) {
            defs.push_back(trim(raw_sql.substr(seg_start, i - seg_start)));
            seg_start = i + 1;
        }
    }
    defs.push_back(trim(raw_sql.substr(seg_start, body_end - seg_start)));

    for (const auto& def : defs) {
        std::string upper_def = to_upper(def);

        // Check for table-level CHECK constraint
        // Could be: CHECK(...) or CONSTRAINT name CHECK(...)
        bool is_check = false;
        CheckConstraint chk;

        if (starts_with(upper_def, "CHECK")) {
            is_check = true;
            // Extract expression from CHECK(...)
            auto paren = def.find('(');
            if (paren != std::string::npos) {
                // Find matching close paren
                int d = 0;
                size_t expr_end = std::string::npos;
                for (size_t i = paren; i < def.size(); ++i) {
                    if (def[i] == '(') ++d;
                    else if (def[i] == ')') {
                        --d;
                        if (d == 0) { expr_end = i; break; }
                    }
                }
                if (expr_end != std::string::npos)
                    chk.expression = trim(def.substr(paren + 1, expr_end - paren - 1));
            }
        } else if (starts_with(upper_def, "CONSTRAINT")) {
            // CONSTRAINT name CHECK/PRIMARY KEY/FOREIGN KEY(...)
            auto check_pos = upper_def.find("CHECK");
            if (check_pos != std::string::npos) {
                is_check = true;
                // Extract constraint name: between CONSTRAINT and CHECK
                chk.name = strip_quotes(trim(def.substr(10, check_pos - 10)));
                // Extract expression
                auto paren = def.find('(', check_pos);
                if (paren != std::string::npos) {
                    int d = 0;
                    size_t expr_end = std::string::npos;
                    for (size_t i = paren; i < def.size(); ++i) {
                        if (def[i] == '(') ++d;
                        else if (def[i] == ')') {
                            --d;
                            if (d == 0) { expr_end = i; break; }
                        }
                    }
                    if (expr_end != std::string::npos)
                        chk.expression = trim(def.substr(paren + 1, expr_end - paren - 1));
                }
            } else {
                auto pk_pos = upper_def.find("PRIMARY KEY");
                auto fk_pos = upper_def.find("FOREIGN KEY");
                if (pk_pos != std::string::npos) {
                    result.pk_constraint_name =
                        strip_quotes(trim(def.substr(10, pk_pos - 10)));
                } else if (fk_pos != std::string::npos) {
                    std::string name_part =
                        strip_quotes(trim(def.substr(10, fk_pos - 10)));
                    // Extract from_columns from FOREIGN KEY(col1, col2)
                    auto paren = def.find('(', fk_pos);
                    if (paren != std::string::npos) {
                        int d = 0;
                        size_t cols_end = std::string::npos;
                        for (size_t i = paren; i < def.size(); ++i) {
                            if (def[i] == '(') ++d;
                            else if (def[i] == ')') {
                                --d;
                                if (d == 0) { cols_end = i; break; }
                            }
                        }
                        if (cols_end != std::string::npos) {
                            std::string cols_str = def.substr(paren + 1, cols_end - paren - 1);
                            std::string key;
                            std::istringstream css(cols_str);
                            std::string col;
                            bool first = true;
                            while (std::getline(css, col, ',')) {
                                if (!first) key += ',';
                                key += strip_quotes(trim(col));
                                first = false;
                            }
                            result.fk_constraint_names[key] = name_part;
                        }
                    }
                }
                continue;
            }
        }

        if (is_check) {
            result.checks.push_back(std::move(chk));
            continue;
        }

        // Check for column-level GENERATED ALWAYS AS (expr)
        auto gen_pos = upper_def.find("GENERATED ALWAYS AS");
        if (gen_pos != std::string::npos) {
            // Extract column name (first token of the definition)
            auto first_space = def.find_first_of(" \t");
            std::string col_name;
            if (first_space != std::string::npos)
                col_name = def.substr(0, first_space);
            else
                col_name = def;
            col_name = strip_quotes(col_name);

            // Find the expression in parens after GENERATED ALWAYS AS
            auto paren = def.find('(', gen_pos);
            if (paren != std::string::npos) {
                int d = 0;
                size_t expr_end = std::string::npos;
                for (size_t i = paren; i < def.size(); ++i) {
                    if (def[i] == '(') ++d;
                    else if (def[i] == ')') {
                        --d;
                        if (d == 0) { expr_end = i; break; }
                    }
                }
                if (expr_end != std::string::npos)
                    result.generated_exprs[col_name] =
                        trim(def.substr(paren + 1, expr_end - paren - 1));
            }
        }
    }

    return result;
}

// Extract module name and args from a CREATE VIRTUAL TABLE statement.
// Input: "CREATE VIRTUAL TABLE [IF NOT EXISTS] <name> USING <module>[(<args>)]"
// Output: (module, args). Returns ("", "") if the statement can't be parsed.
struct VirtualTableSpec {
    std::string module;
    std::string args;
};

VirtualTableSpec parse_virtual_table_sql(const std::string& raw_sql) {
    VirtualTableSpec spec;
    std::string upper = to_upper(raw_sql);
    auto using_pos = upper.find(" USING ");
    if (using_pos == std::string::npos) return spec;

    // Skip past " USING " to the module name.
    size_t pos = using_pos + 7;
    while (pos < raw_sql.size() &&
           std::isspace(static_cast<unsigned char>(raw_sql[pos]))) ++pos;
    size_t module_start = pos;
    while (pos < raw_sql.size() && raw_sql[pos] != '(' &&
           !std::isspace(static_cast<unsigned char>(raw_sql[pos]))) ++pos;
    spec.module = raw_sql.substr(module_start, pos - module_start);

    // Optional args inside the outermost parens. CREATE VIRTUAL TABLE allows
    // omitting them entirely (e.g. "USING fts4"), in which case args stays "".
    while (pos < raw_sql.size() && raw_sql[pos] != '(' &&
           std::isspace(static_cast<unsigned char>(raw_sql[pos]))) ++pos;
    if (pos >= raw_sql.size() || raw_sql[pos] != '(') return spec;

    size_t args_start = pos + 1;
    int depth = 1;
    bool in_string = false;
    char quote = 0;
    ++pos;
    while (pos < raw_sql.size() && depth > 0) {
        char c = raw_sql[pos];
        if (in_string) {
            if (c == quote) {
                if (pos + 1 < raw_sql.size() && raw_sql[pos + 1] == quote)
                    ++pos; // escaped quote inside string literal
                else
                    in_string = false;
            }
        } else {
            if (c == '\'' || c == '"') { in_string = true; quote = c; }
            else if (c == '(') ++depth;
            else if (c == ')') {
                --depth;
                if (depth == 0) break;
            }
        }
        ++pos;
    }
    spec.args = raw_sql.substr(args_start, pos - args_start);
    return spec;
}

// True if a sqlite_master row whose `sql` starts with these tokens describes
// a virtual table (i.e. was created via CREATE VIRTUAL TABLE).
bool is_virtual_table_sql(const std::string& sql) {
    auto trimmed = trim(sql);
    if (trimmed.size() < 21) return false; // "CREATE VIRTUAL TABLE "
    auto upper = to_upper(trimmed.substr(0, 21));
    return upper == "CREATE VIRTUAL TABLE ";
}

// Shadow-table name suffixes per virtual-table module. SQLite materializes
// these as real CREATE TABLE rows in sqlite_master (with full SQL, not NULL
// sql) but they are module-internal — the user never declares them. We
// filter them during extract so they don't show up as regular tables and
// trigger spurious DROP ops on the next diff.
//
// To support a new module, add its shadow suffixes here. An unknown module's
// shadows will leak through as regular tables (visible failure mode — easy
// to spot) rather than silently corrupting the parent vtable on apply.
const std::map<std::string, std::vector<std::string>>& shadow_suffixes() {
    static const std::map<std::string, std::vector<std::string>> kSuffixes = {
        {"FTS5", {"_data", "_idx", "_content", "_docsize", "_config"}},
        {"FTS4", {"_content", "_segments", "_segdir", "_docsize", "_stat"}},
        {"FTS3", {"_content", "_segments", "_segdir"}},
        {"RTREE", {"_node", "_rowid", "_parent"}},
    };
    return kSuffixes;
}

// Parse table options after the closing ')' of CREATE TABLE.
// Returns (without_rowid, strict).
std::pair<bool, bool> parse_table_options(const std::string& raw_sql) {
    bool without_rowid = false;
    bool strict = false;

    // Find the last ')' at depth 0
    int depth = 0;
    size_t close_paren = std::string::npos;
    for (size_t i = 0; i < raw_sql.size(); ++i) {
        if (raw_sql[i] == '(') ++depth;
        else if (raw_sql[i] == ')') {
            --depth;
            if (depth == 0) { close_paren = i; break; }
        }
    }
    if (close_paren == std::string::npos || close_paren + 1 >= raw_sql.size())
        return {without_rowid, strict};

    std::string tail = raw_sql.substr(close_paren + 1);
    // Split by comma
    std::istringstream iss(tail);
    std::string token;
    while (std::getline(iss, token, ',')) {
        std::string t = to_upper(trim(token));
        if (t == "WITHOUT ROWID") without_rowid = true;
        else if (t == "STRICT") strict = true;
    }

    return {without_rowid, strict};
}

} // namespace

Schema extract(sqlite3* db) {
    Schema schema;

    // Query sqlite_master for all user-defined objects.
    Statement master_stmt(db,
        "SELECT type, name, tbl_name, sql FROM sqlite_master "
        "WHERE type IN ('table', 'index', 'view', 'trigger') "
        "AND name NOT LIKE 'sqlite_%' "
        "AND name != '_sqlift_state' "
        "ORDER BY type, name");

    struct MasterRow {
        std::string type, name, tbl_name, sql;
    };
    std::vector<MasterRow> rows;
    while (master_stmt.step()) {
        rows.push_back({
            master_stmt.column_text(0),
            master_stmt.column_text(1),
            master_stmt.column_text(2),
            master_stmt.column_text(3),
        });
    }

    // First pass: identify virtual tables and the set of shadow-table names
    // they generate. FTS5 et al. materialize shadows as real CREATE TABLE
    // rows in sqlite_master with non-NULL sql, indistinguishable from user
    // tables by row shape alone — we have to know the module's shadow
    // naming convention.
    std::set<std::string> shadow_table_names;
    for (const auto& row : rows) {
        if (row.type != "table") continue;
        if (!is_virtual_table_sql(row.sql)) continue;
        auto spec = parse_virtual_table_sql(row.sql);
        auto it = shadow_suffixes().find(to_upper(spec.module));
        if (it == shadow_suffixes().end()) continue;
        for (const auto& suffix : it->second)
            shadow_table_names.insert(row.name + suffix);
    }

    for (const auto& row : rows) {
        if (row.type == "table") {
            // Module-internal shadow tables: filtered by name based on the
            // parent vtable's module. A user table named `foo_data_real` is
            // NOT in this set (only the exact shadow suffixes for the
            // module match), so it survives extraction normally.
            if (shadow_table_names.count(row.name)) continue;

            if (is_virtual_table_sql(row.sql)) {
                VirtualTable vt;
                vt.name = row.name;
                vt.raw_sql = row.sql;
                auto spec = parse_virtual_table_sql(row.sql);
                vt.module = std::move(spec.module);
                vt.args = std::move(spec.args);
                schema.virtual_tables[vt.name] = std::move(vt);
                continue;
            }

            Table table;
            table.name = row.name;
            table.raw_sql = row.sql;

            // Detect WITHOUT ROWID and STRICT from table options
            auto [wor, strict_flag] = parse_table_options(row.sql);
            table.without_rowid = wor;
            table.strict = strict_flag;

            // Columns via PRAGMA table_xinfo (includes generated column info)
            Statement col_stmt(db,
                "PRAGMA table_xinfo(" + quote_id(row.name) + ")");
            while (col_stmt.step()) {
                Column col;
                col.name = col_stmt.column_text(1);
                col.type = to_upper(col_stmt.column_text(2));
                col.notnull = col_stmt.column_int(3) != 0;
                col.default_value = col_stmt.column_text(4);
                col.pk = static_cast<int>(col_stmt.column_int(5));
                auto hidden = col_stmt.column_int(6);
                if (hidden != 0 && hidden != 2 && hidden != 3)
                    throw ExtractError("Unsupported generated column type: " + std::to_string(hidden));
                col.generated = static_cast<GeneratedType>(hidden);
                table.columns.push_back(std::move(col));
            }

            // Collation via sqlite3_table_column_metadata
            for (auto& col : table.columns) {
                const char* collation = nullptr;
                int rc = sqlite3_table_column_metadata(
                    db, nullptr, row.name.c_str(), col.name.c_str(),
                    nullptr, &collation, nullptr, nullptr, nullptr);
                if (rc == SQLITE_OK && collation) {
                    std::string coll = collation;
                    if (to_upper(coll) != "BINARY")
                        col.collation = to_upper(coll);
                }
            }

            // Parse CHECK constraints and GENERATED expressions from raw_sql
            auto parsed = parse_create_table_body(row.sql);
            table.check_constraints = std::move(parsed.checks);
            for (auto& col : table.columns) {
                auto it = parsed.generated_exprs.find(col.name);
                if (it != parsed.generated_exprs.end())
                    col.generated_expr = it->second;
            }

            // Foreign keys via PRAGMA foreign_key_list
            Statement fk_stmt(db,
                "PRAGMA foreign_key_list(" + quote_id(row.name) + ")");
            // FK rows are grouped by id (seq=0 starts a new FK).
            std::map<int, ForeignKey> fk_map;
            while (fk_stmt.step()) {
                int id = static_cast<int>(fk_stmt.column_int(0));
                int seq = static_cast<int>(fk_stmt.column_int(1));
                if (seq == 0) {
                    ForeignKey fk;
                    fk.to_table = fk_stmt.column_text(2);
                    fk.on_update = to_upper(fk_stmt.column_text(5));
                    fk.on_delete = to_upper(fk_stmt.column_text(6));
                    fk_map[id] = std::move(fk);
                }
                fk_map[id].from_columns.push_back(fk_stmt.column_text(3));
                fk_map[id].to_columns.push_back(fk_stmt.column_text(4));
            }
            for (auto& [_, fk] : fk_map)
                table.foreign_keys.push_back(std::move(fk));

            // Populate constraint names from parsed raw_sql
            table.pk_constraint_name = std::move(parsed.pk_constraint_name);
            for (auto& fk : table.foreign_keys) {
                std::string key;
                for (size_t i = 0; i < fk.from_columns.size(); ++i) {
                    if (i > 0) key += ',';
                    key += fk.from_columns[i];
                }
                auto it = parsed.fk_constraint_names.find(key);
                if (it != parsed.fk_constraint_names.end())
                    fk.constraint_name = it->second;
            }

            schema.tables[table.name] = std::move(table);
        }
        else if (row.type == "index") {
            // Skip auto-indexes
            if (starts_with(row.name, "sqlite_autoindex_")) continue;
            // Auto-indexes have NULL sql
            if (row.sql.empty()) continue;

            Index idx;
            idx.name = row.name;
            idx.table_name = row.tbl_name;
            idx.raw_sql = row.sql;

            // Uniqueness via PRAGMA index_list (authoritative)
            {
                Statement il_stmt(db,
                    "PRAGMA index_list(" + quote_id(row.tbl_name) + ")");
                while (il_stmt.step()) {
                    if (il_stmt.column_text(1) == row.name) {
                        idx.unique = il_stmt.column_int(2) != 0;
                        break;
                    }
                }
            }

            // Columns via PRAGMA index_info
            Statement idx_info(db,
                "PRAGMA index_info(" + quote_id(row.name) + ")");
            while (idx_info.step()) {
                std::string col_name = idx_info.column_text(2);
                if (col_name.empty()) {
                    // Expression index — extract from raw SQL
                    col_name = "<expr>";
                }
                idx.columns.push_back(std::move(col_name));
            }

            // Partial index WHERE clause: extract from raw SQL.
            // Uses rfind to find the last WHERE, then checks it's at
            // top-level (not inside parentheses or string literals).
            auto upper_sql = to_upper(row.sql);
            auto where_pos = upper_sql.rfind("WHERE");
            if (where_pos != std::string::npos) {
                int paren_depth = 0;
                bool in_string = false;
                char string_char = 0;
                for (size_t i = 0; i < where_pos; ++i) {
                    char c = row.sql[i];
                    if (in_string) {
                        if (c == string_char) {
                            if (i + 1 < where_pos && row.sql[i + 1] == string_char)
                                ++i; // escaped quote
                            else
                                in_string = false;
                        }
                    } else {
                        if (c == '\'' || c == '"') {
                            in_string = true;
                            string_char = c;
                        } else if (c == '(') {
                            ++paren_depth;
                        } else if (c == ')') {
                            --paren_depth;
                        }
                    }
                }
                if (paren_depth == 0) {
                    idx.where_clause = row.sql.substr(where_pos + 6);
                    // Trim leading/trailing whitespace
                    auto start = idx.where_clause.find_first_not_of(" \t\n\r");
                    auto end = idx.where_clause.find_last_not_of(" \t\n\r");
                    if (start != std::string::npos)
                        idx.where_clause = idx.where_clause.substr(start, end - start + 1);
                }
            }

            schema.indexes[idx.name] = std::move(idx);
        }
        else if (row.type == "view") {
            View view;
            view.name = row.name;
            view.sql = row.sql;
            schema.views[view.name] = std::move(view);
        }
        else if (row.type == "trigger") {
            Trigger trigger;
            trigger.name = row.name;
            trigger.table_name = row.tbl_name;
            trigger.sql = row.sql;
            schema.triggers[trigger.name] = std::move(trigger);
        }
    }

    return schema;
}


// --- parse.cpp ---



Schema parse(const std::string& sql) {
    Database db(":memory:");

    try {
        db.exec(sql);
    } catch (const Error& e) {
        throw ParseError(std::string("Failed to parse schema SQL: ") + e.what());
    }

    return extract(db);
}


// --- diff.cpp ---




namespace {

// Check if a column can be added via simple ALTER TABLE ADD COLUMN.
bool can_add_column(const Column& col) {
    // SQLite restrictions on ADD COLUMN:
    // - Cannot be PRIMARY KEY
    // - Must have DEFAULT or allow NULL if NOT NULL
    // - Cannot be a generated column
    if (col.pk != 0) return false;
    if (col.notnull && col.default_value.empty()) return false;
    if (col.generated != GeneratedType::Normal) return false;
    return true;
}

// Extract SQL references: tokenize SQL into identifiers and check against known names.
// Excludes the object's own name.
std::set<std::string> extract_sql_references(
    const std::string& sql, const std::string& own_name,
    const std::set<std::string>& known_names)
{
    std::set<std::string> refs;
    std::string word;
    for (size_t i = 0; i <= sql.size(); ++i) {
        char c = (i < sql.size()) ? sql[i] : '\0';
        if (std::isalnum(static_cast<unsigned char>(c)) || c == '_') {
            word += c;
        } else {
            if (!word.empty()) {
                if (word != own_name && known_names.count(word))
                    refs.insert(word);
                word.clear();
            }
        }
    }
    return refs;
}

// Topological sort using Kahn's algorithm.
// If reverse==true, returns reverse topological order (dependents first).
std::vector<std::string> topo_sort(
    const std::vector<std::string>& nodes,
    const std::map<std::string, std::set<std::string>>& deps,
    bool reverse = false)
{
    // Build in-degree map
    std::map<std::string, int> in_degree;
    std::map<std::string, std::set<std::string>> dependents;
    for (const auto& n : nodes) in_degree[n] = 0;

    for (const auto& n : nodes) {
        auto it = deps.find(n);
        if (it != deps.end()) {
            for (const auto& dep : it->second) {
                if (in_degree.count(dep)) {
                    in_degree[n]++;
                    dependents[dep].insert(n);
                }
            }
        }
    }

    std::vector<std::string> queue;
    for (const auto& n : nodes) {
        if (in_degree[n] == 0)
            queue.push_back(n);
    }
    // Sort queue for deterministic ordering
    std::sort(queue.begin(), queue.end());

    std::vector<std::string> result;
    size_t front = 0;
    while (front < queue.size()) {
        std::string n = queue[front++];
        result.push_back(n);
        if (dependents.count(n)) {
            std::vector<std::string> newly_free;
            for (const auto& dep : dependents[n]) {
                if (--in_degree[dep] == 0)
                    newly_free.push_back(dep);
            }
            std::sort(newly_free.begin(), newly_free.end());
            for (auto& nf : newly_free)
                queue.push_back(std::move(nf));
        }
    }

    if (result.size() != nodes.size())
        throw DiffError("Circular dependency detected among views/triggers");

    if (reverse)
        std::reverse(result.begin(), result.end());

    return result;
}

// Check if the only difference is columns appended at the end (AddColumn fast path).
bool is_append_only(const Table& current, const Table& desired) {
    // All existing columns must be unchanged
    if (desired.columns.size() <= current.columns.size()) return false;
    for (size_t i = 0; i < current.columns.size(); ++i) {
        if (!(current.columns[i] == desired.columns[i])) return false;
    }
    // Foreign keys must be unchanged
    if (current.foreign_keys != desired.foreign_keys) return false;
    // CHECK constraints must be unchanged
    if (current.check_constraints != desired.check_constraints) return false;
    // WITHOUT ROWID must be unchanged
    if (current.without_rowid != desired.without_rowid) return false;
    // STRICT must be unchanged
    if (current.strict != desired.strict) return false;
    // All new columns must be addable
    for (size_t i = current.columns.size(); i < desired.columns.size(); ++i) {
        if (!can_add_column(desired.columns[i])) return false;
    }
    return true;
}

// Build an ADD COLUMN SQL statement.
std::string add_column_sql(const std::string& table_name, const Column& col) {
    std::ostringstream oss;
    oss << "ALTER TABLE " << quote_id(table_name)
        << " ADD COLUMN " << quote_id(col.name);
    if (!col.type.empty()) oss << ' ' << col.type;
    if (!col.collation.empty()) oss << " COLLATE " << col.collation;
    if (col.notnull) oss << " NOT NULL";
    if (!col.default_value.empty()) oss << " DEFAULT " << col.default_value;
    return oss.str();
}

bool schema_empty(const Schema& s) {
    return s.tables.empty() && s.indexes.empty() && s.views.empty()
        && s.triggers.empty() && s.virtual_tables.empty();
}

std::string create_table_sql_from_ir(const Table& table) {
    if (!table.raw_sql.empty()) return table.raw_sql;
    std::ostringstream oss;
    oss << "CREATE TABLE " << quote_id(table.name) << " (";
    for (size_t i = 0; i < table.columns.size(); ++i) {
        if (i) oss << ", ";
        const auto& col = table.columns[i];
        oss << quote_id(col.name);
        if (!col.type.empty()) oss << ' ' << col.type;
        if (col.notnull) oss << " NOT NULL";
        if (!col.default_value.empty()) oss << " DEFAULT " << col.default_value;
    }
    std::vector<std::string> pk;
    for (const auto& col : table.columns)
        if (col.pk > 0) pk.push_back(col.name);
    if (!pk.empty()) {
        oss << ", PRIMARY KEY (";
        for (size_t i = 0; i < pk.size(); ++i) {
            if (i) oss << ", ";
            oss << quote_id(pk[i]);
        }
        oss << ")";
    }
    for (const auto& fk : table.foreign_keys) {
        oss << ", ";
        if (!fk.constraint_name.empty())
            oss << "CONSTRAINT " << quote_id(fk.constraint_name) << ' ';
        oss << "FOREIGN KEY (";
        for (size_t i = 0; i < fk.from_columns.size(); ++i) {
            if (i) oss << ", ";
            oss << quote_id(fk.from_columns[i]);
        }
        oss << ") REFERENCES " << quote_id(fk.to_table) << " (";
        for (size_t i = 0; i < fk.to_columns.size(); ++i) {
            if (i) oss << ", ";
            oss << quote_id(fk.to_columns[i]);
        }
        oss << ")";
        if (!fk.on_update.empty() && fk.on_update != "NO ACTION")
            oss << " ON UPDATE " << fk.on_update;
        if (!fk.on_delete.empty() && fk.on_delete != "NO ACTION")
            oss << " ON DELETE " << fk.on_delete;
    }
    for (const auto& chk : table.check_constraints) {
        oss << ", ";
        if (!chk.name.empty())
            oss << "CONSTRAINT " << quote_id(chk.name) << ' ';
        oss << "CHECK (" << chk.expression << ")";
    }
    oss << ")";
    return oss.str();
}

std::vector<Operation> emit_pg_table_ops(const Table& current,
                       const Table& desired,
                       const std::vector<std::string>& violations) {
    std::vector<Operation> ops;
    const std::string& name = desired.name;
    bool data_dependent = !violations.empty();
    auto annotate = [&](std::string desc) {
        if (!data_dependent) return desc;
        desc += " [data-dependent:";
        for (const auto& v : violations) desc += " " + v + ";";
        desc += "]";
        return desc;
    };

    std::map<std::string, const Column*> cur_cols, des_cols;
    for (const auto& c : current.columns) cur_cols[c.name] = &c;
    for (const auto& c : desired.columns) des_cols[c.name] = &c;

    for (const auto& [cname, col] : cur_cols) {
        if (des_cols.count(cname)) continue;
        ops.push_back({
            .type = OpType::DropColumn,
            .object_name = name,
            .description = annotate("Drop column " + cname + " from " + name),
            .sql = {"ALTER TABLE " + quote_id(name) +
                    " DROP COLUMN " + quote_id(cname)},
            .destructive = true,
        });
    }

    for (const auto& col : desired.columns) {
        if (cur_cols.count(col.name)) continue;
        bool dd = col.notnull && col.default_value.empty() && col.pk == 0;
        ops.push_back({
            .type = OpType::AddColumn,
            .object_name = name,
            .description = annotate("Add column " + col.name + " to " + name),
            .sql = {add_column_sql(name, col)},
            .data_dependent = dd || data_dependent,
        });
    }

    for (const auto& col : desired.columns) {
        auto it = cur_cols.find(col.name);
        if (it == cur_cols.end()) continue;
        const Column& cur = *it->second;
        if (cur == col) continue;
        if (cur.type != col.type) {
            ops.push_back({
                .type = OpType::AlterColumnType,
                .object_name = name,
                .description = annotate("Alter column " + col.name +
                                        " type on " + name),
                .sql = {"ALTER TABLE " + quote_id(name) + " ALTER COLUMN " +
                        quote_id(col.name) + " TYPE " + col.type},
                .requires_rebuild = true,
            });
        }
        if (cur.notnull && !col.notnull) {
            ops.push_back({
                .type = OpType::DropNotNull,
                .object_name = name,
                .description = annotate("Drop NOT NULL on " + name + "." +
                                        col.name),
                .sql = {"ALTER TABLE " + quote_id(name) + " ALTER COLUMN " +
                        quote_id(col.name) + " DROP NOT NULL"},
                .loosens_only = true,
            });
        }
        if (!cur.notnull && col.notnull) {
            ops.push_back({
                .type = OpType::SetNotNull,
                .object_name = name,
                .description = annotate("Set NOT NULL on " + name + "." +
                                        col.name),
                .sql = {"ALTER TABLE " + quote_id(name) + " ALTER COLUMN " +
                        quote_id(col.name) + " SET NOT NULL"},
                .data_dependent = true,
            });
        }
        if (cur.default_value != col.default_value) {
            if (col.default_value.empty()) {
                ops.push_back({
                    .type = OpType::DropDefault,
                    .object_name = name,
                    .description = annotate("Drop default on " + name + "." +
                                            col.name),
                    .sql = {"ALTER TABLE " + quote_id(name) + " ALTER COLUMN " +
                            quote_id(col.name) + " DROP DEFAULT"},
                });
            } else {
                ops.push_back({
                    .type = OpType::SetDefault,
                    .object_name = name,
                    .description = annotate("Set default on " + name + "." +
                                            col.name),
                    .sql = {"ALTER TABLE " + quote_id(name) + " ALTER COLUMN " +
                            quote_id(col.name) + " SET DEFAULT " +
                            col.default_value},
                });
            }
        }
    }

    for (const auto& fk : current.foreign_keys) {
        bool found = false;
        for (const auto& d : desired.foreign_keys)
            if (d == fk) { found = true; break; }
        if (found) continue;
        std::string cname = fk.constraint_name.empty()
            ? (name + "_" + fk.from_columns[0] + "_fkey")
            : fk.constraint_name;
        ops.push_back({
            .type = OpType::DropConstraint,
            .object_name = name,
            .description = annotate("Drop foreign key on " + name),
            .sql = {"ALTER TABLE " + quote_id(name) +
                    " DROP CONSTRAINT " + quote_id(cname)},
            .destructive = false,
            .loosens_only = true,
        });
    }
    for (const auto& fk : desired.foreign_keys) {
        bool found = false;
        for (const auto& c : current.foreign_keys)
            if (c == fk) { found = true; break; }
        if (found) continue;
        std::ostringstream oss;
        oss << "ALTER TABLE " << quote_id(name) << " ADD";
        if (!fk.constraint_name.empty())
            oss << " CONSTRAINT " << quote_id(fk.constraint_name);
        oss << " FOREIGN KEY (";
        for (size_t i = 0; i < fk.from_columns.size(); ++i) {
            if (i) oss << ", ";
            oss << quote_id(fk.from_columns[i]);
        }
        oss << ") REFERENCES " << quote_id(fk.to_table) << " (";
        for (size_t i = 0; i < fk.to_columns.size(); ++i) {
            if (i) oss << ", ";
            oss << quote_id(fk.to_columns[i]);
        }
        oss << ")";
        ops.push_back({
            .type = OpType::AddConstraint,
            .object_name = name,
            .description = annotate("Add foreign key on " + name),
            .sql = {oss.str()},
            .data_dependent = true,
        });
    }

    for (const auto& chk : current.check_constraints) {
        bool found = false;
        for (const auto& d : desired.check_constraints)
            if (d == chk) { found = true; break; }
        if (found || chk.name.empty()) continue;
        ops.push_back({
            .type = OpType::DropConstraint,
            .object_name = name,
            .description = annotate("Drop CHECK on " + name),
            .sql = {"ALTER TABLE " + quote_id(name) +
                    " DROP CONSTRAINT " + quote_id(chk.name)},
            .loosens_only = true,
        });
    }
    for (const auto& chk : desired.check_constraints) {
        bool found = false;
        for (const auto& c : current.check_constraints)
            if (c == chk) { found = true; break; }
        if (found) continue;
        std::ostringstream oss;
        oss << "ALTER TABLE " << quote_id(name) << " ADD";
        if (!chk.name.empty())
            oss << " CONSTRAINT " << quote_id(chk.name);
        oss << " CHECK (" << chk.expression << ")";
        ops.push_back({
            .type = OpType::AddConstraint,
            .object_name = name,
            .description = annotate("Add CHECK on " + name),
            .sql = {oss.str()},
            .data_dependent = true,
        });
    }
    return ops;
}

// Build the SQL for a 12-step table rebuild.
std::vector<std::string> rebuild_table_sql(
    const Table& current, const Table& desired,
    const Schema& desired_schema)
{
    std::vector<std::string> stmts;
    std::string tmp_name = quote_id(desired.name + "_sqlift_new");
    std::string tbl_name = quote_id(desired.name);

    // Step 1: Disable foreign keys
    stmts.push_back("PRAGMA foreign_keys=OFF");

    // Step 2: Begin transaction
    stmts.push_back("SAVEPOINT sqlift_rebuild");

    // Step 3: Create new table with desired schema
    stmts.push_back(desired.raw_sql);
    // Replace the table name in the CREATE TABLE statement with the temp name.
    // The raw_sql has the real name; we need to create with the temp name.
    auto& create_stmt = stmts.back();
    // Replace first occurrence of table name after CREATE TABLE
    {
        std::string create_sql = desired.raw_sql;
        // Find the table name in the CREATE TABLE statement and replace with tmp name.
        // Reconstruct: CREATE TABLE <tmp_name> (rest...)
        auto paren_pos = create_sql.find('(');
        if (paren_pos != std::string::npos) {
            create_stmt = "CREATE TABLE " + tmp_name +
                          " " + create_sql.substr(paren_pos);
        }
    }

    // Step 4: Copy data from old table to new (common columns only).
    // Skip generated columns — they are computed and can't be inserted into.
    std::vector<std::string> common_cols;
    std::set<std::string> desired_col_names;
    std::set<std::string> generated_col_names;
    for (const auto& col : desired.columns) {
        desired_col_names.insert(col.name);
        if (col.generated != GeneratedType::Normal)
            generated_col_names.insert(col.name);
    }
    for (const auto& col : current.columns) {
        if (desired_col_names.count(col.name) && !generated_col_names.count(col.name))
            common_cols.push_back(quote_id(col.name));
    }
    if (!common_cols.empty()) {
        std::ostringstream oss;
        oss << "INSERT INTO " << tmp_name << " (";
        for (size_t i = 0; i < common_cols.size(); ++i) {
            if (i > 0) oss << ", ";
            oss << common_cols[i];
        }
        oss << ") SELECT ";
        for (size_t i = 0; i < common_cols.size(); ++i) {
            if (i > 0) oss << ", ";
            oss << common_cols[i];
        }
        oss << " FROM " << tbl_name;
        stmts.push_back(oss.str());
    }

    // Step 5: Drop old table
    stmts.push_back("DROP TABLE " + tbl_name);

    // Step 6: Rename new table
    stmts.push_back("ALTER TABLE " + tmp_name + " RENAME TO " + tbl_name);

    // Step 7: Recreate indexes for this table
    for (const auto& [idx_name, idx] : desired_schema.indexes) {
        if (idx.table_name == desired.name && !idx.raw_sql.empty()) {
            stmts.push_back(idx.raw_sql);
        }
    }

    // Step 8: Recreate triggers for this table
    for (const auto& [trig_name, trig] : desired_schema.triggers) {
        if (trig.table_name == desired.name && !trig.sql.empty()) {
            stmts.push_back(trig.sql);
        }
    }

    // Step 9: (content verification — skipped, sqlift uses FK check instead)

    // Step 10: FK check
    stmts.push_back("PRAGMA foreign_key_check(" + quote_id(desired.name) + ")");

    // Step 11: Release savepoint
    stmts.push_back("RELEASE SAVEPOINT sqlift_rebuild");

    // Step 12: Re-enable foreign keys
    stmts.push_back("PRAGMA foreign_keys=ON");

    return stmts;
}

// Describe what changed between two tables.
std::string describe_table_changes(const Table& current, const Table& desired) {
    std::ostringstream oss;
    oss << "Rebuild table " << desired.name << ":";

    // Find added/removed/changed columns
    std::set<std::string> current_cols, desired_cols;
    std::map<std::string, const Column*> current_col_map, desired_col_map;
    for (const auto& c : current.columns) {
        current_cols.insert(c.name);
        current_col_map[c.name] = &c;
    }
    for (const auto& c : desired.columns) {
        desired_cols.insert(c.name);
        desired_col_map[c.name] = &c;
    }

    for (const auto& name : desired_cols) {
        if (!current_cols.count(name))
            oss << " add column " << name << ";";
    }
    for (const auto& name : current_cols) {
        if (!desired_cols.count(name))
            oss << " drop column " << name << ";";
    }
    for (const auto& name : current_cols) {
        if (desired_cols.count(name)) {
            const auto* c = current_col_map[name];
            const auto* d = desired_col_map[name];
            if (!(*c == *d))
                oss << " modify column " << name << ";";
        }
    }

    if (current.foreign_keys != desired.foreign_keys)
        oss << " foreign keys changed;";
    if (current.check_constraints != desired.check_constraints)
        oss << " CHECK constraints changed;";
    if (current.without_rowid != desired.without_rowid)
        oss << " WITHOUT ROWID changed;";
    if (current.strict != desired.strict)
        oss << " STRICT changed;";

    return oss.str();
}

// True when every difference between current and desired is a strict
// relaxation: dropping a CHECK or FK, or making an existing NOT NULL
// column nullable. Used to gate the SQLIFT_ALLOW_LOOSEN policy.
bool rebuild_is_pure_loosening(const Table& current, const Table& desired) {
    // Same column set, same order, same type/default/PK; only NOT NULL
    // may relax (true -> false).
    if (current.columns.size() != desired.columns.size()) return false;
    for (size_t i = 0; i < current.columns.size(); ++i) {
        const Column& c = current.columns[i];
        const Column& d = desired.columns[i];
        if (c.name != d.name) return false;
        if (c.type != d.type) return false;
        if (c.collation != d.collation) return false;
        if (c.default_value != d.default_value) return false;
        if (c.pk != d.pk) return false;
        if (c.generated != d.generated) return false;
        // NOT NULL may relax (true -> false), never tighten (false -> true).
        if (!c.notnull && d.notnull) return false;
    }
    // FKs and CHECKs may be removed, never added or changed.
    for (const auto& fk : desired.foreign_keys) {
        bool found = false;
        for (const auto& cur_fk : current.foreign_keys) {
            if (cur_fk == fk) { found = true; break; }
        }
        if (!found) return false;
    }
    for (const auto& chk : desired.check_constraints) {
        bool found = false;
        for (const auto& cur_chk : current.check_constraints) {
            if (cur_chk == chk) { found = true; break; }
        }
        if (!found) return false;
    }
    // Table-level options must be unchanged.
    if (current.without_rowid != desired.without_rowid) return false;
    if (current.strict != desired.strict) return false;
    return true;
}

bool rebuild_is_destructive(const Table& current, const Table& desired) {
    std::set<std::string> desired_cols;
    for (const auto& c : desired.columns)
        desired_cols.insert(c.name);
    for (const auto& c : current.columns) {
        if (!desired_cols.count(c.name))
            return true; // Column removed
    }
    return false;
}

} // namespace

bool MigrationPlan::has_destructive_operations() const {
    return std::any_of(ops_.begin(), ops_.end(),
                       [](const Operation& op) { return op.destructive; });
}

bool MigrationPlan::has_rebuild_operations() const {
    return std::any_of(ops_.begin(), ops_.end(),
                       [](const Operation& op) { return op.requires_rebuild; });
}

MigrationPlan diff(const Schema& current, const Schema& desired) {
    MigrationPlan plan;

    if (current.dialect != desired.dialect &&
        !schema_empty(current) && !schema_empty(desired)) {
        throw DiffError("cannot diff " + current.dialect +
                        " schema against " + desired.dialect + " schema");
    }
    const bool is_pg = current.dialect == "postgres" ||
                       desired.dialect == "postgres";

    // Build known names for dependency analysis
    std::set<std::string> known_names;
    for (const auto& [n, _] : current.tables) known_names.insert(n);
    for (const auto& [n, _] : current.views) known_names.insert(n);
    for (const auto& [n, _] : desired.tables) known_names.insert(n);
    for (const auto& [n, _] : desired.views) known_names.insert(n);

    // --- Phase 1: Drop triggers that are removed or changed ---
    // Collect triggers to drop, then sort by reverse dependency order
    {
        std::vector<std::string> to_drop;
        std::map<std::string, bool> drop_destructive;
        for (const auto& [name, trig] : current.triggers) {
            auto it = desired.triggers.find(name);
            if (it == desired.triggers.end() || it->second.sql != trig.sql) {
                to_drop.push_back(name);
                drop_destructive[name] = (it == desired.triggers.end());
            }
        }
        // Build dependency graph for triggers being dropped
        std::set<std::string> drop_set(to_drop.begin(), to_drop.end());
        std::map<std::string, std::set<std::string>> deps;
        for (const auto& name : to_drop) {
            deps[name] = extract_sql_references(
                current.triggers.at(name).sql, name, known_names);
            // Only keep deps that are also being dropped
            std::set<std::string> filtered;
            for (const auto& d : deps[name])
                if (drop_set.count(d)) filtered.insert(d);
            deps[name] = std::move(filtered);
        }
        auto sorted = topo_sort(to_drop, deps, true);
        for (const auto& name : sorted) {
            std::string drop_sql = "DROP TRIGGER IF EXISTS " + quote_id(name);
            if (is_pg)
                drop_sql += " ON " +
                    quote_id(current.triggers.at(name).table_name);
            plan.ops_.push_back({
                .type = OpType::DropTrigger,
                .object_name = name,
                .description = "Drop trigger " + name,
                .sql = {std::move(drop_sql)},
                .destructive = drop_destructive[name],
            });
        }
    }

    // --- Phase 2: Drop views that are removed or changed ---
    // Sort by reverse dependency order (dependents dropped first)
    {
        std::vector<std::string> to_drop;
        std::map<std::string, bool> drop_destructive;
        for (const auto& [name, view] : current.views) {
            auto it = desired.views.find(name);
            if (it == desired.views.end() || it->second.sql != view.sql) {
                to_drop.push_back(name);
                drop_destructive[name] = (it == desired.views.end());
            }
        }
        std::map<std::string, std::set<std::string>> deps;
        for (const auto& name : to_drop) {
            deps[name] = extract_sql_references(
                current.views.at(name).sql, name, known_names);
        }
        auto sorted = topo_sort(to_drop, deps, true);
        for (const auto& name : sorted) {
            plan.ops_.push_back({
                .type = OpType::DropView,
                .object_name = name,
                .description = "Drop view " + name,
                .sql = {"DROP VIEW IF EXISTS " + quote_id(name)},
                .destructive = drop_destructive[name],
            });
        }
    }

    // --- Phase 3: Drop indexes that are removed or changed ---
    // Also drop indexes on tables that will be rebuilt (they get recreated in the rebuild).
    std::set<std::string> tables_to_rebuild;

    // Pre-scan to find which tables need rebuilding
    for (const auto& [name, table] : desired.tables) {
        auto it = current.tables.find(name);
        if (it != current.tables.end() && !(it->second == table)) {
            if (!is_pg && !is_append_only(it->second, table)) {
                tables_to_rebuild.insert(name);
            }
        }
    }

    for (const auto& [name, idx] : current.indexes) {
        auto it = desired.indexes.find(name);
        bool needs_drop = false;

        if (it == desired.indexes.end()) {
            needs_drop = true;
        } else if (!(it->second == idx)) {
            needs_drop = true;
        } else if (tables_to_rebuild.count(idx.table_name)) {
            // Index will be recreated as part of rebuild
            needs_drop = true;
        }

        if (needs_drop) {
            plan.ops_.push_back({
                .type = OpType::DropIndex,
                .object_name = name,
                .description = "Drop index " + name,
                .sql = {"DROP INDEX IF EXISTS " + quote_id(name)},
                .destructive = (it == desired.indexes.end()),
            });
        }
    }

    // --- Phase 4: Table operations ---

    // Create new tables
    for (const auto& [name, table] : desired.tables) {
        if (!current.tables.count(name)) {
            plan.ops_.push_back({
                .type = OpType::CreateTable,
                .object_name = name,
                .description = "Create table " + name,
                .sql = {create_table_sql_from_ir(table)},
                .destructive = false,
            });
        }
    }

    // Detect data-dependent changes across all modified tables. Each table
    // accumulates a list of violation messages; the resulting RebuildTable
    // op carries them in its description so the apply-time gate can
    // surface a useful error. We do NOT throw here -- the policy gate
    // belongs in apply().
    std::map<std::string, std::vector<std::string>> data_dependent_violations;
    for (const auto& [name, desired_table] : desired.tables) {
        auto it = current.tables.find(name);
        if (it == current.tables.end()) continue;
        const auto& current_table = it->second;
        if (current_table == desired_table) continue;

        std::vector<std::string> violations;
        std::map<std::string, const Column*> cur_col_map;
        for (const auto& col : current_table.columns)
            cur_col_map[col.name] = &col;

        // (a) Existing nullable column becomes NOT NULL.
        for (const auto& col : desired_table.columns) {
            auto cit = cur_col_map.find(col.name);
            if (cit != cur_col_map.end() && !cit->second->notnull && col.notnull) {
                violations.push_back(
                    "column '" + col.name +
                    "' changes from nullable to NOT NULL");
            }
        }

        // (b) New FK constraint on existing table.
        for (const auto& fk : desired_table.foreign_keys) {
            bool found = false;
            for (const auto& cur_fk : current_table.foreign_keys) {
                if (cur_fk == fk) { found = true; break; }
            }
            if (!found) {
                std::ostringstream oss;
                oss << "adds foreign key (";
                for (size_t i = 0; i < fk.from_columns.size(); ++i) {
                    if (i > 0) oss << ", ";
                    oss << fk.from_columns[i];
                }
                oss << ") references " << fk.to_table << "(";
                for (size_t i = 0; i < fk.to_columns.size(); ++i) {
                    if (i > 0) oss << ", ";
                    oss << fk.to_columns[i];
                }
                oss << ")";
                violations.push_back(oss.str());
            }
        }

        // (c) New CHECK constraint on existing table.
        for (const auto& chk : desired_table.check_constraints) {
            bool found = false;
            for (const auto& cur_chk : current_table.check_constraints) {
                if (cur_chk == chk) { found = true; break; }
            }
            if (!found) {
                violations.push_back(
                    std::string("adds CHECK constraint") +
                    (chk.name.empty() ? "" : " '" + chk.name + "'") +
                    " (" + chk.expression + ")");
            }
        }

        // (d) New NOT NULL column without DEFAULT.
        for (const auto& col : desired_table.columns) {
            if (cur_col_map.find(col.name) == cur_col_map.end() &&
                col.notnull && col.default_value.empty() && col.pk == 0) {
                violations.push_back(
                    "new column '" + col.name +
                    "' is NOT NULL without DEFAULT");
            }
        }

        if (!violations.empty())
            data_dependent_violations[name] = std::move(violations);
    }

    // Modify existing tables
    for (const auto& [name, desired_table] : desired.tables) {
        auto it = current.tables.find(name);
        if (it == current.tables.end()) continue;
        const auto& current_table = it->second;

        if (current_table == desired_table) continue;

        if (is_append_only(current_table, desired_table)) {
            // AddColumn fast path
            for (size_t i = current_table.columns.size();
                 i < desired_table.columns.size(); ++i)
            {
                plan.ops_.push_back({
                    .type = OpType::AddColumn,
                    .object_name = name,
                    .description = "Add column " + desired_table.columns[i].name +
                                   " to " + name,
                    .sql = {add_column_sql(name, desired_table.columns[i])},
                    .destructive = false,
                });
            }
        } else if (is_pg) {
            auto vit = data_dependent_violations.find(name);
            std::vector<std::string> violations;
            if (vit != data_dependent_violations.end())
                violations = vit->second;
            for (auto& op : emit_pg_table_ops(current_table, desired_table,
                                              violations))
                plan.ops_.push_back(std::move(op));
        } else {
            // Full rebuild
            auto vit = data_dependent_violations.find(name);
            bool data_dependent = vit != data_dependent_violations.end();
            std::string description = describe_table_changes(current_table, desired_table);
            if (data_dependent) {
                description += " [data-dependent:";
                for (const auto& v : vit->second)
                    description += " " + v + ";";
                description += "]";
            }
            plan.ops_.push_back({
                .type = OpType::RebuildTable,
                .object_name = name,
                .description = std::move(description),
                .sql = rebuild_table_sql(current_table, desired_table, desired),
                .destructive = rebuild_is_destructive(current_table, desired_table),
                .requires_rebuild = true,
                .loosens_only = rebuild_is_pure_loosening(current_table, desired_table),
                .data_dependent = data_dependent,
            });
        }
    }

    // Drop removed tables
    for (const auto& [name, table] : current.tables) {
        if (!desired.tables.count(name)) {
            plan.ops_.push_back({
                .type = OpType::DropTable,
                .object_name = name,
                .description = "Drop table " + name,
                .sql = {"DROP TABLE IF EXISTS " + quote_id(name)},
                .destructive = true,
            });
        }
    }

    // --- Phase 4b: Virtual table operations ---
    // Virtual tables have no in-place modification path. Any change
    // (module name, args, or removal) is drop+recreate, always destructive
    // for the drop half. Drops emitted before creates so a recreate works
    // when the name is reused.
    for (const auto& [name, vt] : current.virtual_tables) {
        auto it = desired.virtual_tables.find(name);
        if (it == desired.virtual_tables.end() || !(it->second == vt)) {
            plan.ops_.push_back({
                .type = OpType::DropVirtualTable,
                .object_name = name,
                .description = "Drop virtual table " + name,
                .sql = {"DROP TABLE IF EXISTS " + quote_id(name)},
                .destructive = true,
            });
        }
    }
    for (const auto& [name, vt] : desired.virtual_tables) {
        auto it = current.virtual_tables.find(name);
        if (it == current.virtual_tables.end() || !(it->second == vt)) {
            plan.ops_.push_back({
                .type = OpType::CreateVirtualTable,
                .object_name = name,
                .description = "Create virtual table " + name +
                               " USING " + vt.module,
                .sql = {vt.raw_sql},
                .destructive = false,
            });
        }
    }

    // --- Phase 5: Create indexes (not part of rebuilds) ---
    for (const auto& [name, idx] : desired.indexes) {
        auto it = current.indexes.find(name);
        bool needs_create = false;

        if (it == current.indexes.end()) {
            needs_create = true;
        } else if (!(it->second == idx)) {
            needs_create = true;
        }

        // Skip indexes on rebuilt tables (they were recreated in the rebuild)
        if (tables_to_rebuild.count(idx.table_name)) continue;

        if (needs_create) {
            plan.ops_.push_back({
                .type = OpType::CreateIndex,
                .object_name = name,
                .description = "Create index " + name + " on " + idx.table_name,
                .sql = {idx.raw_sql},
                .destructive = false,
            });
        }
    }

    // --- Phase 6: Create views ---
    // Sort by topological order (dependencies created first)
    {
        std::vector<std::string> to_create;
        for (const auto& [name, view] : desired.views) {
            auto it = current.views.find(name);
            if (it == current.views.end() || it->second.sql != view.sql) {
                to_create.push_back(name);
            }
        }
        // Build known names from desired schema for create ordering
        std::set<std::string> desired_known;
        for (const auto& [n, _] : desired.tables) desired_known.insert(n);
        for (const auto& [n, _] : desired.views) desired_known.insert(n);

        std::map<std::string, std::set<std::string>> deps;
        for (const auto& name : to_create) {
            deps[name] = extract_sql_references(
                desired.views.at(name).sql, name, desired_known);
        }
        auto sorted = topo_sort(to_create, deps, false);
        for (const auto& name : sorted) {
            plan.ops_.push_back({
                .type = OpType::CreateView,
                .object_name = name,
                .description = "Create view " + name,
                .sql = {desired.views.at(name).sql},
                .destructive = false,
            });
        }
    }

    // --- Phase 7: Create triggers ---
    // Sort by topological order (dependencies created first)
    {
        std::vector<std::string> to_create;
        for (const auto& [name, trig] : desired.triggers) {
            auto it = current.triggers.find(name);
            if (it == current.triggers.end() || it->second.sql != trig.sql) {
                to_create.push_back(name);
            }
        }
        std::set<std::string> desired_known;
        for (const auto& [n, _] : desired.tables) desired_known.insert(n);
        for (const auto& [n, _] : desired.views) desired_known.insert(n);
        for (const auto& [n, _] : desired.triggers) desired_known.insert(n);

        std::map<std::string, std::set<std::string>> deps;
        for (const auto& name : to_create) {
            deps[name] = extract_sql_references(
                desired.triggers.at(name).sql, name, desired_known);
        }
        auto sorted = topo_sort(to_create, deps, false);
        for (const auto& name : sorted) {
            plan.ops_.push_back({
                .type = OpType::CreateTrigger,
                .object_name = name,
                .description = "Create trigger " + name,
                .sql = {desired.triggers.at(name).sql},
                .destructive = false,
            });
        }
    }

    plan.warnings_ = detect_redundant_indexes(desired);

    return plan;
}

std::vector<Warning> detect_redundant_indexes(const Schema& schema) {
    std::vector<Warning> warnings;
    std::set<std::string> pk_flagged; // Indexes already flagged as PK-duplicate.

    // Group indexes by table.
    std::map<std::string, std::vector<const Index*>> by_table;
    for (const auto& [name, idx] : schema.indexes)
        by_table[idx.table_name].push_back(&idx);

    for (const auto& [table_name, table] : schema.tables) {
        // Build PK column list ordered by pk position.
        std::vector<std::pair<int, std::string>> pk_pairs;
        for (const auto& col : table.columns) {
            if (col.pk > 0)
                pk_pairs.push_back({col.pk, col.name});
        }
        std::sort(pk_pairs.begin(), pk_pairs.end());
        std::vector<std::string> pk_columns;
        for (const auto& [pos, name] : pk_pairs)
            pk_columns.push_back(name);

        auto it = by_table.find(table_name);
        if (it == by_table.end()) continue;
        const auto& indexes = it->second;

        // --- PK-duplicate detection ---
        if (!pk_columns.empty()) {
            for (const auto* idx : indexes) {
                // Partial indexes can't be PK-duplicates (PK has no WHERE).
                if (!idx->where_clause.empty()) continue;

                if (idx->columns.size() > pk_columns.size()) continue;

                // Check if idx->columns is a prefix of pk_columns.
                if (!std::equal(idx->columns.begin(), idx->columns.end(),
                                pk_columns.begin())) continue;

                bool exact_match = (idx->columns.size() == pk_columns.size());
                if (exact_match || !idx->unique) {
                    // Exact PK match: always redundant (PK implies uniqueness).
                    // Strict prefix + non-unique: redundant (PK index covers lookups).
                    // Strict prefix + unique: NOT redundant (tighter constraint).
                    pk_flagged.insert(idx->name);
                    warnings.push_back({
                        .type = WarningType::RedundantIndex,
                        .message = "Index '" + idx->name + "' on table '" +
                                   table_name + "' is redundant: columns are " +
                                   (exact_match ? "identical to" : "a prefix of") +
                                   " PRIMARY KEY",
                        .index_name = idx->name,
                        .covered_by = "PRIMARY KEY",
                        .table_name = table_name,
                    });
                }
            }
        }

        // --- Prefix-duplicate detection ---
        for (const auto* shorter : indexes) {
            if (pk_flagged.count(shorter->name)) continue;

            for (const auto* longer : indexes) {
                if (shorter == longer) continue;
                if (pk_flagged.count(longer->name)) continue;
                if (shorter->columns.size() >= longer->columns.size()) continue;
                if (shorter->where_clause != longer->where_clause) continue;

                // Check if shorter->columns is a strict prefix of longer->columns.
                if (!std::equal(shorter->columns.begin(), shorter->columns.end(),
                                longer->columns.begin())) continue;

                // Non-unique shorter: always redundant (longer covers lookups).
                // Unique shorter: enforces a tighter constraint, NOT redundant.
                if (!shorter->unique) {
                    warnings.push_back({
                        .type = WarningType::RedundantIndex,
                        .message = "Index '" + shorter->name + "' on table '" +
                                   table_name + "' is redundant: columns are a prefix of index '" +
                                   longer->name + "'",
                        .index_name = shorter->name,
                        .covered_by = longer->name,
                        .table_name = table_name,
                    });
                    break; // One warning per redundant index.
                }
            }
        }

        // --- Exact-duplicate detection (same columns, same WHERE) ---
        for (size_t i = 0; i < indexes.size(); ++i) {
            if (pk_flagged.count(indexes[i]->name)) continue;

            for (size_t j = i + 1; j < indexes.size(); ++j) {
                if (pk_flagged.count(indexes[j]->name)) continue;
                if (indexes[i]->columns != indexes[j]->columns) continue;
                if (indexes[i]->where_clause != indexes[j]->where_clause) continue;

                // Same columns, same WHERE. Determine which is redundant.
                const Index* redundant = nullptr;
                const Index* keeper = nullptr;

                if (indexes[i]->unique == indexes[j]->unique) {
                    // Same uniqueness: flag the later one alphabetically.
                    if (indexes[i]->name < indexes[j]->name) {
                        redundant = indexes[j];
                        keeper = indexes[i];
                    } else {
                        redundant = indexes[i];
                        keeper = indexes[j];
                    }
                } else if (!indexes[i]->unique) {
                    // i is non-unique, j is unique: i is redundant.
                    redundant = indexes[i];
                    keeper = indexes[j];
                } else {
                    // i is unique, j is non-unique: j is redundant.
                    redundant = indexes[j];
                    keeper = indexes[i];
                }

                // Skip if this index was already flagged as prefix-duplicate.
                bool already_warned = false;
                for (const auto& w : warnings) {
                    if (w.index_name == redundant->name) {
                        already_warned = true;
                        break;
                    }
                }
                if (already_warned) continue;

                warnings.push_back({
                    .type = WarningType::RedundantIndex,
                    .message = "Index '" + redundant->name + "' on table '" +
                               table_name + "' is redundant: duplicate of index '" +
                               keeper->name + "'",
                    .index_name = redundant->name,
                    .covered_by = keeper->name,
                    .table_name = table_name,
                });
            }
        }
    }

    // Sort warnings by (table_name, index_name) for deterministic output.
    std::sort(warnings.begin(), warnings.end(),
              [](const Warning& a, const Warning& b) {
                  if (a.table_name != b.table_name) return a.table_name < b.table_name;
                  return a.index_name < b.index_name;
              });

    return warnings;
}


// --- apply.cpp ---



namespace {

void ensure_state_table(sqlite3* db) {
    Statement stmt(db,
        "CREATE TABLE IF NOT EXISTS _sqlift_state ("
        "  key   TEXT PRIMARY KEY,"
        "  value TEXT NOT NULL"
        ")");
    stmt.step();
}

void store_schema_hash(sqlite3* db, const std::string& hash) {
    ensure_state_table(db);
    Statement stmt(db,
        "INSERT OR REPLACE INTO _sqlift_state (key, value) VALUES ('schema_hash', ?)");
    stmt.bind_text(1, hash);
    stmt.step();

    // Increment migration version counter
    Statement ver_stmt(db,
        "INSERT OR REPLACE INTO _sqlift_state (key, value) "
        "VALUES ('migration_version', COALESCE("
        "(SELECT CAST(value AS INTEGER) + 1 FROM _sqlift_state "
        "WHERE key='migration_version'), 1))");
    ver_stmt.step();
}

std::string load_schema_hash(sqlite3* db) {
    // Check if table exists first.
    Statement check(db,
        "SELECT name FROM sqlite_master WHERE type='table' AND name='_sqlift_state'");
    if (!check.step()) return {};

    Statement stmt(db,
        "SELECT value FROM _sqlift_state WHERE key='schema_hash'");
    if (stmt.step())
        return stmt.column_text(0);
    return {};
}

} // namespace

void apply(sqlite3* db, const MigrationPlan& plan, const ApplyOptions& opts) {
    if (plan.empty()) return;

    // Policy gates, in order of severity. Each rebuild op is permitted
    // when allow_rebuild is set, OR when it is pure loosening and
    // allow_loosen is set. A data-dependent op also requires
    // allow_data_dependent. Drops require allow_destructive.
    // Messages always include op.description so on-device crash reports
    // can diagnose without re-diffing offline.
    for (const auto& op : plan.operations()) {
        if (op.requires_rebuild) {
            bool rebuild_ok = opts.allow_rebuild ||
                              (opts.allow_loosen && op.loosens_only);
            if (!rebuild_ok) {
                throw RebuildError(
                    "Migration plan requires a table rebuild for '" +
                    op.object_name + "': " + op.description +
                    ". Set SQLIFT_ALLOW_REBUILD in opts.allow" +
                    (op.loosens_only
                        ? " (or SQLIFT_ALLOW_LOOSEN -- this rebuild is pure loosening)"
                        : "") +
                    " to proceed.");
            }
        }
        if (op.data_dependent && !opts.allow_data_dependent) {
            throw BreakingChangeError(
                "Migration plan contains data-dependent change on '" +
                op.object_name + "': " + op.description +
                ". Set SQLIFT_ALLOW_DATA_DEPENDENT in opts.allow to proceed "
                "(success depends on existing data).");
        }
        if (op.destructive && !opts.allow_destructive) {
            throw DestructiveError(
                "Migration plan contains destructive operation on '" +
                op.object_name + "': " + op.description +
                ". Set SQLIFT_ALLOW_DESTRUCTIVE in opts.allow to proceed.");
        }
    }

    // Check for drift
    Schema current = extract(db);
    std::string stored_hash = load_schema_hash(db);
    if (!stored_hash.empty()) {
        std::string actual_hash = current.hash();
        if (stored_hash != actual_hash) {
            throw DriftError(
                "Schema drift detected: the database schema has been modified "
                "outside of sqlift. Stored hash: " + stored_hash +
                ", actual hash: " + actual_hash);
        }
    }

    // Save current FK enforcement state so we can restore it on failure.
    bool fk_was_on = false;
    {
        Statement fk_query(db, "PRAGMA foreign_keys");
        fk_was_on = fk_query.step() && fk_query.column_int(0) != 0;
    }

    try {
        for (const auto& op : plan.operations()) {
            for (const auto& sql : op.sql) {
                try {
                    // PRAGMA foreign_key_check returns rows if there are violations.
                    // We need to handle this specially. The prefix match is safe
                    // because this SQL is generated by rebuild_table_sql() — plans
                    // from from_json() are trusted input (same as schema DDL).
                    if (sql.find("PRAGMA foreign_key_check") == 0) {
                        Statement stmt(db, sql);
                        if (stmt.step()) {
                            std::string table = stmt.column_text(0);
                            int64_t rowid = stmt.column_int(1);
                            std::string parent = stmt.column_text(2);
                            throw ApplyError(
                                "Foreign key violation in table '" + table +
                                "' (rowid " + std::to_string(rowid) +
                                "): references missing row in parent table '" +
                                parent + "'; during '" + op.object_name +
                                "' (" + op.description + "); SQL: " + sql);
                        }
                        continue;
                    }

                    Statement stmt(db, sql);
                    stmt.step();
                } catch (const ApplyError&) {
                    throw;
                } catch (const std::exception& e) {
                    throw ApplyError(
                        std::string("Failed applying '") + op.object_name +
                        "' (" + op.description + "): " + e.what() +
                        "; SQL: " + sql);
                }
            }
        }
    } catch (...) {
        // Roll back any open savepoint from a failed rebuild, then restore FK state.
        // PRAGMA foreign_keys cannot be changed inside an open transaction/savepoint.
        try {
            Statement rb(db, "ROLLBACK TO SAVEPOINT sqlift_rebuild");
            rb.step();
            Statement rel(db, "RELEASE SAVEPOINT sqlift_rebuild");
            rel.step();
        } catch (...) {}
        // Restore FK enforcement to its state before apply() was called.
        try {
            Statement restore(db, fk_was_on ? "PRAGMA foreign_keys=ON"
                                            : "PRAGMA foreign_keys=OFF");
            restore.step();
        } catch (...) {}
        throw;
    }

    // Update stored hash
    Schema after = extract(db);
    store_schema_hash(db, after.hash());
}

int64_t migration_version(sqlite3* db) {
    Statement check(db,
        "SELECT name FROM sqlite_master WHERE type='table' AND name='_sqlift_state'");
    if (!check.step()) return 0;

    Statement stmt(db,
        "SELECT value FROM _sqlift_state WHERE key='migration_version'");
    if (stmt.step()) {
        try {
            return std::stoll(stmt.column_text(0));
        } catch (...) {
            return 0;
        }
    }
    return 0;
}


// --- json.cpp ---




namespace {

struct OpTypeEntry {
    OpType type;
    const char* name;
};

constexpr OpTypeEntry op_type_names[] = {
    {OpType::CreateTable,        "CreateTable"},
    {OpType::DropTable,          "DropTable"},
    {OpType::RebuildTable,       "RebuildTable"},
    {OpType::AddColumn,          "AddColumn"},
    {OpType::CreateIndex,        "CreateIndex"},
    {OpType::DropIndex,          "DropIndex"},
    {OpType::CreateView,         "CreateView"},
    {OpType::DropView,           "DropView"},
    {OpType::CreateTrigger,      "CreateTrigger"},
    {OpType::DropTrigger,        "DropTrigger"},
    {OpType::CreateVirtualTable, "CreateVirtualTable"},
    {OpType::DropVirtualTable,   "DropVirtualTable"},
    {OpType::DropColumn,         "DropColumn"},
    {OpType::AlterColumnType,    "AlterColumnType"},
    {OpType::SetNotNull,         "SetNotNull"},
    {OpType::DropNotNull,        "DropNotNull"},
    {OpType::SetDefault,         "SetDefault"},
    {OpType::DropDefault,        "DropDefault"},
    {OpType::AddConstraint,      "AddConstraint"},
    {OpType::DropConstraint,     "DropConstraint"},
};

} // namespace

std::string to_string(OpType type) {
    for (const auto& entry : op_type_names) {
        if (entry.type == type) return entry.name;
    }
    throw JsonError("Unknown OpType value: " +
                    std::to_string(static_cast<int>(type)));
}

OpType op_type_from_string(const std::string& s) {
    for (const auto& entry : op_type_names) {
        if (s == entry.name) return entry.type;
    }
    throw JsonError("Unknown OpType string: " + s);
}

std::string to_json(const MigrationPlan& plan) {
    nlohmann::json j;
    j["version"] = 1;

    auto& ops = j["operations"];
    ops = nlohmann::json::array();

    for (const auto& op : plan.operations()) {
        nlohmann::json jop;
        jop["type"] = to_string(op.type);
        jop["object_name"] = op.object_name;
        jop["description"] = op.description;
        jop["sql"] = op.sql;
        jop["destructive"] = op.destructive;
        jop["requires_rebuild"] = op.requires_rebuild;
        jop["loosens_only"] = op.loosens_only;
        jop["data_dependent"] = op.data_dependent;
        ops.push_back(std::move(jop));
    }

    if (!plan.warnings().empty()) {
        auto& warns = j["warnings"];
        warns = nlohmann::json::array();
        for (const auto& w : plan.warnings()) {
            nlohmann::json jw;
            jw["type"] = "RedundantIndex";
            jw["message"] = w.message;
            jw["index_name"] = w.index_name;
            jw["covered_by"] = w.covered_by;
            jw["table_name"] = w.table_name;
            warns.push_back(std::move(jw));
        }
    }

    return j.dump(2);
}

MigrationPlan from_json(const std::string& json_str) {
    nlohmann::json j;
    try {
        j = nlohmann::json::parse(json_str);
    } catch (const nlohmann::json::parse_error& e) {
        throw JsonError(std::string("Invalid JSON: ") + e.what());
    }

    if (!j.is_object())
        throw JsonError("Expected top-level JSON object");

    if (!j.contains("version") || !j["version"].is_number_integer())
        throw JsonError("Missing or invalid 'version' field");
    int version = j["version"].get<int>();
    if (version != 1)
        throw JsonError("Unsupported version: " + std::to_string(version));

    if (!j.contains("operations") || !j["operations"].is_array())
        throw JsonError("Missing or invalid 'operations' array");

    MigrationPlan plan;
    for (const auto& jop : j["operations"]) {
        if (!jop.is_object())
            throw JsonError("Each operation must be a JSON object");

        Operation op;

        if (!jop.contains("type") || !jop["type"].is_string())
            throw JsonError("Operation missing 'type' string field");
        op.type = op_type_from_string(jop["type"].get<std::string>());

        if (!jop.contains("object_name") || !jop["object_name"].is_string())
            throw JsonError("Operation missing 'object_name' string field");
        op.object_name = jop["object_name"].get<std::string>();

        if (!jop.contains("description") || !jop["description"].is_string())
            throw JsonError("Operation missing 'description' string field");
        op.description = jop["description"].get<std::string>();

        if (!jop.contains("sql") || !jop["sql"].is_array())
            throw JsonError("Operation missing 'sql' array field");
        for (const auto& s : jop["sql"]) {
            if (!s.is_string())
                throw JsonError("'sql' array must contain only strings");
            op.sql.push_back(s.get<std::string>());
        }

        if (!jop.contains("destructive") || !jop["destructive"].is_boolean())
            throw JsonError("Operation missing 'destructive' boolean field");
        op.destructive = jop["destructive"].get<bool>();

        // requires_rebuild is optional for forward-compat with older plan JSON;
        // default to true for RebuildTable ops, false otherwise, when absent.
        if (jop.contains("requires_rebuild")) {
            if (!jop["requires_rebuild"].is_boolean())
                throw JsonError("Operation 'requires_rebuild' must be boolean");
            op.requires_rebuild = jop["requires_rebuild"].get<bool>();
        } else {
            op.requires_rebuild = (op.type == OpType::RebuildTable);
        }

        // loosens_only and data_dependent are optional for forward-compat
        // with older plan JSON; default to false (the conservative choice
        // -- callers will get a more restrictive gate on old plans, never
        // a more permissive one).
        if (jop.contains("loosens_only")) {
            if (!jop["loosens_only"].is_boolean())
                throw JsonError("Operation 'loosens_only' must be boolean");
            op.loosens_only = jop["loosens_only"].get<bool>();
        }
        if (jop.contains("data_dependent")) {
            if (!jop["data_dependent"].is_boolean())
                throw JsonError("Operation 'data_dependent' must be boolean");
            op.data_dependent = jop["data_dependent"].get<bool>();
        }

        // Validate SQL prefix matches OpType
        if (!op.sql.empty()) {
            const std::string& first_sql = op.sql[0];
            std::string expected_prefix;
            switch (op.type) {
                case OpType::CreateTable:   expected_prefix = "CREATE TABLE"; break;
                case OpType::DropTable:     expected_prefix = "DROP TABLE"; break;
                case OpType::RebuildTable:  expected_prefix = "PRAGMA foreign_keys"; break;
                case OpType::AddColumn:     expected_prefix = "ALTER TABLE"; break;
                case OpType::CreateIndex:   expected_prefix = "CREATE"; break;
                case OpType::DropIndex:     expected_prefix = "DROP INDEX"; break;
                case OpType::CreateView:    expected_prefix = "CREATE VIEW"; break;
                case OpType::DropView:      expected_prefix = "DROP VIEW"; break;
                case OpType::CreateTrigger: expected_prefix = "CREATE TRIGGER"; break;
                case OpType::DropTrigger:   expected_prefix = "DROP TRIGGER"; break;
                case OpType::CreateVirtualTable: expected_prefix = "CREATE VIRTUAL TABLE"; break;
                case OpType::DropVirtualTable:   expected_prefix = "DROP TABLE"; break;
                case OpType::DropColumn:
                case OpType::AlterColumnType:
                case OpType::SetNotNull:
                case OpType::DropNotNull:
                case OpType::SetDefault:
                case OpType::DropDefault:
                case OpType::AddConstraint:
                case OpType::DropConstraint:
                    expected_prefix = "ALTER TABLE"; break;
            }
            if (!starts_with(first_sql, expected_prefix)) {
                throw JsonError(
                    "Operation '" + to_string(op.type) + "' on '" +
                    op.object_name + "': first SQL statement does not start with '" +
                    expected_prefix + "'");
            }
        }

        plan.ops_.push_back(std::move(op));
    }

    // Warnings are optional (backward-compatible with older JSON).
    if (j.contains("warnings") && j["warnings"].is_array()) {
        for (const auto& jw : j["warnings"]) {
            if (!jw.is_object()) continue;
            Warning w;
            w.type = WarningType::RedundantIndex;
            w.message = jw.value("message", "");
            w.index_name = jw.value("index_name", "");
            w.covered_by = jw.value("covered_by", "");
            w.table_name = jw.value("table_name", "");
            plan.warnings_.push_back(std::move(w));
        }
    }

    return plan;
}


// --- schema_json.cpp ---




std::string schema_to_json(const Schema& schema) {
    nlohmann::json j;
    if (schema.dialect != "sqlite")
        j["dialect"] = schema.dialect;
    if (!schema.schema_name.empty())
        j["schema"] = schema.schema_name;

    // Tables
    auto& jt = j["tables"];
    jt = nlohmann::json::object();
    for (const auto& [name, table] : schema.tables) {
        nlohmann::json jtbl;
        jtbl["name"] = table.name;

        auto& jcols = jtbl["columns"];
        jcols = nlohmann::json::array();
        for (const auto& col : table.columns) {
            nlohmann::json jcol;
            jcol["name"] = col.name;
            jcol["type"] = col.type;
            jcol["notnull"] = col.notnull;
            jcol["default_value"] = col.default_value;
            jcol["pk"] = col.pk;
            jcol["collation"] = col.collation;
            jcol["generated"] = static_cast<int>(col.generated);
            jcol["generated_expr"] = col.generated_expr;
            jcols.push_back(std::move(jcol));
        }

        auto& jfks = jtbl["foreign_keys"];
        jfks = nlohmann::json::array();
        for (const auto& fk : table.foreign_keys) {
            nlohmann::json jfk;
            jfk["constraint_name"] = fk.constraint_name;
            jfk["from_columns"] = fk.from_columns;
            jfk["to_table"] = fk.to_table;
            jfk["to_columns"] = fk.to_columns;
            jfk["on_update"] = fk.on_update;
            jfk["on_delete"] = fk.on_delete;
            jfks.push_back(std::move(jfk));
        }

        auto& jchks = jtbl["check_constraints"];
        jchks = nlohmann::json::array();
        for (const auto& chk : table.check_constraints) {
            nlohmann::json jchk;
            jchk["name"] = chk.name;
            jchk["expression"] = chk.expression;
            jchks.push_back(std::move(jchk));
        }

        jtbl["pk_constraint_name"] = table.pk_constraint_name;
        jtbl["without_rowid"] = table.without_rowid;
        jtbl["strict"] = table.strict;
        jtbl["raw_sql"] = table.raw_sql;

        jt[name] = std::move(jtbl);
    }

    // Indexes
    auto& ji = j["indexes"];
    ji = nlohmann::json::object();
    for (const auto& [name, idx] : schema.indexes) {
        nlohmann::json jidx;
        jidx["name"] = idx.name;
        jidx["table_name"] = idx.table_name;
        jidx["columns"] = idx.columns;
        jidx["unique"] = idx.unique;
        jidx["where_clause"] = idx.where_clause;
        jidx["raw_sql"] = idx.raw_sql;
        ji[name] = std::move(jidx);
    }

    // Views
    auto& jv = j["views"];
    jv = nlohmann::json::object();
    for (const auto& [name, view] : schema.views) {
        nlohmann::json jview;
        jview["name"] = view.name;
        jview["sql"] = view.sql;
        jv[name] = std::move(jview);
    }

    // Triggers
    auto& jtr = j["triggers"];
    jtr = nlohmann::json::object();
    for (const auto& [name, trig] : schema.triggers) {
        nlohmann::json jtrig;
        jtrig["name"] = trig.name;
        jtrig["table_name"] = trig.table_name;
        jtrig["sql"] = trig.sql;
        jtr[name] = std::move(jtrig);
    }

    // Virtual tables. Emit only when non-empty so JSON for schemas without
    // virtual tables stays byte-identical to the pre-feature output.
    if (!schema.virtual_tables.empty()) {
        auto& jvt = j["virtual_tables"];
        jvt = nlohmann::json::object();
        for (const auto& [name, vt] : schema.virtual_tables) {
            nlohmann::json jvtbl;
            jvtbl["name"] = vt.name;
            jvtbl["module"] = vt.module;
            jvtbl["args"] = vt.args;
            jvtbl["raw_sql"] = vt.raw_sql;
            jvt[name] = std::move(jvtbl);
        }
    }

    return j.dump(2);
}

Schema schema_from_json(const std::string& json_str) {
    nlohmann::json j;
    try {
        j = nlohmann::json::parse(json_str);
    } catch (const nlohmann::json::parse_error& e) {
        throw JsonError(std::string("Invalid JSON: ") + e.what());
    }

    if (!j.is_object())
        throw JsonError("Expected top-level JSON object");

    Schema schema;
    schema.dialect = j.value("dialect", "sqlite");
    schema.schema_name = j.value("schema", "");

    // Tables
    if (j.contains("tables") && j["tables"].is_object()) {
        for (const auto& [name, jtbl] : j["tables"].items()) {
            Table table;
            table.name = jtbl.value("name", "");

            if (jtbl.contains("columns") && jtbl["columns"].is_array()) {
                for (const auto& jcol : jtbl["columns"]) {
                    Column col;
                    col.name = jcol.value("name", "");
                    col.type = jcol.value("type", "");
                    col.notnull = jcol.value("notnull", false);
                    col.default_value = jcol.value("default_value", "");
                    col.pk = jcol.value("pk", 0);
                    col.collation = jcol.value("collation", "");
                    col.generated = static_cast<GeneratedType>(jcol.value("generated", 0));
                    col.generated_expr = jcol.value("generated_expr", "");
                    table.columns.push_back(std::move(col));
                }
            }

            if (jtbl.contains("foreign_keys") && jtbl["foreign_keys"].is_array()) {
                for (const auto& jfk : jtbl["foreign_keys"]) {
                    ForeignKey fk;
                    fk.constraint_name = jfk.value("constraint_name", "");
                    fk.from_columns = jfk.value("from_columns", std::vector<std::string>{});
                    fk.to_table = jfk.value("to_table", "");
                    fk.to_columns = jfk.value("to_columns", std::vector<std::string>{});
                    fk.on_update = jfk.value("on_update", "NO ACTION");
                    fk.on_delete = jfk.value("on_delete", "NO ACTION");
                    table.foreign_keys.push_back(std::move(fk));
                }
            }

            if (jtbl.contains("check_constraints") && jtbl["check_constraints"].is_array()) {
                for (const auto& jchk : jtbl["check_constraints"]) {
                    CheckConstraint chk;
                    chk.name = jchk.value("name", "");
                    chk.expression = jchk.value("expression", "");
                    table.check_constraints.push_back(std::move(chk));
                }
            }

            table.pk_constraint_name = jtbl.value("pk_constraint_name", "");
            table.without_rowid = jtbl.value("without_rowid", false);
            table.strict = jtbl.value("strict", false);
            table.raw_sql = jtbl.value("raw_sql", "");

            schema.tables[name] = std::move(table);
        }
    }

    // Indexes
    if (j.contains("indexes") && j["indexes"].is_object()) {
        for (const auto& [name, jidx] : j["indexes"].items()) {
            Index idx;
            idx.name = jidx.value("name", "");
            idx.table_name = jidx.value("table_name", "");
            idx.columns = jidx.value("columns", std::vector<std::string>{});
            idx.unique = jidx.value("unique", false);
            idx.where_clause = jidx.value("where_clause", "");
            idx.raw_sql = jidx.value("raw_sql", "");
            schema.indexes[name] = std::move(idx);
        }
    }

    // Views
    if (j.contains("views") && j["views"].is_object()) {
        for (const auto& [name, jview] : j["views"].items()) {
            View view;
            view.name = jview.value("name", "");
            view.sql = jview.value("sql", "");
            schema.views[name] = std::move(view);
        }
    }

    // Triggers
    if (j.contains("triggers") && j["triggers"].is_object()) {
        for (const auto& [name, jtrig] : j["triggers"].items()) {
            Trigger trig;
            trig.name = jtrig.value("name", "");
            trig.table_name = jtrig.value("table_name", "");
            trig.sql = jtrig.value("sql", "");
            schema.triggers[name] = std::move(trig);
        }
    }

    // Virtual tables (optional for forward-compat with older JSON).
    if (j.contains("virtual_tables") && j["virtual_tables"].is_object()) {
        for (const auto& [name, jvt] : j["virtual_tables"].items()) {
            VirtualTable vt;
            vt.name = jvt.value("name", "");
            vt.module = jvt.value("module", "");
            vt.args = jvt.value("args", "");
            vt.raw_sql = jvt.value("raw_sql", "");
            schema.virtual_tables[name] = std::move(vt);
        }
    }

    return schema;
}


// --- postgres extract / apply / parse_on ---

namespace {

struct RowSink {
    std::vector<std::vector<std::string>> rows;
};

void row_cb(void* ctx, int ncols, const char* const* cols) {
    auto* sink = static_cast<RowSink*>(ctx);
    std::vector<std::string> row;
    row.reserve(static_cast<size_t>(ncols));
    for (int i = 0; i < ncols; ++i)
        row.emplace_back(cols[i] ? cols[i] : "");
    sink->rows.push_back(std::move(row));
}

void exec_via(const sqlift_exec& ex, const std::string& sql) {
    if (!ex.exec) throw Error("sqlift_exec.exec is null");
    char* err = nullptr;
    int rc = ex.exec(ex.ctx, sql.c_str(), &err);
    if (rc != 0) {
        std::string msg = err ? err : "exec failed";
        std::free(err);
        throw Error(msg + "; SQL: " + sql);
    }
}

std::vector<std::vector<std::string>>
query_via(const sqlift_exec& ex, const std::string& sql) {
    if (!ex.query) throw Error("sqlift_exec.query is null");
    RowSink sink;
    char* err = nullptr;
    int rc = ex.query(ex.ctx, sql.c_str(), row_cb, &sink, &err);
    if (rc != 0) {
        std::string msg = err ? err : "query failed";
        std::free(err);
        throw Error(msg + "; SQL: " + sql);
    }
    return sink.rows;
}

std::string normalize_pg_type(std::string t) {
    t = trim(t);
    std::string lower;
    lower.reserve(t.size());
    for (char c : t) lower.push_back(static_cast<char>(std::tolower(static_cast<unsigned char>(c))));
    if (lower == "int8") return "bigint";
    if (lower == "int4" || lower == "int") return "integer";
    if (lower == "int2") return "smallint";
    if (lower == "float8") return "double precision";
    if (lower == "float4") return "real";
    if (lower == "bool") return "boolean";
    if (lower == "timestamptz") return "timestamp with time zone";
    if (lower == "timestamp") return "timestamp without time zone";
    if (lower.rfind("character varying", 0) == 0) {
        if (lower.size() == 17) return "varchar";
        return "varchar" + t.substr(17);
    }
    if (lower.rfind("character(", 0) == 0 || lower.rfind("character (", 0) == 0)
        return t;
    return lower;
}

std::string strip_check_def(const std::string& def) {
    // "CHECK (expr)" or "CONSTRAINT name CHECK (expr)"
    auto pos = to_upper(def).find("CHECK");
    if (pos == std::string::npos) return def;
    auto open = def.find('(', pos);
    if (open == std::string::npos) return def;
    int depth = 0;
    for (size_t i = open; i < def.size(); ++i) {
        if (def[i] == '(') ++depth;
        else if (def[i] == ')') {
            --depth;
            if (depth == 0)
                return trim(def.substr(open + 1, i - open - 1));
        }
    }
    return def;
}

} // namespace

Schema extract_exec(const sqlift_exec& ex, const std::string& schema_name) {
    Schema schema;
    schema.dialect = "postgres";
    schema.schema_name = schema_name.empty() ? "public" : schema_name;
    const std::string nsp = quote_lit(schema.schema_name);

    auto tables = query_via(ex,
        "SELECT c.relname FROM pg_class c "
        "JOIN pg_namespace n ON n.oid = c.relnamespace "
        "WHERE n.nspname = " + nsp + " AND c.relkind = 'r' "
        "AND c.relname <> '_sqlift_state' "
        "ORDER BY c.relname");

    for (const auto& tr : tables) {
        if (tr.empty() || tr[0].empty()) continue;
        Table table;
        table.name = tr[0];
        const std::string tlit = quote_lit(table.name);

        auto cols = query_via(ex,
            "SELECT a.attname, format_type(a.atttypid, a.atttypmod), "
            "a.attnotnull, "
            "pg_get_expr(ad.adbin, ad.adrelid), "
            "COALESCE((SELECT a2.attnum FROM pg_index i "
            " JOIN pg_attribute a2 ON a2.attrelid = i.indrelid "
            "  AND a2.attnum = ANY (i.indkey) "
            " WHERE i.indrelid = c.oid AND i.indisprimary "
            "  AND a2.attnum = a.attnum), 0) "
            "FROM pg_class c "
            "JOIN pg_namespace n ON n.oid = c.relnamespace "
            "JOIN pg_attribute a ON a.attrelid = c.oid "
            "LEFT JOIN pg_attrdef ad ON ad.adrelid = c.oid AND ad.adnum = a.attnum "
            "WHERE n.nspname = " + nsp + " AND c.relname = " + tlit + " "
            "AND a.attnum > 0 AND NOT a.attisdropped "
            "ORDER BY a.attnum");
        int pk_ord = 0;
        for (const auto& row : cols) {
            Column col;
            col.name = row[0];
            col.type = normalize_pg_type(row[1]);
            col.notnull = (row[2] == "t" || row[2] == "true" || row[2] == "1");
            col.default_value = row.size() > 3 ? row[3] : "";
            bool is_pk = row.size() > 4 && row[4] != "0" && !row[4].empty();
            if (is_pk) col.pk = ++pk_ord;
            table.columns.push_back(std::move(col));
        }

        auto fks = query_via(ex,
            "SELECT con.conname, "
            "pg_get_constraintdef(con.oid, true) "
            "FROM pg_constraint con "
            "JOIN pg_class c ON c.oid = con.conrelid "
            "JOIN pg_namespace n ON n.oid = c.relnamespace "
            "WHERE n.nspname = " + nsp + " AND c.relname = " + tlit + " "
            "AND con.contype = 'f' ORDER BY con.conname");
        for (const auto& row : fks) {
            ForeignKey fk;
            fk.constraint_name = row[0];
            // FOREIGN KEY (a, b) REFERENCES tbl(c, d) [ON DELETE ...] [ON UPDATE ...]
            const std::string& def = row[1];
            auto fk_pos = to_upper(def).find("FOREIGN KEY");
            auto ref_pos = to_upper(def).find("REFERENCES");
            if (fk_pos == std::string::npos || ref_pos == std::string::npos)
                continue;
            auto from_open = def.find('(', fk_pos);
            auto from_close = def.find(')', from_open);
            auto to_name_start = ref_pos + 10;
            while (to_name_start < def.size() &&
                   std::isspace(static_cast<unsigned char>(def[to_name_start])))
                ++to_name_start;
            auto to_open = def.find('(', to_name_start);
            std::string to_table = trim(def.substr(to_name_start,
                                                   to_open - to_name_start));
            to_table = strip_quotes(to_table);
            auto to_close = def.find(')', to_open);
            auto split_cols = [](const std::string& inner) {
                std::vector<std::string> out;
                std::string cur;
                for (char ch : inner) {
                    if (ch == ',') {
                        out.push_back(strip_quotes(trim(cur)));
                        cur.clear();
                    } else cur += ch;
                }
                if (!trim(cur).empty())
                    out.push_back(strip_quotes(trim(cur)));
                return out;
            };
            fk.from_columns = split_cols(def.substr(from_open + 1,
                                                    from_close - from_open - 1));
            fk.to_table = to_table;
            fk.to_columns = split_cols(def.substr(to_open + 1,
                                                  to_close - to_open - 1));
            auto upd = to_upper(def).rfind("ON UPDATE ");
            auto del = to_upper(def).rfind("ON DELETE ");
            auto action = [&](size_t pos) {
                if (pos == std::string::npos) return std::string("NO ACTION");
                std::string rest = trim(def.substr(pos + 10));
                if (to_upper(rest).rfind("CASCADE", 0) == 0) return std::string("CASCADE");
                if (to_upper(rest).rfind("SET NULL", 0) == 0) return std::string("SET NULL");
                if (to_upper(rest).rfind("SET DEFAULT", 0) == 0) return std::string("SET DEFAULT");
                if (to_upper(rest).rfind("RESTRICT", 0) == 0) return std::string("RESTRICT");
                return std::string("NO ACTION");
            };
            fk.on_update = action(upd);
            fk.on_delete = action(del);
            table.foreign_keys.push_back(std::move(fk));
        }

        auto checks = query_via(ex,
            "SELECT con.conname, pg_get_constraintdef(con.oid, true) "
            "FROM pg_constraint con "
            "JOIN pg_class c ON c.oid = con.conrelid "
            "JOIN pg_namespace n ON n.oid = c.relnamespace "
            "WHERE n.nspname = " + nsp + " AND c.relname = " + tlit + " "
            "AND con.contype = 'c' ORDER BY con.conname");
        for (const auto& row : checks) {
            CheckConstraint chk;
            chk.name = row[0];
            chk.expression = strip_check_def(row[1]);
            table.check_constraints.push_back(std::move(chk));
        }

        auto pkn = query_via(ex,
            "SELECT con.conname FROM pg_constraint con "
            "JOIN pg_class c ON c.oid = con.conrelid "
            "JOIN pg_namespace n ON n.oid = c.relnamespace "
            "WHERE n.nspname = " + nsp + " AND c.relname = " + tlit + " "
            "AND con.contype = 'p'");
        if (!pkn.empty() && !pkn[0].empty())
            table.pk_constraint_name = pkn[0][0];

        table.raw_sql = create_table_sql_from_ir(table);
        schema.tables[table.name] = std::move(table);
    }

    auto indexes = query_via(ex,
        "SELECT i.relname, t.relname, ix.indisunique, "
        "pg_get_indexdef(ix.indexrelid) "
        "FROM pg_index ix "
        "JOIN pg_class i ON i.oid = ix.indexrelid "
        "JOIN pg_class t ON t.oid = ix.indrelid "
        "JOIN pg_namespace n ON n.oid = t.relnamespace "
        "WHERE n.nspname = " + nsp + " "
        "AND NOT ix.indisprimary "
        "AND NOT EXISTS (SELECT 1 FROM pg_constraint co "
        " WHERE co.conindid = ix.indexrelid) "
        "ORDER BY i.relname");
    for (const auto& row : indexes) {
        Index idx;
        idx.name = row[0];
        idx.table_name = row[1];
        idx.unique = (row[2] == "t" || row[2] == "true" || row[2] == "1");
        idx.raw_sql = row[3];
        auto cols = query_via(ex,
            "SELECT a.attname FROM pg_index ix "
            "JOIN pg_class i ON i.oid = ix.indexrelid "
            "JOIN pg_attribute a ON a.attrelid = ix.indrelid "
            " AND a.attnum = ANY (ix.indkey) "
            "WHERE i.relname = " + quote_lit(idx.name) + " "
            "ORDER BY array_position(ix.indkey, a.attnum)");
        for (const auto& c : cols)
            if (!c.empty()) idx.columns.push_back(c[0]);
        auto upper = to_upper(idx.raw_sql);
        auto w = upper.rfind(" WHERE ");
        if (w != std::string::npos)
            idx.where_clause = trim(idx.raw_sql.substr(w + 7));
        schema.indexes[idx.name] = std::move(idx);
    }

    auto views = query_via(ex,
        "SELECT c.relname, pg_get_viewdef(c.oid, true) "
        "FROM pg_class c "
        "JOIN pg_namespace n ON n.oid = c.relnamespace "
        "WHERE n.nspname = " + nsp + " AND c.relkind = 'v' "
        "ORDER BY c.relname");
    for (const auto& row : views) {
        View view;
        view.name = row[0];
        std::string def = trim(row[1]);
        if (!def.empty() && def.back() == ';') def.pop_back();
        view.sql = "CREATE VIEW " + quote_id(view.name) + " AS " + def;
        schema.views[view.name] = std::move(view);
    }

    auto trigs = query_via(ex,
        "SELECT t.tgname, c.relname, pg_get_triggerdef(t.oid, true) "
        "FROM pg_trigger t "
        "JOIN pg_class c ON c.oid = t.tgrelid "
        "JOIN pg_namespace n ON n.oid = c.relnamespace "
        "WHERE n.nspname = " + nsp + " AND NOT t.tgisinternal "
        "ORDER BY t.tgname");
    for (const auto& row : trigs) {
        Trigger trig;
        trig.name = row[0];
        trig.table_name = row[1];
        trig.sql = row[2];
        schema.triggers[trig.name] = std::move(trig);
    }

    return schema;
}

void apply_exec(const sqlift_exec& ex, const MigrationPlan& plan,
                const ApplyOptions& opts) {
    if (plan.empty()) return;

    for (const auto& op : plan.operations()) {
        if (op.requires_rebuild) {
            bool rebuild_ok = opts.allow_rebuild ||
                              (opts.allow_loosen && op.loosens_only);
            if (!rebuild_ok) {
                throw RebuildError(
                    "Migration plan requires ALTER TYPE / rebuild for '" +
                    op.object_name + "': " + op.description +
                    ". Set SQLIFT_ALLOW_REBUILD in opts.allow to proceed.");
            }
        }
        if (op.data_dependent && !opts.allow_data_dependent) {
            throw BreakingChangeError(
                "Migration plan contains data-dependent change on '" +
                op.object_name + "': " + op.description +
                ". Set SQLIFT_ALLOW_DATA_DEPENDENT in opts.allow to proceed "
                "(success depends on existing data).");
        }
        if (op.destructive && !opts.allow_destructive) {
            throw DestructiveError(
                "Migration plan contains destructive operation on '" +
                op.object_name + "': " + op.description +
                ". Set SQLIFT_ALLOW_DESTRUCTIVE in opts.allow to proceed.");
        }
    }

    Schema current = extract_exec(ex, "public");
    std::string stored_hash;
    try {
        auto rows = query_via(ex,
            "SELECT 1 FROM information_schema.tables "
            "WHERE table_schema = 'sqlpipe' AND table_name = '_sqlift_state'");
        if (!rows.empty()) {
            auto hv = query_via(ex,
                "SELECT value FROM sqlpipe._sqlift_state WHERE key = 'schema_hash'");
            if (!hv.empty() && !hv[0].empty())
                stored_hash = hv[0][0];
        }
    } catch (...) {
        stored_hash.clear();
    }
    if (!stored_hash.empty()) {
        std::string actual = current.hash();
        if (stored_hash != actual) {
            throw DriftError(
                "Schema drift detected: the database schema has been modified "
                "outside of sqlift. Stored hash: " + stored_hash +
                ", actual hash: " + actual);
        }
    }

    try {
        exec_via(ex, "BEGIN");
        for (const auto& op : plan.operations()) {
            for (const auto& sql : op.sql) {
                try {
                    exec_via(ex, sql);
                } catch (const std::exception& e) {
                    throw ApplyError(
                        std::string("Failed applying '") + op.object_name +
                        "' (" + op.description + "): " + e.what() +
                        "; SQL: " + sql);
                }
            }
        }
        exec_via(ex, "COMMIT");
    } catch (...) {
        try { exec_via(ex, "ROLLBACK"); } catch (...) {}
        throw;
    }

    Schema after = extract_exec(ex, "public");
    exec_via(ex, "CREATE SCHEMA IF NOT EXISTS sqlpipe");
    exec_via(ex,
        "CREATE TABLE IF NOT EXISTS sqlpipe._sqlift_state ("
        "key TEXT PRIMARY KEY, value TEXT NOT NULL)");
    exec_via(ex,
        "INSERT INTO sqlpipe._sqlift_state (key, value) VALUES "
        "('schema_hash', " + quote_lit(after.hash()) + ") "
        "ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value");
    exec_via(ex,
        "INSERT INTO sqlpipe._sqlift_state (key, value) "
        "VALUES ('migration_version', '1') "
        "ON CONFLICT (key) DO UPDATE SET "
        "value = (EXCLUDED.value::integer + "
        "sqlpipe._sqlift_state.value::integer)::text");
}

Schema parse_on_exec(const sqlift_exec& ex, const std::string& ddl) {
    try {
        exec_via(ex, "BEGIN");
        exec_via(ex, "DROP SCHEMA IF EXISTS sqlift_parse_tmp CASCADE");
        exec_via(ex, "CREATE SCHEMA sqlift_parse_tmp");
        exec_via(ex, "SET LOCAL search_path TO sqlift_parse_tmp");
        exec_via(ex, ddl);
        Schema schema = extract_exec(ex, "sqlift_parse_tmp");
        exec_via(ex, "DROP SCHEMA sqlift_parse_tmp CASCADE");
        exec_via(ex, "COMMIT");
        schema.schema_name = "public";
        return schema;
    } catch (...) {
        try { exec_via(ex, "ROLLBACK"); } catch (...) {}
        throw;
    }
}

int64_t migration_version_exec(const sqlift_exec& ex) {
    try {
        auto rows = query_via(ex,
            "SELECT value FROM sqlpipe._sqlift_state WHERE key = 'migration_version'");
        if (rows.empty() || rows[0].empty()) return 0;
        return std::stoll(rows[0][0]);
    } catch (...) {
        return 0;
    }
}

} // namespace sqlift


// --- C wrapper ---------------------------------------------------------------


namespace {

// Duplicate a std::string to a malloc'd C string (caller frees with sqlift_free).
char* sqlift_dup_str(const std::string& s) {
    char* p = static_cast<char*>(std::malloc(s.size() + 1));
    if (p) std::memcpy(p, s.c_str(), s.size() + 1);
    return p;
}

// Set error output pointers. msg is malloc'd; caller frees with sqlift_free.
void sqlift_set_error(int* err_type, char** err_msg, int type, const std::string& msg) {
    if (err_type) *err_type = type;
    if (err_msg)  *err_msg = sqlift_dup_str(msg);
}

void sqlift_clear_error(int* err_type, char** err_msg) {
    if (err_type) *err_type = SQLIFT_OK;
    if (err_msg)  *err_msg = nullptr;
}

// Map a C++ exception to the error type enum.
int classify_exception(const std::exception& e) {
    if (dynamic_cast<const sqlift::ParseError*>(&e))          return SQLIFT_PARSE_ERROR;
    if (dynamic_cast<const sqlift::ExtractError*>(&e))        return SQLIFT_EXTRACT_ERROR;
    if (dynamic_cast<const sqlift::DiffError*>(&e))           return SQLIFT_DIFF_ERROR;
    if (dynamic_cast<const sqlift::DriftError*>(&e))          return SQLIFT_DRIFT_ERROR;
    if (dynamic_cast<const sqlift::DestructiveError*>(&e))    return SQLIFT_DESTRUCTIVE_ERROR;
    if (dynamic_cast<const sqlift::BreakingChangeError*>(&e)) return SQLIFT_BREAKING_CHANGE_ERROR;
    if (dynamic_cast<const sqlift::RebuildError*>(&e))        return SQLIFT_REBUILD_ERROR;
    if (dynamic_cast<const sqlift::JsonError*>(&e))           return SQLIFT_JSON_ERROR;
    if (dynamic_cast<const sqlift::ApplyError*>(&e))          return SQLIFT_APPLY_ERROR;
    if (dynamic_cast<const sqlift::Error*>(&e))               return SQLIFT_ERROR;
    return SQLIFT_ERROR;
}

// Warning JSON serialization (reused by sqlift_diff and sqlift_detect_redundant_indexes).
std::string warnings_to_json(const std::vector<sqlift::Warning>& warnings) {
    std::string s = "[";
    for (size_t i = 0; i < warnings.size(); ++i) {
        if (i > 0) s += ',';
        const auto& w = warnings[i];
        // Manual JSON to avoid pulling nlohmann into this TU via includes.
        // The values are simple strings, no escaping issues in practice.
        s += "{\"type\":\"RedundantIndex\"";
        s += ",\"message\":\"" + w.message + "\"";
        s += ",\"index_name\":\"" + w.index_name + "\"";
        s += ",\"covered_by\":\"" + w.covered_by + "\"";
        s += ",\"table_name\":\"" + w.table_name + "\"}";
    }
    s += "]";
    return s;
}

} // namespace

// --- opaque handle -----------------------------------------------------------

struct sqlift_db {
    int engine = SQLIFT_ENGINE_SQLITE;
    std::unique_ptr<sqlift::Database> sqlite;
    sqlift_exec exec{};

    sqlite3* sqlite_handle() const {
        return sqlite ? sqlite->get() : nullptr;
    }
};

// --- C API -------------------------------------------------------------------

extern "C" {

sqlift_db* sqlift_db_open(const char* path, int flags,
                          int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto* h = new sqlift_db;
        h->engine = SQLIFT_ENGINE_SQLITE;
        h->sqlite = std::make_unique<sqlift::Database>(
            path, flags ? flags : (SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE));
        return h;
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

sqlift_db* sqlift_db_wrap_sqlite(sqlite3* handle,
                                 int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto* h = new sqlift_db;
        h->engine = SQLIFT_ENGINE_SQLITE;
        h->sqlite = std::make_unique<sqlift::Database>(handle);
        return h;
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

sqlift_db* sqlift_db_wrap_exec(int engine, sqlift_exec exec,
                               int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    if (engine != SQLIFT_ENGINE_SQLITE && engine != SQLIFT_ENGINE_POSTGRES) {
        sqlift_set_error(err_type, err_msg, SQLIFT_ERROR, "unknown sqlift_engine");
        return nullptr;
    }
    if (!exec.exec || !exec.query) {
        sqlift_set_error(err_type, err_msg, SQLIFT_ERROR,
                  "sqlift_exec requires exec and query");
        return nullptr;
    }
    auto* h = new sqlift_db;
    h->engine = engine;
    h->exec = exec;
    return h;
}

void sqlift_db_close(sqlift_db* db) {
    delete db;
}

int sqlift_db_exec(sqlift_db* db, const char* sql, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        if (db->engine == SQLIFT_ENGINE_SQLITE) {
            db->sqlite->exec(sql);
        } else {
            char* e = nullptr;
            if (db->exec.exec(db->exec.ctx, sql, &e) != 0) {
                if (err_msg) *err_msg = e;
                else std::free(e);
                return 1;
            }
        }
        return 0;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = sqlift_dup_str(e.what());
        return 1;
    }
}

char* sqlift_parse(const char* ddl, int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::parse(ddl);
        return sqlift_dup_str(sqlift::schema_to_json(schema));
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_extract(sqlift_db* db, int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        sqlift::Schema schema = (db->engine == SQLIFT_ENGINE_SQLITE)
            ? sqlift::extract(db->sqlite_handle())
            : sqlift::extract_exec(db->exec, "public");
        return sqlift_dup_str(sqlift::schema_to_json(schema));
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_parse_on(sqlift_db* db, const char* ddl,
                      int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        if (db->engine == SQLIFT_ENGINE_SQLITE) {
            db->sqlite->exec(ddl);
            auto schema = sqlift::extract(db->sqlite_handle());
            return sqlift_dup_str(sqlift::schema_to_json(schema));
        }
        auto schema = sqlift::parse_on_exec(db->exec, ddl);
        return sqlift_dup_str(sqlift::schema_to_json(schema));
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_diff(const char* current_json, const char* desired_json,
                  int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto current = sqlift::schema_from_json(current_json);
        auto desired = sqlift::schema_from_json(desired_json);
        auto plan = sqlift::diff(current, desired);
        // Include warnings in the plan JSON (they're part of to_json output).
        return sqlift_dup_str(sqlift::to_json(plan));
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

int sqlift_apply(sqlift_db* db, const char* plan_json,
                 const sqlift_apply_options c_opts,
                 int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto plan = sqlift::from_json(plan_json);
        sqlift::ApplyOptions opts;
        opts.allow_destructive = (c_opts.allow & SQLIFT_ALLOW_DESTRUCTIVE) != 0;
        opts.allow_rebuild = (c_opts.allow & SQLIFT_ALLOW_REBUILD) != 0;
        opts.allow_loosen = (c_opts.allow & SQLIFT_ALLOW_LOOSEN) != 0;
        opts.allow_data_dependent = (c_opts.allow & SQLIFT_ALLOW_DATA_DEPENDENT) != 0;
        if (db->engine == SQLIFT_ENGINE_SQLITE)
            sqlift::apply(db->sqlite_handle(), plan, opts);
        else
            sqlift::apply_exec(db->exec, plan, opts);
        return 0;
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return 1;
    }
}

int64_t sqlift_migration_version(sqlift_db* db, int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        if (db->engine == SQLIFT_ENGINE_SQLITE)
            return sqlift::migration_version(db->sqlite_handle());
        return sqlift::migration_version_exec(db->exec);
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return -1;
    }
}

char* sqlift_detect_redundant_indexes(const char* schema_json,
                                      int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::schema_from_json(schema_json);
        auto warnings = sqlift::detect_redundant_indexes(schema);
        return sqlift_dup_str(warnings_to_json(warnings));
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

char* sqlift_schema_hash(const char* schema_json,
                         int* err_type, char** err_msg) {
    sqlift_clear_error(err_type, err_msg);
    try {
        auto schema = sqlift::schema_from_json(schema_json);
        return sqlift_dup_str(schema.hash());
    } catch (const std::exception& e) {
        sqlift_set_error(err_type, err_msg, classify_exception(e), e.what());
        return nullptr;
    }
}

int sqlift_db_query_int64(sqlift_db* db, const char* sql,
                          int64_t* result, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        if (db->engine != SQLIFT_ENGINE_SQLITE) {
            char* e = nullptr;
            auto take = [](void* ctx, int ncols, const char* const* cols) {
                auto* out = static_cast<std::pair<int64_t, bool>*>(ctx);
                if (ncols > 0 && cols[0] && !out->second) {
                    out->first = std::stoll(cols[0]);
                    out->second = true;
                }
            };
            std::pair<int64_t, bool> box{0, false};
            if (db->exec.query(db->exec.ctx, sql, take, &box, &e) != 0) {
                if (err_msg) *err_msg = e;
                else std::free(e);
                return 1;
            }
            if (result) *result = box.first;
            return 0;
        }
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db->sqlite_handle(), sql, -1, &stmt, nullptr);
        if (rc != SQLITE_OK) {
            if (err_msg) *err_msg = sqlift_dup_str(sqlite3_errmsg(db->sqlite_handle()));
            return 1;
        }
        rc = sqlite3_step(stmt);
        if (rc == SQLITE_ROW) {
            if (result) *result = sqlite3_column_int64(stmt, 0);
            sqlite3_finalize(stmt);
            return 0;
        }
        sqlite3_finalize(stmt);
        if (rc == SQLITE_DONE) {
            // No rows -- return 0 as default.
            if (result) *result = 0;
            return 0;
        }
        if (err_msg) *err_msg = sqlift_dup_str(sqlite3_errmsg(db->sqlite_handle()));
        return 1;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = sqlift_dup_str(e.what());
        return 1;
    }
}

char* sqlift_db_query_text(sqlift_db* db, const char* sql, char** err_msg) {
    if (err_msg) *err_msg = nullptr;
    try {
        if (db->engine != SQLIFT_ENGINE_SQLITE) {
            char* e = nullptr;
            std::string text;
            auto take = [](void* ctx, int ncols, const char* const* cols) {
                auto* out = static_cast<std::string*>(ctx);
                if (out->empty() && ncols > 0 && cols[0])
                    *out = cols[0];
            };
            if (db->exec.query(db->exec.ctx, sql, take, &text, &e) != 0) {
                if (err_msg) *err_msg = e;
                else std::free(e);
                return nullptr;
            }
            return sqlift_dup_str(text);
        }
        sqlite3_stmt* stmt = nullptr;
        int rc = sqlite3_prepare_v2(db->sqlite_handle(), sql, -1, &stmt, nullptr);
        if (rc != SQLITE_OK) {
            if (err_msg) *err_msg = sqlift_dup_str(sqlite3_errmsg(db->sqlite_handle()));
            return nullptr;
        }
        rc = sqlite3_step(stmt);
        if (rc == SQLITE_ROW) {
            const char* text = reinterpret_cast<const char*>(sqlite3_column_text(stmt, 0));
            char* result = sqlift_dup_str(text ? text : "");
            sqlite3_finalize(stmt);
            return result;
        }
        sqlite3_finalize(stmt);
        if (rc == SQLITE_DONE) {
            // No rows -- return empty string.
            return sqlift_dup_str("");
        }
        if (err_msg) *err_msg = sqlift_dup_str(sqlite3_errmsg(db->sqlite_handle()));
        return nullptr;
    } catch (const std::exception& e) {
        if (err_msg) *err_msg = sqlift_dup_str(e.what());
        return nullptr;
    }
}

void sqlift_free(void* ptr) {
    std::free(ptr);
}

} // extern "C"

// ── sqlift_db_wrap (sqlpipe shim) ───────────────────────────────────
// Forwards to sqlift_db_wrap_sqlite (borrowed handle, not owned).

extern "C" sqlift_db* sqlift_db_wrap(sqlite3* handle) {
    return sqlift_db_wrap_sqlite(handle, nullptr, nullptr);
}

// ── sqldeep (query transpiler) ──────────────────────────────────
// Source: vendor/src/sqldeep.cpp

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
//
// sqldeep — JSON5-like SQL syntax transpiler.
//
// Implementation strategy: parse the input via deepparser (which knows
// the sqldeep grammar), then walk the resulting AST and rewrite every
// sqldeep-extension node in place into standard SQL nodes. Once the
// AST contains only plain SQL kinds, deepparser's canonical unparser
// emits the final SQL text. Sqldeep itself owns nothing about parsing
// or printing — it is purely an AST-to-AST transformer plus the
// outward-facing C API.


extern "C" {
#include "liteparser.h"
#include "arena.h"
}

#include <algorithm>
#include <climits>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <map>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

namespace sqldeep {

struct ForeignKey {
    std::string from_table;
    std::string to_table;
    struct ColumnPair {
        std::string from_column;
        std::string to_column;
    };
    std::vector<ColumnPair> columns;
};

enum class Backend { sqlite, postgres, sqlite_vanilla };

class Error : public std::runtime_error {
public:
    Error(const std::string& msg, int line, int col)
        : std::runtime_error(msg), line_(line), col_(col) {}
    int line() const { return line_; }
    int col()  const { return col_; }
private:
    int line_;
    int col_;
};

namespace {

// ── FK index ────────────────────────────────────────────────────────

using FkIndex = std::map<std::pair<std::string,std::string>,
                         std::vector<const ForeignKey*>>;

FkIndex build_fk_index(const std::vector<ForeignKey>& fks) {
    FkIndex idx;
    for (const auto& fk : fks) {
        idx[{fk.from_table, fk.to_table}].push_back(&fk);
    }
    return idx;
}

// Convention mode (fk_index == nullptr): child has FK '<parent>_id'.
std::vector<std::pair<std::string,std::string>>
resolve_fk_columns(const std::string& child_table,
                   const std::string& parent_table,
                   const FkIndex* fk_index) {
    if (!fk_index) {
        std::string col = parent_table + "_id";
        return {{col, col}};
    }
    auto it = fk_index->find({child_table, parent_table});
    if (it == fk_index->end() || it->second.empty()) {
        throw Error("no foreign key from '" + child_table + "' to '" +
                    parent_table + "'", 0, 0);
    }
    if (it->second.size() > 1) {
        throw Error("ambiguous foreign key from '" + child_table + "' to '" +
                    parent_table + "' (" + std::to_string(it->second.size()) +
                    " candidates)", 0, 0);
    }
    std::vector<std::pair<std::string,std::string>> cols;
    cols.reserve(it->second[0]->columns.size());
    for (const auto& cp : it->second[0]->columns)
        cols.emplace_back(cp.from_column, cp.to_column);
    return cols;
}

// ── AST construction helpers ─────────────────────────────────────────
//
// Sqldeep mutates the deepparser AST in place and also constructs new
// LpNode objects for replacement subtrees. The helpers below build the
// standard-SQL node kinds we emit; they parallel deepparser's
// grammar-action factories but live outside the parser since we're not
// parsing — we have nothing but an arena to allocate into.

LpNode *new_node(arena_t *arena, LpNodeKind kind) {
    auto *n = static_cast<LpNode*>(arena_zeroalloc(arena, sizeof(LpNode)));
    if (n) n->kind = kind;
    return n;
}

char *arena_str(arena_t *arena, const std::string& s) {
    return arena_strdup(arena, s.c_str());
}

void list_append(arena_t *arena, LpNodeList *list, LpNode *item) {
    if (list->count >= list->capacity) {
        int cap = list->capacity ? list->capacity * 2 : 4;
        auto **items = static_cast<LpNode**>(
            arena_alloc(arena, sizeof(LpNode*) * cap));
        if (!items) return;
        if (list->items)
            std::memcpy(items, list->items, sizeof(LpNode*) * list->count);
        list->items = items;
        list->capacity = cap;
    }
    list->items[list->count++] = item;
}

LpNode *make_string_lit(arena_t *arena, const std::string& value) {
    auto *n = new_node(arena, LP_EXPR_LITERAL_STRING);
    if (n) n->u.literal.value = arena_str(arena, value);
    return n;
}

LpNode *make_int_lit(arena_t *arena, const std::string& digits) {
    auto *n = new_node(arena, LP_EXPR_LITERAL_INT);
    if (n) n->u.literal.value = arena_str(arena, digits);
    return n;
}

LpNode *make_column_ref(arena_t *arena, const std::string& column) {
    auto *n = new_node(arena, LP_EXPR_COLUMN_REF);
    if (n) n->u.column_ref.column = arena_str(arena, column);
    return n;
}

LpNode *make_column_ref2(arena_t *arena, const std::string& table,
                          const std::string& column) {
    auto *n = new_node(arena, LP_EXPR_COLUMN_REF);
    if (!n) return nullptr;
    n->u.column_ref.table  = arena_str(arena, table);
    n->u.column_ref.column = arena_str(arena, column);
    return n;
}

LpNode *make_binop(arena_t *arena, LpBinOp op, LpNode *left, LpNode *right) {
    auto *n = new_node(arena, LP_EXPR_BINARY_OP);
    if (!n) return nullptr;
    n->u.binary.op    = op;
    n->u.binary.left  = left;
    n->u.binary.right = right;
    return n;
}

// AND together a chain of expressions, left-associative.
LpNode *and_chain(arena_t *arena, const std::vector<LpNode*>& parts) {
    if (parts.empty()) return nullptr;
    LpNode *acc = parts[0];
    for (size_t i = 1; i < parts.size(); i++)
        acc = make_binop(arena, LP_OP_AND, acc, parts[i]);
    return acc;
}

LpNode *make_from_table(arena_t *arena, const std::string& name,
                         const std::string& alias) {
    auto *n = new_node(arena, LP_FROM_TABLE);
    if (!n) return nullptr;
    n->u.from_table.name  = arena_str(arena, name);
    if (!alias.empty()) n->u.from_table.alias = arena_str(arena, alias);
    return n;
}

LpNode *make_join(arena_t *arena, LpNode *left, LpNode *right,
                   LpNode *on_expr) {
    auto *n = new_node(arena, LP_JOIN_CLAUSE);
    if (!n) return nullptr;
    n->u.join.left    = left;
    n->u.join.right   = right;
    n->u.join.on_expr = on_expr;
    /* join_type=0 → INNER JOIN; matches sqldeep's existing emission. */
    return n;
}

LpNode *make_function(arena_t *arena, const std::string& name,
                       std::vector<LpNode*> args) {
    auto *n = new_node(arena, LP_EXPR_FUNCTION);
    if (!n) return nullptr;
    n->u.function.name = arena_str(arena, name);
    for (auto *a : args) list_append(arena, &n->u.function.args, a);
    return n;
}

LpNode *make_cast(arena_t *arena, LpNode *expr, const std::string& type_name) {
    auto *n = new_node(arena, LP_EXPR_CAST);
    if (!n) return nullptr;
    n->u.cast.expr = expr;
    n->u.cast.type_name = arena_str(arena, type_name);
    return n;
}

LpNode *make_limit(arena_t *arena, int count) {
    auto *n = new_node(arena, LP_LIMIT);
    if (!n) return nullptr;
    char buf[16];
    std::snprintf(buf, sizeof(buf), "%d", count);
    n->u.limit.count = make_int_lit(arena, buf);
    return n;
}

// ── Positional-parameter order preservation ─────────────────────────
//
// Anonymous positional parameters ("?") are order-sensitive: SQLite and
// PostgreSQL number them by left-to-right textual position, so the Nth
// "?" in the caller's input must remain the Nth "?" in the transpiled
// output, or the caller's bound values silently land on the wrong
// placeholders. Sqldeep's rewrites can move expressions around (most
// obviously the FROM-first SELECT, whose projection is authored last but
// emitted first), so we must guarantee the order survives — or refuse.
//
// Rather than reason about which rewrite reorders what, we verify it
// empirically: before transforming, every "?" is renamed to a unique
// sentinel named parameter (":__sqldeep_param_<N>__") in source order.
// Transforms move AST nodes by kind and structure, never by variable
// name, so each sentinel lands exactly where its "?" would have. After
// unparsing we read the sentinels back out of the output; if they don't
// appear exactly once each, in ascending order, a rewrite reordered
// (or dropped/duplicated) the parameters and we raise an error.
// Otherwise the sentinels are substituted back to "?".
//
// Numbered ("?N") and named (":x" / "@x" / "$x") parameters bind by
// explicit index or name, so their position is irrelevant; they are left
// untouched and pass through unharmed.
constexpr const char *kPosParamPrefix = ":__sqldeep_param_";
constexpr const char *kPosParamSuffix = "__";

// Collect every anonymous "?" variable node in a statement.
void collect_anon_params(LpNode *root, std::vector<LpNode*>& out) {
    struct Ctx { LpVisitor v; std::vector<LpNode*> *out; } ctx{};
    ctx.v.user_data = &ctx;
    ctx.out = &out;
    ctx.v.enter = [](LpVisitor *v, LpNode *nd) -> int {
        if (nd->kind == LP_EXPR_VARIABLE && nd->u.variable.name
            && std::strcmp(nd->u.variable.name, "?") == 0) {
            static_cast<Ctx*>(v->user_data)->out->push_back(nd);
        }
        return 0;
    };
    ctx.v.leave = nullptr;
    lp_ast_walk(root, &ctx.v);
}

// Rename every "?" across all statements to a sentinel named parameter
// numbered 1..N in source (byte-offset) order. Returns N.
int mark_positional_params(LpNodeList *stmts, arena_t *arena) {
    std::vector<LpNode*> params;
    for (int i = 0; i < stmts->count; i++)
        collect_anon_params(stmts->items[i], params);
    std::sort(params.begin(), params.end(), [](LpNode *a, LpNode *b) {
        return a->pos.offset < b->pos.offset;
    });
    for (size_t i = 0; i < params.size(); i++) {
        std::string name = kPosParamPrefix + std::to_string(i + 1)
                         + kPosParamSuffix;
        params[i]->u.variable.name = arena_str(arena, name);
    }
    return static_cast<int>(params.size());
}

// Verify the N sentinels appear exactly once each, in ascending order,
// in the transpiled output; throw if a rewrite reordered them. On
// success, substitute each sentinel back to "?".
void restore_positional_params(std::string& sql, int count) {
    if (count == 0) return;
    const std::string prefix = kPosParamPrefix;
    std::vector<int> order;
    for (size_t pos = 0; (pos = sql.find(prefix, pos)) != std::string::npos;) {
        size_t num = pos + prefix.size();
        size_t end = num;
        while (end < sql.size() && sql[end] >= '0' && sql[end] <= '9') end++;
        order.push_back(std::atoi(sql.substr(num, end - num).c_str()));
        pos = end;
    }
    bool ok = (static_cast<int>(order.size()) == count);
    for (int i = 0; ok && i < count; i++) ok = (order[i] == i + 1);
    if (!ok) {
        throw Error("cannot preserve positional parameter (?) order: a "
                    "rewrite reordered the '?' placeholders, which would "
                    "misbind the caller's values. Use named (:name) or "
                    "numbered (?1) parameters instead.", 0, 0);
    }
    for (int n = 1; n <= count; n++) {
        std::string marker = prefix + std::to_string(n) + kPosParamSuffix;
        for (size_t pos = 0;
             (pos = sql.find(marker, pos)) != std::string::npos;) {
            sql.replace(pos, marker.size(), "?");
            pos += 1;
        }
    }
}

// ── Transformer ─────────────────────────────────────────────────────

constexpr int kMaxNestingDepth = 200;

// XML emission flavour. JSX and JSONML are sqldeep transpiler macros
// — `jsx(<el/>)` / `jsonml(<el/>)` — that select alternative XML
// helper function names (xml_element_jsx, xml_attrs_jsx, jsx_agg vs.
// the plain xml_* family).
enum class XmlMode { Xml, Jsx, Jsonml };

class Transformer {
public:
    Transformer(arena_t *arena, Backend backend, const FkIndex *fk_index)
        : arena_(arena), backend_(backend), fk_index_(fk_index) {}

    // Mutate the AST in place. Returns the (possibly new) root node.
    LpNode *transform(LpNode *node) {
        build_alias_map(node);
        return walk(node, /*depth=*/0, XmlMode::Xml);
    }

private:
    arena_t      *arena_;
    Backend       backend_;
    const FkIndex *fk_index_;

    // alias → underlying table name, collected by a pre-pass over the
    // entire AST so a sqldeep join path like `c->orders o` can resolve
    // the leftmost name `c` (an alias from an enclosing FROM clause)
    // to its real table when building the start-correlation.
    std::map<std::string, std::string> alias_map_;

    // ── Alias-map prepass ─────────────────────────────────────────

    void build_alias_map(LpNode *node) {
        if (!node) return;
        switch (node->kind) {
            case LP_FROM_TABLE:
                if (node->u.from_table.name) {
                    /* Use alias if given, else map the table to itself
                     * so unqualified table names also resolve. */
                    const char *key = node->u.from_table.alias
                                       ? node->u.from_table.alias
                                       : node->u.from_table.name;
                    alias_map_[key] = node->u.from_table.name;
                }
                break;
            case LP_SQLDEEP_JOIN_PATH:
                for (int i = 0; i < node->u.sqldeep_join_path.steps.count; i++) {
                    LpNode *s = node->u.sqldeep_join_path.steps.items[i];
                    if (!s) continue;
                    const auto& ss = s->u.sqldeep_join_step;
                    if (ss.table) {
                        const char *key = ss.alias ? ss.alias : ss.table;
                        alias_map_[key] = ss.table;
                    }
                }
                build_alias_map(node->u.sqldeep_join_path.prefix);
                break;
            default:
                break;
        }
        // Recurse into all child slots that can contain FROM clauses
        // or further sub-selects.
        walk_for_aliases(node);
    }

    void walk_for_aliases(LpNode *node) {
        switch (node->kind) {
            case LP_STMT_SELECT:
                build_alias_map(node->u.select.from);
                for (int i = 0; i < node->u.select.result_columns.count; i++)
                    build_alias_map(node->u.select.result_columns.items[i]);
                for (int i = 0; i < node->u.select.group_by.count; i++)
                    build_alias_map(node->u.select.group_by.items[i]);
                build_alias_map(node->u.select.where);
                build_alias_map(node->u.select.having);
                for (int i = 0; i < node->u.select.order_by.count; i++)
                    build_alias_map(node->u.select.order_by.items[i]);
                build_alias_map(node->u.select.limit);
                build_alias_map(node->u.select.with);
                break;
            case LP_COMPOUND_SELECT:
                build_alias_map(node->u.compound.left);
                build_alias_map(node->u.compound.right);
                break;
            case LP_RESULT_COLUMN:
                build_alias_map(node->u.result_column.expr);
                break;
            case LP_JOIN_CLAUSE:
                build_alias_map(node->u.join.left);
                build_alias_map(node->u.join.right);
                build_alias_map(node->u.join.on_expr);
                break;
            case LP_FROM_SUBQUERY:
                build_alias_map(node->u.from_subquery.select);
                break;
            case LP_EXPR_SUBQUERY:
                build_alias_map(node->u.subquery.select);
                break;
            case LP_EXPR_FUNCTION:
                for (int i = 0; i < node->u.function.args.count; i++)
                    build_alias_map(node->u.function.args.items[i]);
                break;
            case LP_EXPR_SQLDEEP_OBJECT:
                for (int i = 0; i < node->u.sqldeep_object.fields.count; i++)
                    build_alias_map(node->u.sqldeep_object.fields.items[i]);
                break;
            case LP_SQLDEEP_FIELD:
                build_alias_map(node->u.sqldeep_field.value);
                break;
            case LP_EXPR_SQLDEEP_ARRAY:
                for (int i = 0; i < node->u.sqldeep_array.elements.count; i++)
                    build_alias_map(node->u.sqldeep_array.elements.items[i]);
                break;
            case LP_EXPR_SQLDEEP_XML:
                for (int i = 0; i < node->u.sqldeep_xml.children.count; i++)
                    build_alias_map(node->u.sqldeep_xml.children.items[i]);
                for (int i = 0; i < node->u.sqldeep_xml.attrs.count; i++) {
                    LpNode *a = node->u.sqldeep_xml.attrs.items[i];
                    if (a) build_alias_map(a->u.sqldeep_xml_attr.value);
                }
                break;
            case LP_WITH:
                for (int i = 0; i < node->u.with.ctes.count; i++)
                    build_alias_map(node->u.with.ctes.items[i]);
                break;
            case LP_CTE:
                build_alias_map(node->u.cte.select);
                break;
            case LP_STMT_CREATE_VIEW:
                build_alias_map(node->u.create_view.select);
                break;
            default:
                break;
        }
    }

    const std::string& resolve_alias(const std::string& alias) {
        auto it = alias_map_.find(alias);
        return (it != alias_map_.end()) ? it->second : alias;
    }

    // Dialect function names ----------------------------------------

    const char *fn_object() const {
        switch (backend_) {
            case Backend::postgres:       return "jsonb_build_object";
            case Backend::sqlite_vanilla: return "json_object";
            default:                      return "sqldeep_json_object";
        }
    }
    const char *fn_array() const {
        switch (backend_) {
            case Backend::postgres:       return "jsonb_build_array";
            case Backend::sqlite_vanilla: return "json_array";
            default:                      return "sqldeep_json_array";
        }
    }
    const char *fn_group_array() const {
        switch (backend_) {
            case Backend::postgres:       return "jsonb_agg";
            case Backend::sqlite_vanilla: return "json_group_array";
            default:                      return "sqldeep_json_group_array";
        }
    }
    // BLOB-protocol wrapper used to inject a typed JSON value into a
    // JSON-building call so booleans (and only booleans, currently)
    // round-trip as `true` / `false` rather than `1` / `0`. Postgres
    // jsonb already has a native bool, so no wrapper is needed there.
    const char *fn_json_blob() const {
        switch (backend_) {
            case Backend::postgres:       return nullptr;
            case Backend::sqlite_vanilla: return "json";
            default:                      return "sqldeep_json";
        }
    }

    // Detect a bare `true` / `false` literal used as a value. SQLite's
    // grammar doesn't have boolean keywords — `true`/`false` parse as
    // identifier column refs (with no table qualifier). Returns the
    // canonical lower-case form, or NULL if `expr` isn't one of those.
    static const char *bool_literal_text(LpNode *expr) {
        if (!expr) return nullptr;
        const char *name = nullptr;
        if (expr->kind == LP_EXPR_LITERAL_BOOL) {
            name = expr->u.literal.value;
        } else if (expr->kind == LP_EXPR_COLUMN_REF
                && !expr->u.column_ref.schema
                && !expr->u.column_ref.table) {
            name = expr->u.column_ref.column;
        }
        if (!name) return nullptr;
        if ((name[0] == 't' || name[0] == 'T')
            && (name[1] == 'r' || name[1] == 'R')
            && (name[2] == 'u' || name[2] == 'U')
            && (name[3] == 'e' || name[3] == 'E')
            && name[4] == '\0')
            return "true";
        if ((name[0] == 'f' || name[0] == 'F')
            && (name[1] == 'a' || name[1] == 'A')
            && (name[2] == 'l' || name[2] == 'L')
            && (name[3] == 's' || name[3] == 'S')
            && (name[4] == 'e' || name[4] == 'E')
            && name[5] == '\0')
            return "false";
        return nullptr;
    }

    // Wrap a bare `true` / `false` literal in fn_json('true') /
    // fn_json('false') when it appears in a JSON value slot — object
    // field value, array element, or (later) XML attribute value.
    // No wrap in Postgres mode (jsonb has native bool).
    LpNode *wrap_json_bool(LpNode *expr) {
        const char *lit = bool_literal_text(expr);
        if (!lit) return expr;
        const char *fn = fn_json_blob();
        if (!fn) return expr;
        return make_function(arena_, fn, {make_string_lit(arena_, lit)});
    }

    // ── Walker ────────────────────────────────────────────────────
    //
    // Visit every node in post-order: children first, then the node
    // itself. By the time we rewrite a sqldeep node, its children are
    // already plain-SQL AST.

    LpNode *walk(LpNode *node, int depth, XmlMode mode) {
        if (!node) return nullptr;
        if (depth > kMaxNestingDepth)
            throw Error("maximum nesting depth exceeded", 0, 0);

        // Recursive SELECT (sqldeep_recurse) is handled pre-order:
        // the whole statement is rewritten into a WITH RECURSIVE
        // 3-CTE construction so we don't want the post-order walk
        // to start rewriting the object literal projection (its
        // recursive-children marker still needs reading).
        if (node->kind == LP_STMT_SELECT && node->u.select.sqldeep_recurse) {
            return rewrite_recursive_select(node);
        }

        // jsx(<el/>) / jsonml(<el/>) transpiler-macro wrapper: absorb
        // the outer function call and switch the inner XML element's
        // emission mode. Detected pre-recursion so the mode propagates
        // down through nested XML children.
        if (node->kind == LP_EXPR_FUNCTION
            && node->u.function.name
            && node->u.function.args.count == 1
            && node->u.function.args.items[0]
            && node->u.function.args.items[0]->kind == LP_EXPR_SQLDEEP_XML) {
            XmlMode m = XmlMode::Xml;
            const char *fn = node->u.function.name;
            if (std::strcmp(fn, "jsx") == 0)        m = XmlMode::Jsx;
            else if (std::strcmp(fn, "jsonml") == 0) m = XmlMode::Jsonml;
            if (m != XmlMode::Xml) {
                LpNode *xml = node->u.function.args.items[0];
                walk_xml_children(xml, depth + 1, m);
                return rewrite_xml(xml, m);
            }
        }

        walk_children(node, depth + 1, mode);

        switch (node->kind) {
            case LP_EXPR_SQLDEEP_OBJECT:    return rewrite_object(node);
            case LP_EXPR_SQLDEEP_ARRAY:     return rewrite_array(node);
            case LP_EXPR_SQLDEEP_JSON_PATH: return rewrite_json_path(node);
            case LP_EXPR_SQLDEEP_XML:       return rewrite_xml(node, mode);
            case LP_STMT_SELECT:            return rewrite_select(node);
            default:                        return node;
        }
    }

    // Recurse into every child slot that can contain an LpNode or an
    // LpNodeList. This is the bulk of the walker — every node kind
    // that can contain an expression or statement child must list
    // those children here. Anything that's a leaf (literals, naked
    // column refs, identifiers) has nothing to do.

    void walk_children(LpNode *node, int depth, XmlMode mode) {
        switch (node->kind) {
            case LP_STMT_SELECT:
                walk_list(&node->u.select.result_columns, depth, mode);
                node->u.select.from   = walk(node->u.select.from, depth, mode);
                node->u.select.where  = walk(node->u.select.where, depth, mode);
                walk_list(&node->u.select.group_by, depth, mode);
                node->u.select.having = walk(node->u.select.having, depth, mode);
                walk_list(&node->u.select.order_by, depth, mode);
                node->u.select.limit  = walk(node->u.select.limit, depth, mode);
                walk_list(&node->u.select.window_defs, depth, mode);
                node->u.select.with   = walk(node->u.select.with, depth, mode);
                break;
            case LP_COMPOUND_SELECT:
                node->u.compound.left  = walk(node->u.compound.left, depth, mode);
                node->u.compound.right = walk(node->u.compound.right, depth, mode);
                break;
            case LP_RESULT_COLUMN:
                node->u.result_column.expr =
                    walk(node->u.result_column.expr, depth, mode);
                break;
            case LP_EXPR_BINARY_OP:
                node->u.binary.left  = walk(node->u.binary.left, depth, mode);
                node->u.binary.right = walk(node->u.binary.right, depth, mode);
                break;
            case LP_EXPR_UNARY_OP:
                node->u.unary.operand = walk(node->u.unary.operand, depth, mode);
                break;
            case LP_EXPR_FUNCTION:
                walk_list(&node->u.function.args, depth, mode);
                walk_list(&node->u.function.order_by, depth, mode);
                node->u.function.filter = walk(node->u.function.filter, depth, mode);
                node->u.function.over   = walk(node->u.function.over, depth, mode);
                break;
            case LP_EXPR_CAST:
                node->u.cast.expr = walk(node->u.cast.expr, depth, mode);
                break;
            case LP_EXPR_COLLATE:
                node->u.collate.expr = walk(node->u.collate.expr, depth, mode);
                break;
            case LP_EXPR_BETWEEN:
                node->u.between.expr = walk(node->u.between.expr, depth, mode);
                node->u.between.low  = walk(node->u.between.low, depth, mode);
                node->u.between.high = walk(node->u.between.high, depth, mode);
                break;
            case LP_EXPR_IN:
                node->u.in.expr   = walk(node->u.in.expr, depth, mode);
                node->u.in.select = walk(node->u.in.select, depth, mode);
                walk_list(&node->u.in.values, depth, mode);
                break;
            case LP_EXPR_EXISTS:
                node->u.exists.select = walk(node->u.exists.select, depth, mode);
                break;
            case LP_EXPR_SUBQUERY:
                node->u.subquery.select = walk(node->u.subquery.select, depth, mode);
                break;
            case LP_EXPR_CASE:
                node->u.case_.operand = walk(node->u.case_.operand, depth, mode);
                walk_list(&node->u.case_.when_exprs, depth, mode);
                node->u.case_.else_expr = walk(node->u.case_.else_expr, depth, mode);
                break;
            case LP_FROM_SUBQUERY:
                node->u.from_subquery.select =
                    walk(node->u.from_subquery.select, depth, mode);
                break;
            case LP_JOIN_CLAUSE:
                node->u.join.left    = walk(node->u.join.left, depth, mode);
                node->u.join.right   = walk(node->u.join.right, depth, mode);
                node->u.join.on_expr = walk(node->u.join.on_expr, depth, mode);
                break;
            case LP_ORDER_TERM:
                node->u.order_term.expr =
                    walk(node->u.order_term.expr, depth, mode);
                break;
            case LP_LIMIT:
                node->u.limit.count  = walk(node->u.limit.count, depth, mode);
                node->u.limit.offset = walk(node->u.limit.offset, depth, mode);
                break;
            case LP_EXPR_SQLDEEP_OBJECT:
                walk_list(&node->u.sqldeep_object.fields, depth, mode);
                break;
            case LP_SQLDEEP_FIELD:
                node->u.sqldeep_field.key_expr =
                    walk(node->u.sqldeep_field.key_expr, depth, mode);
                node->u.sqldeep_field.value =
                    walk(node->u.sqldeep_field.value, depth, mode);
                break;
            case LP_EXPR_SQLDEEP_ARRAY:
                walk_list(&node->u.sqldeep_array.elements, depth, mode);
                break;
            case LP_EXPR_SQLDEEP_JSON_PATH:
                node->u.sqldeep_json_path.base =
                    walk(node->u.sqldeep_json_path.base, depth, mode);
                break;
            case LP_EXPR_SQLDEEP_XML:
                walk_xml_children(node, depth, mode);
                break;
            case LP_STMT_CREATE_VIEW:
                node->u.create_view.select =
                    walk(node->u.create_view.select, depth, mode);
                break;
            case LP_STMT_INSERT:
                node->u.insert.source = walk(node->u.insert.source, depth, mode);
                node->u.insert.upsert = walk(node->u.insert.upsert, depth, mode);
                walk_list(&node->u.insert.returning, depth, mode);
                break;
            case LP_STMT_UPDATE:
                walk_list(&node->u.update.set_clauses, depth, mode);
                node->u.update.from   = walk(node->u.update.from, depth, mode);
                node->u.update.where  = walk(node->u.update.where, depth, mode);
                walk_list(&node->u.update.order_by, depth, mode);
                node->u.update.limit  = walk(node->u.update.limit, depth, mode);
                walk_list(&node->u.update.returning, depth, mode);
                break;
            case LP_STMT_DELETE:
                node->u.del.where = walk(node->u.del.where, depth, mode);
                walk_list(&node->u.del.order_by, depth, mode);
                node->u.del.limit = walk(node->u.del.limit, depth, mode);
                walk_list(&node->u.del.returning, depth, mode);
                break;
            case LP_WITH:
                walk_list(&node->u.with.ctes, depth, mode);
                break;
            case LP_CTE:
                node->u.cte.select = walk(node->u.cte.select, depth, mode);
                break;
            default:
                break;  // leaves and not-yet-handled cases
        }
    }

    void walk_list(LpNodeList *list, int depth, XmlMode mode) {
        if (!list) return;
        for (int i = 0; i < list->count; i++)
            list->items[i] = walk(list->items[i], depth, mode);
    }

    // XML elements propagate the active mode through nested attributes
    // and children so jsx(<ul><li/></ul>) emits xml_element_jsx for
    // both the outer ul and the inner li.
    void walk_xml_children(LpNode *node, int depth, XmlMode mode) {
        auto& x = node->u.sqldeep_xml;
        for (int i = 0; i < x.attrs.count; i++) {
            LpNode *a = x.attrs.items[i];
            if (!a) continue;
            a->u.sqldeep_xml_attr.value =
                walk(a->u.sqldeep_xml_attr.value, depth, mode);
        }
        for (int i = 0; i < x.children.count; i++)
            x.children.items[i] = walk(x.children.items[i], depth, mode);
    }

    // ── Rewrites ─────────────────────────────────────────────────

    // Detect the "aggregate field" form: a sqldeep field whose value
    // is a SELECT with no FROM clause, exactly one result column, and
    // no other clauses. In sqldeep, `{field: SELECT expr}` (no FROM)
    // means "aggregate `expr` over the current GROUP BY scope" and
    // becomes 'field', fn_group_array(expr) — or just 'field', expr
    // when singular (SELECT/1).
    //
    // The grammar wraps the bare SELECT in LP_EXPR_SUBQUERY; we unwrap
    // it back to the right expression here.
    LpNode *maybe_aggregate_field_value(LpNode *v) {
        if (!v || v->kind != LP_EXPR_SUBQUERY) return v;
        LpNode *sel = v->u.subquery.select;
        if (!sel || sel->kind != LP_STMT_SELECT) return v;
        const auto& s = sel->u.select;
        if (s.from || s.where || s.having || s.with) return v;
        if (s.group_by.count || s.order_by.count || s.window_defs.count) return v;
        if (s.result_columns.count != 1) return v;
        LpNode *rc = s.result_columns.items[0];
        LpNode *expr = (rc && rc->kind == LP_RESULT_COLUMN)
                          ? rc->u.result_column.expr : rc;
        if (!expr) return v;
        // LIMIT 1 (from SELECT/1) → just the bare expr.
        if (s.limit) return expr;
        return make_function(arena_, fn_group_array(), {expr});
    }

    // { a, key: expr, "k": v, (e): v, a.b } →
    //   fn_object('a', a, 'key', expr, 'k', v, e, v, 'b', a.b)
    // Literal `true` / `false` values are wrapped via sqldeep_json('...')
    // so they serialize as JSON bools rather than integers (sqlite/vanilla).
    // Aggregate field form `field: SELECT expr` collapses to
    // 'field', fn_group_array(expr).
    LpNode *rewrite_object(LpNode *node) {
        std::vector<LpNode*> args;
        const auto& fields = node->u.sqldeep_object.fields;
        for (int i = 0; i < fields.count; i++) {
            LpNode *f = fields.items[i];
            if (!f) continue;
            const auto& sf = f->u.sqldeep_field;
            switch (sf.key_form) {
                case 0: { // bare
                    args.push_back(make_string_lit(arena_, sf.key_text));
                    args.push_back(make_column_ref(arena_, sf.key_text));
                    break;
                }
                case 1:  // named id : expr
                case 2:  // "string" : expr
                case 5: { // qualified  a.b
                    LpNode *v = maybe_aggregate_field_value(sf.value);
                    args.push_back(make_string_lit(arena_, sf.key_text));
                    args.push_back(wrap_json_bool(v));
                    break;
                }
                case 3: {  // (expr) : val
                    LpNode *v = maybe_aggregate_field_value(sf.value);
                    args.push_back(sf.key_expr);
                    args.push_back(wrap_json_bool(v));
                    break;
                }
                case 4:  // recursive children
                    // `children: *` only has meaning inside a SELECT
                    // that has a RECURSE clause — when we get here in
                    // the post-order walk, the recursive-SELECT
                    // pre-handler already short-circuited that case.
                    // Reaching this branch means the recursive field
                    // appeared without RECURSE; surface as an error.
                    throw Error("`*` recursive child field requires "
                                "a RECURSE clause on the SELECT", 0, 0);
                default:
                    break;
            }
        }
        node->kind = LP_EXPR_FUNCTION;
        std::memset(&node->u, 0, sizeof(node->u));
        node->u.function.name = arena_strdup(arena_, fn_object());
        for (auto *a : args) list_append(arena_, &node->u.function.args, a);
        return node;
    }

    // [ e1, e2, ... ] → fn_array(e1, e2, ...) with bool wrapping.
    LpNode *rewrite_array(LpNode *node) {
        LpNodeList elements = node->u.sqldeep_array.elements;
        node->kind = LP_EXPR_FUNCTION;
        std::memset(&node->u, 0, sizeof(node->u));
        node->u.function.name = arena_strdup(arena_, fn_array());
        for (int i = 0; i < elements.count; i++)
            list_append(arena_, &node->u.function.args,
                        wrap_json_bool(elements.items[i]));
        return node;
    }

    // (base).a.b[0] →
    //   SQLite/Vanilla: json_extract(CAST((base) AS TEXT), '$.a.b[0]')
    //   Postgres:       jsonb_extract_path(base, 'a', 'b', '0')
    LpNode *rewrite_json_path(LpNode *node) {
        LpNode *base = node->u.sqldeep_json_path.base;
        LpNodeList segs = node->u.sqldeep_json_path.segments;

        if (backend_ == Backend::postgres) {
            std::vector<LpNode*> args;
            args.push_back(base);
            for (int i = 0; i < segs.count; i++) {
                LpNode *seg = segs.items[i];
                if (!seg) continue;
                args.push_back(make_string_lit(arena_,
                    seg->u.literal.value ? seg->u.literal.value : ""));
            }
            node->kind = LP_EXPR_FUNCTION;
            std::memset(&node->u, 0, sizeof(node->u));
            node->u.function.name = arena_strdup(arena_, "jsonb_extract_path");
            for (auto *a : args) list_append(arena_, &node->u.function.args, a);
            return node;
        }

        // Build "$.a.b[0]" path string.
        std::string path = "$";
        for (int i = 0; i < segs.count; i++) {
            LpNode *seg = segs.items[i];
            if (!seg) continue;
            const char *v = seg->u.literal.value ? seg->u.literal.value : "";
            if (seg->kind == LP_EXPR_LITERAL_INT) {
                path += "[";
                path += v;
                path += "]";
            } else {
                path += ".";
                path += v;
            }
        }

        // Wrap base in a 1-element vector so the canonical unparser
        // emits `(base)`. The parens preserve the visual disambiguation
        // (matches sqldeep's pre-existing renderer) and avoid any
        // operator-precedence surprises inside the CAST argument.
        LpNode *parens = new_node(arena_, LP_EXPR_VECTOR);
        list_append(arena_, &parens->u.vector.values, base);
        LpNode *cast = make_cast(arena_, parens, "TEXT");
        LpNode *path_lit = make_string_lit(arena_, path);

        node->kind = LP_EXPR_FUNCTION;
        std::memset(&node->u, 0, sizeof(node->u));
        node->u.function.name = arena_strdup(arena_, "json_extract");
        list_append(arena_, &node->u.function.args, cast);
        list_append(arena_, &node->u.function.args, path_lit);
        return node;
    }

    // Multi-line XML text dedent: scan immediate text children for
    // the common leading-whitespace prefix on each line after a '\n'.
    // Blank-interior lines (only whitespace between two newlines) are
    // skipped from the indent calculation but still trimmed on output.
    int xml_min_indent(LpNode *node) {
        int min_indent = INT_MAX;
        const auto& x = node->u.sqldeep_xml;
        for (int j = 0; j < x.children.count; j++) {
            LpNode *c = x.children.items[j];
            if (!c || c->kind != LP_SQLDEEP_XML_TEXT) continue;
            const char *text = c->u.sqldeep_xml_text.text;
            if (!text) continue;
            size_t i = 0, n = std::strlen(text);
            while (i < n) {
                const char *nl = (const char *)std::memchr(text + i, '\n', n - i);
                if (!nl) break;
                size_t ls = (nl - text) + 1;
                int sp = 0;
                while (ls + sp < n && text[ls + sp] == ' ') ++sp;
                // Skip only fully-blank interior lines (whitespace
                // between two newlines). Lines whose only "content"
                // is whitespace before a child element or closing tag
                // (the text-end case) still carry meaningful indent.
                if (ls + sp < n && text[ls + sp] == '\n') {
                    i = ls; continue;
                }
                if (sp < min_indent) min_indent = sp;
                i = ls + sp + 1;
            }
        }
        return min_indent == INT_MAX ? 0 : min_indent;
    }

    // Apply dedent: drop `indent` spaces immediately after every '\n'
    // in `text`. Returns a new arena-owned string.
    char *xml_dedent_text(const char *text, int indent) {
        if (!text || indent <= 0)
            return arena_strdup(arena_, text ? text : "");
        size_t n = std::strlen(text);
        std::string out;
        out.reserve(n);
        for (size_t i = 0; i < n; ) {
            char c = text[i++];
            out += c;
            if (c == '\n') {
                int dropped = 0;
                while (dropped < indent && i < n && text[i] == ' ') {
                    i++; dropped++;
                }
            }
        }
        return arena_strdup(arena_, out.c_str());
    }

    // <tag attrs>body</tag> →
    //   xml_element('tag', xml_attrs(...), child1, child2, ...)
    // Self-closing <tag/> uses 'tag/' as the literal string so the
    // runtime function knows to emit the void-element form.
    // Mode (XML / Jsonml / Jsx) selects the function family.
    LpNode *rewrite_xml(LpNode *node, XmlMode mode) {
        const auto& x = node->u.sqldeep_xml;
        const char *fn_el, *fn_attrs;
        switch (mode) {
            case XmlMode::Jsx:
                fn_el = "xml_element_jsx";
                fn_attrs = "xml_attrs_jsx";
                break;
            case XmlMode::Jsonml:
                fn_el = "xml_element_jsonml";
                fn_attrs = "xml_attrs_jsonml";
                break;
            default:
                fn_el = "xml_element";
                fn_attrs = "xml_attrs";
                break;
        }

        std::vector<LpNode*> args;

        // tag: bare for non-self-closing, with trailing "/" marker
        // for self-closing so the runtime emits <tag/> form.
        std::string tag = x.tag ? x.tag : "";
        if (x.self_closing) tag += "/";
        args.push_back(make_string_lit(arena_, tag));

        // attrs: xml_attrs('name', value, ...). Boolean attributes
        // (no `=value`) become sqldeep_json('true') wrappers via the
        // BLOB protocol so the runtime distinguishes them from
        // attr="1" / attr="true" string values.
        if (x.attrs.count > 0) {
            std::vector<LpNode*> attr_args;
            for (int i = 0; i < x.attrs.count; i++) {
                LpNode *a = x.attrs.items[i];
                if (!a) continue;
                const auto& av = a->u.sqldeep_xml_attr;
                attr_args.push_back(make_string_lit(arena_, av.name));
                if (av.value) {
                    if (av.dynamic) {
                        attr_args.push_back(wrap_json_bool(av.value));
                    } else {
                        attr_args.push_back(av.value);
                    }
                } else {
                    // Boolean attribute: <input disabled/>
                    const char *fn = fn_json_blob();
                    if (fn) {
                        attr_args.push_back(make_function(
                            arena_, fn,
                            {make_string_lit(arena_, "true")}));
                    } else {
                        attr_args.push_back(make_string_lit(arena_, "true"));
                    }
                }
            }
            args.push_back(make_function(arena_, fn_attrs, attr_args));
        }

        // Multi-line dedent: scan all direct-text children for the
        // common leading-whitespace indent, then strip that indent
        // from each line's start. Source indentation produces the
        // relative indentation in the rendered output.
        int indent = xml_min_indent(node);

        // children: text → string literal (dedented); nested element
        // → recurse (already rewritten in post-order); anything else
        // is an interpolation expression, with bool wrap applied.
        for (int i = 0; i < x.children.count; i++) {
            LpNode *c = x.children.items[i];
            if (!c) continue;
            if (c->kind == LP_SQLDEEP_XML_TEXT) {
                const char *raw = c->u.sqldeep_xml_text.text;
                char *dedented = xml_dedent_text(raw, indent);
                args.push_back(make_string_lit(arena_,
                    dedented ? std::string(dedented) : std::string("")));
            } else {
                args.push_back(wrap_json_bool(c));
            }
        }

        node->kind = LP_EXPR_FUNCTION;
        std::memset(&node->u, 0, sizeof(node->u));
        node->u.function.name = arena_strdup(arena_, fn_el);
        for (auto *a : args) list_append(arena_, &node->u.function.args, a);
        return node;
    }

    // ── Recursive SELECT rewrite ──────────────────────────────────
    //
    // Expands
    //   SELECT { id, name, children: * } FROM t RECURSE ON (fk [= pk]) WHERE ...
    // into the 3-CTE bracket-injection template documented in
    // sqldeep's CLAUDE.md (DFS traversal, per-node JSON object, then
    // event fragments concatenated to form one nested JSON string).
    //
    // Because the WITH RECURSIVE shape is large and well-defined, we
    // build it as SQL text and re-parse via deepparser to obtain the
    // standard SQL AST. The transformer then returns the new AST in
    // place of the original SELECT.

    LpNode *rewrite_recursive_select(LpNode *node) {
        const auto& sel = node->u.select;
        LpNode *recurse = sel.sqldeep_recurse;
        if (!recurse) return node;

        std::string fk_col = recurse->u.sqldeep_recurse.fk_col
                                ? recurse->u.sqldeep_recurse.fk_col : "";
        std::string pk_col = recurse->u.sqldeep_recurse.pk_col
                                ? recurse->u.sqldeep_recurse.pk_col : "id";

        // The FROM target must be a plain LP_FROM_TABLE.
        if (!sel.from || sel.from->kind != LP_FROM_TABLE) {
            throw Error("RECURSE requires a single FROM table", 0, 0);
        }
        std::string table = sel.from->u.from_table.name
                              ? sel.from->u.from_table.name : "";

        // Projection must be a sqldeep object literal with a recursive
        // children field. Extract field info.
        if (sel.result_columns.count != 1) {
            throw Error("RECURSE requires a single object projection", 0, 0);
        }
        LpNode *rc = sel.result_columns.items[0];
        LpNode *proj = (rc && rc->kind == LP_RESULT_COLUMN)
                          ? rc->u.result_column.expr : rc;
        if (!proj || proj->kind != LP_EXPR_SQLDEEP_OBJECT) {
            throw Error("RECURSE requires a sqldeep object projection",
                        0, 0);
        }

        std::string children_field;
        std::vector<std::string> field_keys;   // column-name used in CTE
        std::vector<std::string> obj_pairs;    // "'key', expr" for json_object
        for (int i = 0; i < proj->u.sqldeep_object.fields.count; i++) {
            LpNode *f = proj->u.sqldeep_object.fields.items[i];
            if (!f) continue;
            const auto& sf = f->u.sqldeep_field;
            if (sf.key_form == 4) {           // recursive children marker
                children_field = sf.key_text ? sf.key_text : "children";
                continue;
            }
            // Only bare and qualified-bare are supported here — those
            // are the forms that map cleanly to "include this column
            // in the DFS CTE column list".
            std::string col = sf.key_text ? sf.key_text : "";
            field_keys.push_back(col);
            obj_pairs.push_back("'" + sql_escape_lit(col) + "', " + col);
        }
        if (children_field.empty()) {
            throw Error("RECURSE requires a `children: *` field in the "
                        "projection", 0, 0);
        }

        // Root condition (the SELECT's WHERE clause); we emit it back
        // into the anchor leg of the recursive CTE. The canonical
        // unparser produces the SQL text.
        std::string root_where;
        if (sel.where) {
            char *w = lp_ast_to_sql(sel.where, transient_arena());
            if (w) root_where = w;
        }

        bool singular = !!sel.sqldeep_singular;
        bool is_pg = (backend_ == Backend::postgres);

        // Build column list. The CTE always includes fk_col; pk_col
        // is added separately when it differs from fk_col and isn't
        // already in the field list.
        std::string col_list;
        std::string col_select;  // "c.col1, c.col2, ..." for recursive step
        for (size_t i = 0; i < field_keys.size(); i++) {
            if (i > 0) { col_list += ", "; col_select += ", "; }
            col_list += field_keys[i];
            col_select += "c." + field_keys[i];
        }
        if (!field_keys.empty()) { col_list += ", "; col_select += ", "; }
        col_list += fk_col;
        col_select += "c." + fk_col;
        bool pk_in_fields = false;
        for (const auto& f : field_keys)
            if (f == pk_col) { pk_in_fields = true; break; }
        if (pk_col != fk_col && !pk_in_fields) {
            col_list += ", " + pk_col;
            col_select += ", c." + pk_col;
        }

        std::string pad   = is_pg
            ? "lpad(CAST(" + pk_col + " AS text), 10, '0')"
            : "printf('%010d', " + pk_col + ")";
        std::string c_pad = is_pg
            ? "lpad(CAST(c." + pk_col + " AS text), 10, '0')"
            : "printf('%010d', c." + pk_col + ")";
        std::string concat_fn = is_pg
            ? "string_agg(_fragment, '' ORDER BY _sort_key)"
            : "group_concat(_fragment, '')";
        std::string high_char = is_pg ? "chr(127)" : "char(127)";

        // Build the obj_args for fn_object call.
        std::string obj_args;
        for (size_t i = 0; i < obj_pairs.size(); i++) {
            if (i > 0) obj_args += ", ";
            obj_args += obj_pairs[i];
        }

        std::string out;
        out += "WITH RECURSIVE _sdq_dfs(";
        out += col_list;
        out += ", _depth, _path) AS (SELECT ";
        out += col_list;
        out += ", 0, ";
        out += pad;
        out += " FROM ";
        out += table;
        if (!root_where.empty()) {
            out += " WHERE ";
            out += root_where;
        }
        out += " UNION ALL SELECT ";
        out += col_select;
        out += ", d._depth + 1, d._path || '/' || ";
        out += c_pad;
        out += " FROM ";
        out += table;
        out += " c JOIN _sdq_dfs d ON c.";
        out += fk_col;
        out += " = d.";
        out += pk_col;
        out += "), _sdq_ranked AS (SELECT *, ";
        out += fn_object();
        out += "(";
        out += obj_args;
        out += ") AS _obj, ROW_NUMBER() OVER (PARTITION BY ";
        out += fk_col;
        out += " ORDER BY ";
        out += pk_col;
        out += ") AS _child_rank FROM _sdq_dfs), ";
        out += "_sdq_events(_sort_key, _fragment) AS (SELECT _path, ";
        out += "CASE WHEN _child_rank > 1 THEN ',' ELSE '' END || ";
        out += "substr(_obj, 1, length(_obj) - 1) || ',\"";
        out += sql_escape_lit(children_field);
        out += "\":[' FROM _sdq_ranked UNION ALL SELECT _path || ";
        out += high_char;
        out += ", ']}' FROM _sdq_ranked) SELECT ";
        if (!singular) out += "'[' || ";
        out += concat_fn;
        if (!singular) out += " || ']'";
        out += " FROM (SELECT _fragment FROM _sdq_events ORDER BY _sort_key)";

        // Re-parse the constructed SQL via deepparser so we hand back
        // a standard SQL AST. (Re-using deepparser this way costs one
        // extra parse but spares us building a multi-CTE AST by hand.)
        const char *err = nullptr;
        LpNode *new_ast = lp_parse(out.c_str(), arena_, &err);
        if (!new_ast) {
            std::string msg = err ? err : "internal: recursive expansion failed";
            throw Error(msg, 0, 0);
        }
        return new_ast;
    }

    // Helper: escape single quotes in a string for a SQL string literal.
    static std::string sql_escape_lit(const std::string& s) {
        std::string out;
        for (char c : s) {
            if (c == '\'') out += "''";
            else out += c;
        }
        return out;
    }

    // The recursive-SELECT rewrite needs to emit the existing WHERE
    // clause as SQL text. The transient arena is just the regular
    // arena — it owns those strings for the lifetime of the parse.
    arena_t *transient_arena() { return arena_; }

    // ── Join path rewrite ─────────────────────────────────────────
    //
    // FROM c->orders o ON|USING ... -> ... AST representation:
    //   LP_SQLDEEP_JOIN_PATH { prefix, start_alias, steps[] }
    //
    // becomes:
    //   FROM step1.table [alias] JOIN step2.table [alias] ON ... ...
    //   WHERE step1 ↔ start  (added to the enclosing SELECT)

    struct JoinColumnPair {
        std::string child_col;
        std::string parent_col;
    };

    // Convention-based resolution: child has FK `<parent>_id`,
    // parent's PK is the same name. FK-guided mode looks up via
    // fk_index_; ambiguous or missing entries throw.
    std::vector<JoinColumnPair>
    resolve_columns(const std::string& child_table,
                     const std::string& parent_table) {
        if (!fk_index_) {
            return {{parent_table + "_id", parent_table + "_id"}};
        }
        auto it = fk_index_->find({child_table, parent_table});
        if (it == fk_index_->end() || it->second.empty()) {
            throw Error("no foreign key from '" + child_table + "' to '"
                        + parent_table + "'", 0, 0);
        }
        if (it->second.size() > 1) {
            throw Error("ambiguous foreign key from '" + child_table +
                        "' to '" + parent_table + "' (" +
                        std::to_string(it->second.size()) +
                        " candidates)", 0, 0);
        }
        std::vector<JoinColumnPair> out;
        for (const auto& cp : it->second[0]->columns)
            out.push_back({cp.from_column, cp.to_column});
        return out;
    }

    // Build a chain of LP_FROM_TABLE / LP_JOIN_CLAUSE for the path
    // and emit the start-correlation WHERE clause into *where_out.
    // Returns the from-chain node.
    LpNode *rewrite_join_path(LpNode *jp, LpNode **where_out) {
        const auto& path = jp->u.sqldeep_join_path;
        if (path.steps.count == 0) {
            *where_out = nullptr;
            return jp;
        }

        std::string start_alias = path.start_alias ? path.start_alias : "";
        // The leftmost name must refer to a table or alias already in
        // scope (from an outer FROM clause or another step). Unknown
        // names are sqldeep errors — there's no implicit "anything
        // goes" auto-join base.
        auto it = alias_map_.find(start_alias);
        if (it == alias_map_.end()) {
            throw Error("unknown join alias '" + start_alias + "'", 0, 0);
        }
        std::string start_table = it->second;

        // First step.
        LpNode *step1 = path.steps.items[0];
        const auto& s1 = step1->u.sqldeep_join_step;
        std::string s1_table = s1.table ? s1.table : "";
        std::string s1_alias = s1.alias ? s1.alias : "";
        std::string s1_ref   = s1_alias.empty() ? s1_table : s1_alias;

        // FROM target for the chain.
        LpNode *chain = make_from_table(arena_, s1_table, s1_alias);

        // WHERE correlation: step1 ↔ start.
        *where_out = build_join_condition(step1, start_alias, start_table,
                                           s1_ref, s1_table);

        std::string prev_ref   = s1_ref;
        std::string prev_table = s1_table;

        // Subsequent steps become JOIN ... ON ....
        for (int i = 1; i < path.steps.count; i++) {
            LpNode *step = path.steps.items[i];
            const auto& s = step->u.sqldeep_join_step;
            std::string s_table = s.table ? s.table : "";
            std::string s_alias = s.alias ? s.alias : "";
            std::string s_ref   = s_alias.empty() ? s_table : s_alias;

            LpNode *table_node = make_from_table(arena_, s_table, s_alias);
            LpNode *on_expr =
                build_join_condition(step, prev_ref, prev_table,
                                      s_ref, s_table);
            chain = make_join(arena_, chain, table_node, on_expr);

            prev_ref   = s_ref;
            prev_table = s_table;
        }

        return chain;
    }

    // Build the boolean expression connecting two endpoints of a
    // join-arrow step. `prev` is the left side of the arrow; `curr`
    // is the right side. forward (`->`) means curr is the child of
    // prev; reverse (`<-`) means prev is the child of curr.
    LpNode *build_join_condition(LpNode *step,
                                  const std::string& prev_ref,
                                  const std::string& prev_table,
                                  const std::string& curr_ref,
                                  const std::string& curr_table) {
        const auto& s = step->u.sqldeep_join_step;

        // Inline USING (cols): each col → curr.col = prev.col.
        if (s.using_cols.count > 0) {
            std::vector<LpNode*> parts;
            for (int i = 0; i < s.using_cols.count; i++) {
                LpNode *c = s.using_cols.items[i];
                if (!c) continue;
                const char *col = (c->kind == LP_EXPR_COLUMN_REF)
                                    ? c->u.column_ref.column
                                    : nullptr;
                if (!col) continue;
                parts.push_back(make_binop(arena_, LP_OP_EQ,
                    make_column_ref2(arena_, curr_ref, col),
                    make_column_ref2(arena_, prev_ref, col)));
            }
            return and_chain(arena_, parts);
        }

        // Inline ON expression.
        if (s.on_expr) {
            return rewrite_on_expr(s.on_expr, s.forward,
                                    prev_ref, curr_ref);
        }

        // Convention / FK-guided.
        std::string child_table  = s.forward ? curr_table : prev_table;
        std::string parent_table = s.forward ? prev_table : curr_table;
        auto cols = resolve_columns(child_table, parent_table);
        std::vector<LpNode*> parts;
        for (const auto& cp : cols) {
            if (s.forward) {
                // curr.child = prev.parent
                parts.push_back(make_binop(arena_, LP_OP_EQ,
                    make_column_ref2(arena_, curr_ref, cp.child_col),
                    make_column_ref2(arena_, prev_ref, cp.parent_col)));
            } else {
                // prev.child = curr.parent
                parts.push_back(make_binop(arena_, LP_OP_EQ,
                    make_column_ref2(arena_, prev_ref, cp.child_col),
                    make_column_ref2(arena_, curr_ref, cp.parent_col)));
            }
        }
        return and_chain(arena_, parts);
    }

    // Re-qualify the bare column refs in an inline ON expression
    // with the appropriate aliases. Handles:
    //   - single column ref → "curr.col = prev.col"
    //   - binary EQ "L = R" → emit "curr.R = prev.L" (forward) or
    //     "prev.R = curr.L" (reverse). Left side names the parent
    //     column, right side names the child column.
    //   - AND of EQs → recurse left/right, combine with AND.
    LpNode *rewrite_on_expr(LpNode *expr, int forward,
                             const std::string& prev_ref,
                             const std::string& curr_ref) {
        if (!expr) return nullptr;

        if (expr->kind == LP_EXPR_COLUMN_REF) {
            // Shorthand: same column name in both tables.
            const char *col = expr->u.column_ref.column;
            if (!col) return expr;
            // For forward (curr is child): curr.col = prev.col
            // For reverse (prev is child): prev.col = curr.col
            if (forward)
                return make_binop(arena_, LP_OP_EQ,
                    make_column_ref2(arena_, curr_ref, col),
                    make_column_ref2(arena_, prev_ref, col));
            else
                return make_binop(arena_, LP_OP_EQ,
                    make_column_ref2(arena_, prev_ref, col),
                    make_column_ref2(arena_, curr_ref, col));
        }

        if (expr->kind == LP_EXPR_BINARY_OP
            && expr->u.binary.op == LP_OP_AND) {
            LpNode *l = rewrite_on_expr(expr->u.binary.left, forward,
                                         prev_ref, curr_ref);
            LpNode *r = rewrite_on_expr(expr->u.binary.right, forward,
                                         prev_ref, curr_ref);
            return make_binop(arena_, LP_OP_AND, l, r);
        }

        if (expr->kind == LP_EXPR_BINARY_OP
            && expr->u.binary.op == LP_OP_EQ) {
            // ON L = R: L names the column on the LEFT side of the
            // arrow; R names the column on the RIGHT side. The arrow
            // direction (forward / reverse) determines which side is
            // the child and which is the parent, but the L→left,
            // R→right mapping is invariant.
            //   Forward  (c -> orders): prev=c=left, curr=orders=right
            //                           emit curr.R = prev.L
            //   Reverse  (o <- vendors): prev=o=left, curr=vendors=right
            //                            emit prev.L = curr.R
            LpNode *L = expr->u.binary.left;
            LpNode *R = expr->u.binary.right;
            const char *Lcol = (L && L->kind == LP_EXPR_COLUMN_REF)
                                  ? L->u.column_ref.column : nullptr;
            const char *Rcol = (R && R->kind == LP_EXPR_COLUMN_REF)
                                  ? R->u.column_ref.column : nullptr;
            if (Lcol && Rcol) {
                if (forward)
                    return make_binop(arena_, LP_OP_EQ,
                        make_column_ref2(arena_, curr_ref, Rcol),
                        make_column_ref2(arena_, prev_ref, Lcol));
                else
                    return make_binop(arena_, LP_OP_EQ,
                        make_column_ref2(arena_, prev_ref, Lcol),
                        make_column_ref2(arena_, curr_ref, Rcol));
            }
        }

        return expr;  // fallback: leave as-is
    }

    // SELECT-level handling: join path expansion, singular → LIMIT 1,
    // deep projection wrapping. The from_first flag is also cleared
    // here because canonical unparser emits standard SELECT-FROM order.
    LpNode *rewrite_select(LpNode *node) {
        node->u.select.sqldeep_from_first = 0;

        // Expand a sqldeep join path in the FROM clause into a
        // standard FROM_TABLE / JOIN_CLAUSE chain plus an AND-ed
        // start-correlation predicate added to the WHERE clause.
        if (node->u.select.from
            && node->u.select.from->kind == LP_SQLDEEP_JOIN_PATH) {
            LpNode *jp = node->u.select.from;
            LpNode *extra_where = nullptr;
            LpNode *chain = rewrite_join_path(jp, &extra_where);
            node->u.select.from = chain;
            if (extra_where) {
                node->u.select.where = node->u.select.where
                    ? make_binop(arena_, LP_OP_AND,
                                  extra_where, node->u.select.where)
                    : extra_where;
            }
        }

        bool was_singular = node->u.select.sqldeep_singular;

        // SELECT/1 → LIMIT 1 (unless a LIMIT is already specified).
        if (was_singular) {
            node->u.select.sqldeep_singular = 0;
            if (!node->u.select.limit) {
                node->u.select.limit = make_limit(arena_, 1);
            }
        }

        // SELECT/1 [single_elem] FROM t → SELECT single_elem FROM t LIMIT 1.
        // The single-element array literal already became fn_array(elem) in
        // the post-order walk; unwrap it back to bare `elem` when singular.
        if (was_singular
            && node->u.select.result_columns.count == 1) {
            LpNode *rc = node->u.select.result_columns.items[0];
            LpNode *proj = (rc && rc->kind == LP_RESULT_COLUMN)
                              ? rc->u.result_column.expr : rc;
            if (proj && proj->kind == LP_EXPR_FUNCTION
                && proj->u.function.name
                && std::strcmp(proj->u.function.name, fn_array()) == 0
                && proj->u.function.args.count == 1) {
                LpNode *elem = proj->u.function.args.items[0];
                if (rc && rc->kind == LP_RESULT_COLUMN)
                    rc->u.result_column.expr = elem;
                else
                    node->u.select.result_columns.items[0] = elem;
            }
        }

        // Deep-projection wrapping. The rules (mirroring sqldeep):
        //
        //   Array projection [...] — ALWAYS wraps in fn_group_array,
        //   regardless of nesting. Single-element arrays unwrap to
        //   the bare element first; multi-element wrap the whole
        //   fn_array(...) call.
        //
        //   Object projection {...} — wraps only when the SELECT is a
        //   value subquery in an "aggregating" context: parent is
        //   LP_EXPR_SUBQUERY whose parent is LP_SQLDEEP_FIELD or
        //   LP_EXPR_SQLDEEP_ARRAY or LP_EXPR_IN.
        //
        //   Singular (/1) — never wrap, no group_array. The singular
        //   array-element unwrap already happened above.
        //
        // Scalar-subquery contexts (function args, WHERE comparisons,
        // arithmetic, EXISTS, etc.) do NOT wrap.

        if (node->u.select.limit) return node;  // singular / explicit LIMIT
        if (node->u.select.result_columns.count != 1) return node;

        LpNode *rc = node->u.select.result_columns.items[0];
        LpNode *proj = (rc && rc->kind == LP_RESULT_COLUMN)
                          ? rc->u.result_column.expr : rc;
        if (!proj || proj->kind != LP_EXPR_FUNCTION) return node;
        const char *pname = proj->u.function.name;
        if (!pname) return node;

        bool is_obj = std::strcmp(pname, fn_object()) == 0;
        bool is_arr = std::strcmp(pname, fn_array()) == 0;
        bool is_xml = std::strcmp(pname, "xml_element") == 0
                    || std::strcmp(pname, "xml_element_jsx") == 0
                    || std::strcmp(pname, "xml_element_jsonml") == 0;
        if (!is_obj && !is_arr && !is_xml) return node;

        // Helper: set the projection back.
        auto replace_projection = [&](LpNode *new_expr) {
            if (rc && rc->kind == LP_RESULT_COLUMN)
                rc->u.result_column.expr = new_expr;
            else
                node->u.select.result_columns.items[0] = new_expr;
        };

        // XML projection in a value-subquery whose grandparent is an
        // XML element (i.e. <ul>{SELECT <li/> FROM t}</ul>) gets
        // wrapped in the corresponding {xml,jsx,jsonml}_agg helper so
        // rows aggregate into one XML fragment.
        if (is_xml) {
            LpNode *parent = node->parent;
            if (!parent || parent->kind != LP_EXPR_SUBQUERY) return node;
            LpNode *gp = parent->parent;
            if (!gp || gp->kind != LP_EXPR_SQLDEEP_XML) return node;
            const char *agg = "xml_agg";
            if (std::strcmp(pname, "xml_element_jsx") == 0)    agg = "jsx_agg";
            if (std::strcmp(pname, "xml_element_jsonml") == 0) agg = "jsonml_agg";
            replace_projection(make_function(arena_, agg, {proj}));
            return node;
        }

        if (is_obj) {
            // Object wrap fires when the SELECT is a value-subquery in
            // an aggregating sqldeep context (object field value, array
            // element, IN-list value). LP_EXPR_IN holds its inner
            // select directly without an LP_EXPR_SUBQUERY wrapper, so
            // both paths are checked.
            LpNode *parent = node->parent;
            if (!parent) return node;
            bool wrap = false;
            if (parent->kind == LP_EXPR_IN) {
                wrap = true;
            } else if (parent->kind == LP_EXPR_SUBQUERY) {
                LpNode *gp = parent->parent;
                if (gp && (gp->kind == LP_SQLDEEP_FIELD
                        || gp->kind == LP_EXPR_SQLDEEP_ARRAY
                        || gp->kind == LP_EXPR_IN)) {
                    wrap = true;
                }
            }
            if (!wrap) return node;
            replace_projection(make_function(arena_, fn_group_array(), {proj}));
            return node;
        }

        // Array projection: unwrap single-element; wrap in group_array.
        LpNode *inner = (proj->u.function.args.count == 1)
                          ? proj->u.function.args.items[0] : proj;
        replace_projection(make_function(arena_, fn_group_array(), {inner}));
        return node;
    }
};

// ── End-to-end pipeline ──────────────────────────────────────────────

std::string transpile_impl(const std::string& input,
                            const std::vector<ForeignKey>* fks,
                            Backend backend) {
    arena_t *arena = arena_create(64 * 1024);
    if (!arena) throw Error("out of memory", 0, 0);

    // Try to parse as a sequence of statements. If that fails, the
    // input may be a bare expression — sqldeep historically accepted
    // those (e.g. `<div/>`, `{a, b}`) directly. Wrap it as a one-
    // column SELECT, transform, then strip the SELECT framing so the
    // caller still sees just the expression.
    const char *parse_err = nullptr;
    LpNodeList *stmts = lp_parse_all(input.c_str(), arena, &parse_err);
    bool bare_expr = false;
    if (!stmts) {
        std::string wrapped = "SELECT " + input;
        const char *err2 = nullptr;
        stmts = lp_parse_all(wrapped.c_str(), arena, &err2);
        if (!stmts) {
            std::string msg = parse_err ? parse_err
                              : (err2 ? err2 : "parse error");
            // Deepparser formats errors as "LINE:COL: message"; parse
            // out the position so callers can highlight the source.
            int line = 0, col = 0;
            (void)std::sscanf(msg.c_str(), "%d:%d", &line, &col);
            arena_destroy(arena);
            throw Error(msg, line, col);
        }
        bare_expr = true;
    }

    // Rename "?" placeholders to ordered sentinels so we can verify the
    // transform preserves their order before handing SQL back (restored
    // to "?" after the check). See mark_positional_params.
    int pos_param_count = mark_positional_params(stmts, arena);

    FkIndex fk_index;
    if (fks) fk_index = build_fk_index(*fks);

    try {
        Transformer t(arena, backend, fks ? &fk_index : nullptr);
        for (int i = 0; i < stmts->count; i++) {
            stmts->items[i] = t.transform(stmts->items[i]);
            if (stmts->items[i]) lp_fix_parents(stmts->items[i]);
        }
    } catch (...) {
        arena_destroy(arena);
        throw;
    }

    std::string out;
    if (bare_expr && stmts->count == 1) {
        // Extract just the single result-column expression so the
        // caller sees the bare transformed form.
        LpNode *sel = stmts->items[0];
        if (sel && sel->kind == LP_STMT_SELECT
            && sel->u.select.result_columns.count == 1) {
            LpNode *rc = sel->u.select.result_columns.items[0];
            LpNode *expr = (rc && rc->kind == LP_RESULT_COLUMN)
                              ? rc->u.result_column.expr : rc;
            if (expr) {
                char *sql = lp_ast_to_sql(expr, arena);
                if (sql) out = sql;
            }
        }
    } else {
        for (int i = 0; i < stmts->count; i++) {
            if (i > 0) out += "; ";
            char *sql = lp_ast_to_sql(stmts->items[i], arena);
            if (sql) out += sql;
        }
    }

    arena_destroy(arena);

    // Verify "?" order survived the rewrite and restore the placeholders
    // (throws if a rewrite reordered them). The arena is already gone;
    // this works purely on the output string.
    restore_positional_params(out, pos_param_count);
    return out;
}

} // namespace

// ── Public C++ API ──────────────────────────────────────────────────

std::string transpile(const std::string& input) {
    return transpile_impl(input, nullptr, Backend::sqlite);
}

std::string transpile(const std::string& input,
                       const std::vector<ForeignKey>& fks) {
    return transpile_impl(input, &fks, Backend::sqlite);
}

std::string transpile(const std::string& input, Backend backend) {
    return transpile_impl(input, nullptr, backend);
}

std::string transpile(const std::string& input,
                       const std::vector<ForeignKey>& fks,
                       Backend backend) {
    return transpile_impl(input, &fks, backend);
}

} // namespace sqldeep

// ── C API bridge ────────────────────────────────────────────────────

namespace {

char *sqldeep_dup_str(const std::string& s) {
    char *p = static_cast<char*>(std::malloc(s.size() + 1));
    if (p) std::memcpy(p, s.c_str(), s.size() + 1);
    return p;
}

void sqldeep_set_error(char** err_msg, int* err_line, int* err_col,
               const sqldeep::Error& e) {
    if (err_msg)  *err_msg = sqldeep_dup_str(e.what());
    if (err_line) *err_line = e.line();
    if (err_col)  *err_col = e.col();
}

void sqldeep_clear_error(char** err_msg, int* err_line, int* err_col) {
    if (err_msg)  *err_msg = nullptr;
    if (err_line) *err_line = 0;
    if (err_col)  *err_col = 0;
}

sqldeep::Backend to_backend(sqldeep_backend b) {
    switch (b) {
        case SQLDEEP_POSTGRES:       return sqldeep::Backend::postgres;
        case SQLDEEP_SQLITE_VANILLA: return sqldeep::Backend::sqlite_vanilla;
        default:                     return sqldeep::Backend::sqlite;
    }
}

std::vector<sqldeep::ForeignKey> to_cpp_fks(const sqldeep_foreign_key* fks,
                                              int fk_count) {
    std::vector<sqldeep::ForeignKey> cpp_fks;
    cpp_fks.reserve(fk_count);
    for (int i = 0; i < fk_count; ++i) {
        sqldeep::ForeignKey fk;
        fk.from_table = fks[i].from_table;
        fk.to_table   = fks[i].to_table;
        fk.columns.reserve(fks[i].column_count);
        for (int j = 0; j < fks[i].column_count; ++j) {
            fk.columns.push_back({
                fks[i].columns[j].from_column,
                fks[i].columns[j].to_column,
            });
        }
        cpp_fks.push_back(std::move(fk));
    }
    return cpp_fks;
}

} // namespace

extern "C" {

char* sqldeep_transpile(const char* input,
                        char** err_msg, int* err_line, int* err_col) {
    return sqldeep_transpile_backend(input, SQLDEEP_SQLITE,
                                      err_msg, err_line, err_col);
}

char* sqldeep_transpile_fk(const char* input,
                           const sqldeep_foreign_key* fks, int fk_count,
                           char** err_msg, int* err_line, int* err_col) {
    return sqldeep_transpile_fk_backend(input, SQLDEEP_SQLITE, fks, fk_count,
                                         err_msg, err_line, err_col);
}

char* sqldeep_transpile_backend(const char* input,
                                sqldeep_backend backend,
                                char** err_msg, int* err_line, int* err_col) {
    sqldeep_clear_error(err_msg, err_line, err_col);
    try {
        return sqldeep_dup_str(sqldeep::transpile(input, to_backend(backend)));
    } catch (const sqldeep::Error& e) {
        sqldeep_set_error(err_msg, err_line, err_col, e);
        return nullptr;
    }
}

char* sqldeep_transpile_fk_backend(const char* input,
                                   sqldeep_backend backend,
                                   const sqldeep_foreign_key* fks, int fk_count,
                                   char** err_msg, int* err_line, int* err_col) {
    sqldeep_clear_error(err_msg, err_line, err_col);
    try {
        auto cpp_fks = to_cpp_fks(fks, fk_count);
        return sqldeep_dup_str(sqldeep::transpile(input, cpp_fks, to_backend(backend)));
    } catch (const sqldeep::Error& e) {
        sqldeep_set_error(err_msg, err_line, err_col, e);
        return nullptr;
    }
}

const char* sqldeep_version(void) {
    return SQLDEEP_VERSION;
}

void sqldeep_free(void* ptr) {
    std::free(ptr);
}

} // extern "C"

// ── sqldeep_xml (XML runtime for SQLite) ───────────────────────
// Source: vendor/src/sqldeep_xml.c

extern "C" {
// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
//
// SQLite runtime implementations of xml_element, xml_attrs, and xml_agg.
//
// BLOB protocol: all XML output is returned as BLOB so xml_element can
// distinguish "already-XML" children (pass through) from plain TEXT
// (which must be escaped).  The caller uses CAST(... AS TEXT) to
// convert the final result back to a string.


#include <string.h>

static int is_xml_blob(sqlite3_value *v) {
    return sqlite3_value_type(v) == SQLITE_BLOB;
}

// ── Escaping helpers ────────────────────────────────────────────────

static int xml_escaped_len(const char *s) {
    int n = 0;
    for (; *s; ++s) {
        switch (*s) {
        case '<': n += 4; break;
        case '>': n += 4; break;
        case '&': n += 5; break;
        default:  n++; break;
        }
    }
    return n;
}

static void xml_escape_text_to(const char *s, char *out, int *pos) {
    for (; *s; ++s) {
        switch (*s) {
        case '<': memcpy(out + *pos, "&lt;", 4); *pos += 4; break;
        case '>': memcpy(out + *pos, "&gt;", 4); *pos += 4; break;
        case '&': memcpy(out + *pos, "&amp;", 5); *pos += 5; break;
        default:  out[*pos] = *s; (*pos)++; break;
        }
    }
}

// ── xml_attrs(name1, value1, name2, value2, ...) ────────────────────

static void sd_xml_attrs(sqlite3_context *ctx, int argc,
                          sqlite3_value **argv) {
    int i, len = 0;
    if (argc % 2 != 0) {
        sqlite3_result_error(ctx, "xml_attrs requires even number of args", -1);
        return;
    }
    for (i = 0; i < argc; i += 2) {
        const char *val;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        /* sqldeep_json('true') / sqldeep_json('false') = boolean BLOB */
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_BLOB) {
            int blen = sqlite3_value_bytes(argv[i + 1]);
            const char *b = (const char *)sqlite3_value_blob(argv[i + 1]);
            if (blen == 5 && memcmp(b, "false", 5) == 0) continue;
            /* true → bare attribute name */
            len += 1 + (int)strlen((const char *)sqlite3_value_text(argv[i]));
            continue;
        }
        len += 1; /* space */
        len += (int)strlen((const char *)sqlite3_value_text(argv[i]));
        val = (const char *)sqlite3_value_text(argv[i + 1]);
        len += 2 + xml_escaped_len(val); /* ="..." */
    }
    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    for (i = 0; i < argc; i += 2) {
        const char *name, *val;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        name = (const char *)sqlite3_value_text(argv[i]);
        /* sqldeep_json('true') / sqldeep_json('false') = boolean BLOB */
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_BLOB) {
            int blen = sqlite3_value_bytes(argv[i + 1]);
            const char *b = (const char *)sqlite3_value_blob(argv[i + 1]);
            if (blen == 5 && memcmp(b, "false", 5) == 0) continue;
            /* true (or any other JSON BLOB) → bare attribute name */
            out[pos++] = ' ';
            memcpy(out + pos, name, strlen(name));
            pos += (int)strlen(name);
            continue;
        }
        val = (const char *)sqlite3_value_text(argv[i + 1]);
        out[pos++] = ' ';
        memcpy(out + pos, name, strlen(name));
        pos += (int)strlen(name);
        out[pos++] = '=';
        out[pos++] = '"';
        for (const char *p = val; *p; ++p) {
            switch (*p) {
            case '"': memcpy(out + pos, "&quot;", 6); pos += 6; break;
            case '<': memcpy(out + pos, "&lt;", 4); pos += 4; break;
            case '>': memcpy(out + pos, "&gt;", 4); pos += 4; break;
            case '&': memcpy(out + pos, "&amp;", 5); pos += 5; break;
            default:  out[pos++] = *p; break;
            }
        }
        out[pos++] = '"';
    }
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

// ── xml_element(tag, [attrs], ...children) ──────────────────────────

static void sd_xml_element(sqlite3_context *ctx, int argc,
                            sqlite3_value **argv) {
    if (argc < 1) {
        sqlite3_result_error(ctx, "xml_element requires at least 1 arg", -1);
        return;
    }
    const char *tag = (const char *)sqlite3_value_text(argv[0]);
    int taglen = (int)strlen(tag);
    int self_closing = (taglen > 0 && tag[taglen - 1] == '/');
    if (self_closing) taglen--; /* strip trailing '/' from tag name */
    const char *attrs = "";
    int attrslen = 0;
    int child_start = 1;

    if (argc > 1 && is_xml_blob(argv[1])) {
        const char *a = (const char *)sqlite3_value_blob(argv[1]);
        int alen = sqlite3_value_bytes(argv[1]);
        if (alen > 0 && a[0] == ' ') {
            attrs = a;
            attrslen = alen;
            child_start = 2;
        }
    }

    int has_children = 0;
    int children_len = 0;
    for (int i = child_start; i < argc; ++i) {
        if (sqlite3_value_type(argv[i]) == SQLITE_NULL) continue;
        has_children = 1;
        if (is_xml_blob(argv[i])) {
            children_len += sqlite3_value_bytes(argv[i]);
        } else {
            const char *c = (const char *)sqlite3_value_text(argv[i]);
            children_len += xml_escaped_len(c);
        }
    }

    /* <tag attrs> children </tag> or <tag attrs/> + NUL */
    int outlen = 1 + taglen + attrslen +
                 (self_closing ? 2
                  : has_children ? 1 + children_len + 2 + taglen + 1
                  : 1 + 2 + taglen + 1) + 1;
    char *out = (char *)sqlite3_malloc(outlen);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    out[pos++] = '<';
    memcpy(out + pos, tag, taglen); pos += taglen;
    memcpy(out + pos, attrs, attrslen); pos += attrslen;

    if (self_closing) {
        out[pos++] = '/';
        out[pos++] = '>';
    } else if (has_children) {
        out[pos++] = '>';
        for (int i = child_start; i < argc; ++i) {
            if (sqlite3_value_type(argv[i]) == SQLITE_NULL) continue;
            if (is_xml_blob(argv[i])) {
                int blen = sqlite3_value_bytes(argv[i]);
                memcpy(out + pos, sqlite3_value_blob(argv[i]), blen);
                pos += blen;
            } else {
                const char *c = (const char *)sqlite3_value_text(argv[i]);
                xml_escape_text_to(c, out, &pos);
            }
        }
        out[pos++] = '<';
        out[pos++] = '/';
        memcpy(out + pos, tag, taglen); pos += taglen;
        out[pos++] = '>';
    } else {
        out[pos++] = '>';
        out[pos++] = '<';
        out[pos++] = '/';
        memcpy(out + pos, tag, taglen); pos += taglen;
        out[pos++] = '>';
    }
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

// ── xml_agg (aggregate) ─────────────────────────────────────────────

typedef struct XmlAggCtx {
    char *buf;
    int len;
    int cap;
} XmlAggCtx;

static void xml_agg_append(XmlAggCtx *p, const char *s, int n) {
    if (p->len + n >= p->cap) {
        int newcap = (p->cap + n) * 2 + 64;
        p->buf = (char *)sqlite3_realloc(p->buf, newcap);
        p->cap = newcap;
    }
    memcpy(p->buf + p->len, s, n);
    p->len += n;
}

static void sd_xml_agg_step(sqlite3_context *ctx, int argc,
                             sqlite3_value **argv) {
    (void)argc;
    if (sqlite3_value_type(argv[0]) == SQLITE_NULL) return;
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, sizeof(*p));
    if (!p) return;
    if (is_xml_blob(argv[0])) {
        int blen = sqlite3_value_bytes(argv[0]);
        xml_agg_append(p, (const char *)sqlite3_value_blob(argv[0]), blen);
    } else {
        const char *v = (const char *)sqlite3_value_text(argv[0]);
        for (const char *c = v; *c; ++c) {
            switch (*c) {
            case '<': xml_agg_append(p, "&lt;", 4); break;
            case '>': xml_agg_append(p, "&gt;", 4); break;
            case '&': xml_agg_append(p, "&amp;", 5); break;
            default: xml_agg_append(p, c, 1); break;
            }
        }
    }
}

static void sd_xml_agg_final(sqlite3_context *ctx) {
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, 0);
    if (!p || !p->buf || p->len == 0) {
        sqlite3_result_blob(ctx, "", 0, SQLITE_STATIC);
        return;
    }
    sqlite3_result_blob(ctx, p->buf, p->len, sqlite3_free);
}

// ── JSON string escaping helper ──────────────────────────────────────

static int json_escaped_len(const char *s, int n) {
    int len = 0;
    for (int i = 0; i < n; ++i) {
        unsigned char c = (unsigned char)s[i];
        switch (c) {
        case '"': case '\\': len += 2; break;
        case '\b': case '\f': case '\n': case '\r': case '\t': len += 2; break;
        default:
            if (c < 0x20) len += 6; /* \uXXXX */
            else len++;
            break;
        }
    }
    return len;
}

static void json_escape_to(const char *s, int n, char *out, int *pos) {
    static const char hex[] = "0123456789abcdef";
    for (int i = 0; i < n; ++i) {
        unsigned char c = (unsigned char)s[i];
        switch (c) {
        case '"':  out[(*pos)++] = '\\'; out[(*pos)++] = '"'; break;
        case '\\': out[(*pos)++] = '\\'; out[(*pos)++] = '\\'; break;
        case '\b': out[(*pos)++] = '\\'; out[(*pos)++] = 'b'; break;
        case '\f': out[(*pos)++] = '\\'; out[(*pos)++] = 'f'; break;
        case '\n': out[(*pos)++] = '\\'; out[(*pos)++] = 'n'; break;
        case '\r': out[(*pos)++] = '\\'; out[(*pos)++] = 'r'; break;
        case '\t': out[(*pos)++] = '\\'; out[(*pos)++] = 't'; break;
        default:
            if (c < 0x20) {
                out[(*pos)++] = '\\'; out[(*pos)++] = 'u';
                out[(*pos)++] = '0'; out[(*pos)++] = '0';
                out[(*pos)++] = hex[c >> 4]; out[(*pos)++] = hex[c & 0xf];
            } else {
                out[(*pos)++] = (char)c;
            }
            break;
        }
    }
}

// ── BLOB helpers for JSONML/JSX ────────────────────────────────────
//
// Like XML mode, JSONML/JSX functions return BLOB to distinguish
// structured output from plain text.  JSONML BLOBs start with '[',
// attrs BLOBs with '{'.  XML BLOBs start with '<'.  Custom JSON
// functions (sqldeep_json_object, sqldeep_json_array) inspect BLOBs:
// '<' → XML (quote as string); '['/'{' → JSON (inline raw).

// ── xml_attrs_jsonml(name1, value1, name2, value2, ...) ─────────────

static void sd_xml_attrs_jsonml(sqlite3_context *ctx, int argc,
                                 sqlite3_value **argv) {
    int i, len = 2; /* {} */
    int nattrs = 0;
    if (argc % 2 != 0) {
        sqlite3_result_error(ctx, "xml_attrs_jsonml requires even number of args", -1);
        return;
    }
    for (i = 0; i < argc; i += 2) {
        const char *name, *val;
        int namelen, vallen;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        nattrs++;
        name = (const char *)sqlite3_value_text(argv[i]);
        namelen = (int)strlen(name);
        val = (const char *)sqlite3_value_text(argv[i + 1]);
        vallen = (int)strlen(val);
        len += 2 + json_escaped_len(name, namelen); /* "name" */
        len += 1; /* : */
        len += 2 + json_escaped_len(val, vallen);   /* "val" */
    }
    if (nattrs > 1) len += nattrs - 1; /* commas */
    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    int written = 0;
    out[pos++] = '{';
    for (i = 0; i < argc; i += 2) {
        const char *name, *val;
        int namelen, vallen;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        if (written++ > 0) out[pos++] = ',';
        name = (const char *)sqlite3_value_text(argv[i]);
        namelen = (int)strlen(name);
        val = (const char *)sqlite3_value_text(argv[i + 1]);
        vallen = (int)strlen(val);
        out[pos++] = '"';
        json_escape_to(name, namelen, out, &pos);
        out[pos++] = '"';
        out[pos++] = ':';
        out[pos++] = '"';
        json_escape_to(val, vallen, out, &pos);
        out[pos++] = '"';
    }
    out[pos++] = '}';
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

// ── xml_element_jsonml(tag, [attrs], ...children) ───────────────────

static void sd_xml_element_jsonml(sqlite3_context *ctx, int argc,
                                   sqlite3_value **argv) {
    if (argc < 1) {
        sqlite3_result_error(ctx, "xml_element_jsonml requires at least 1 arg", -1);
        return;
    }
    const char *tag = (const char *)sqlite3_value_text(argv[0]);
    int taglen = (int)strlen(tag);
    if (taglen > 0 && tag[taglen - 1] == '/') taglen--; /* strip void marker */
    int child_start = 1;
    const char *attrs = NULL;
    int attrslen = 0;

    /* Detect attrs BLOB: starts with '{' */
    if (argc > 1 && is_xml_blob(argv[1])) {
        const char *a = (const char *)sqlite3_value_blob(argv[1]);
        int alen = sqlite3_value_bytes(argv[1]);
        if (alen > 0 && a[0] == '{') {
            attrs = a;
            attrslen = alen;
            child_start = 2;
        }
    }

    /* Calculate output length: ["tag",{attrs},children...] */
    int len = 1; /* [ */
    len += 2 + json_escaped_len(tag, taglen); /* "tag" */
    if (attrs) {
        len += 1 + attrslen; /* ,{attrs} */
    }
    for (int i = child_start; i < argc; ++i) {
        if (sqlite3_value_type(argv[i]) == SQLITE_NULL) continue;
        if (is_xml_blob(argv[i])) {
            int blen = sqlite3_value_bytes(argv[i]);
            if (blen == 0) continue; /* empty agg result */
            len += 1 + blen; /* comma + raw JSONML */
        } else {
            /* Text — JSON string */
            const char *c = (const char *)sqlite3_value_text(argv[i]);
            int clen = (int)strlen(c);
            len += 1 + 2 + json_escaped_len(c, clen); /* comma + "..." */
        }
    }
    len += 1; /* ] */

    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    out[pos++] = '[';
    out[pos++] = '"';
    json_escape_to(tag, taglen, out, &pos);
    out[pos++] = '"';
    if (attrs) {
        out[pos++] = ',';
        memcpy(out + pos, attrs, attrslen);
        pos += attrslen;
    }
    for (int i = child_start; i < argc; ++i) {
        if (sqlite3_value_type(argv[i]) == SQLITE_NULL) continue;
        if (is_xml_blob(argv[i])) {
            int blen = sqlite3_value_bytes(argv[i]);
            if (blen == 0) continue;
            out[pos++] = ',';
            memcpy(out + pos, sqlite3_value_blob(argv[i]), blen);
            pos += blen;
        } else {
            const char *c = (const char *)sqlite3_value_text(argv[i]);
            int clen = (int)strlen(c);
            out[pos++] = ',';
            out[pos++] = '"';
            json_escape_to(c, clen, out, &pos);
            out[pos++] = '"';
        }
    }
    out[pos++] = ']';
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

// ── jsonml_agg (aggregate) ──────────────────────────────────────────
// Collects JSONML fragments as comma-separated bytes in a BLOB.

static void sd_jsonml_agg_step(sqlite3_context *ctx, int argc,
                                sqlite3_value **argv) {
    (void)argc;
    if (sqlite3_value_type(argv[0]) == SQLITE_NULL) return;
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, sizeof(*p));
    if (!p) return;
    if (p->len > 0) xml_agg_append(p, ",", 1);
    if (is_xml_blob(argv[0])) {
        int blen = sqlite3_value_bytes(argv[0]);
        xml_agg_append(p, (const char *)sqlite3_value_blob(argv[0]), blen);
    } else {
        /* Text child — emit as JSON string */
        const char *v = (const char *)sqlite3_value_text(argv[0]);
        int vlen = (int)strlen(v);
        int elen = 2 + json_escaped_len(v, vlen);
        /* Ensure capacity and write directly */
        if (p->len + elen >= p->cap) {
            int newcap = (p->cap + elen) * 2 + 64;
            p->buf = (char *)sqlite3_realloc(p->buf, newcap);
            p->cap = newcap;
        }
        p->buf[p->len++] = '"';
        json_escape_to(v, vlen, p->buf, &p->len);
        p->buf[p->len++] = '"';
    }
}

static void sd_jsonml_agg_final(sqlite3_context *ctx) {
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, 0);
    if (!p || !p->buf || p->len == 0) {
        sqlite3_result_blob(ctx, "", 0, SQLITE_STATIC);
        return;
    }
    sqlite3_result_blob(ctx, p->buf, p->len, sqlite3_free);
}

// ── xml_attrs_jsx(name1, value1, name2, value2, ...) ──────────────
// Like xml_attrs_jsonml, but values that are JSON BLOBs, INTEGER, or
// FLOAT are emitted as raw JSON values instead of quoted strings.
// This lets sqldeep_json_object(...), numbers, and booleans flow
// through as live values in the JSX attributes object.

/* Return true if the value should be emitted as raw JSON (unquoted).
   BLOBs that don't start with '<' are JSON (inline raw).
   BLOBs starting with '<' are XML (will be quoted as strings). */
static int jsx_is_raw(sqlite3_value *v) {
    int t = sqlite3_value_type(v);
    if (t == SQLITE_INTEGER || t == SQLITE_FLOAT) return 1;
    if (t == SQLITE_BLOB) {
        int blen = sqlite3_value_bytes(v);
        if (blen > 0) {
            const char *b = (const char *)sqlite3_value_blob(v);
            return b[0] != '<'; /* not XML → JSON */
        }
    }
    return 0;
}

static void sd_xml_attrs_jsx(sqlite3_context *ctx, int argc,
                              sqlite3_value **argv) {
    int i, len = 2; /* {} */
    int nattrs = 0;
    if (argc % 2 != 0) {
        sqlite3_result_error(ctx, "xml_attrs_jsx requires even number of args", -1);
        return;
    }
    for (i = 0; i < argc; i += 2) {
        const char *name;
        int namelen, vallen;
        int raw;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        nattrs++;
        /* Check raw BEFORE sqlite3_value_text (which coerces BLOB→TEXT) */
        raw = jsx_is_raw(argv[i + 1]);
        name = (const char *)sqlite3_value_text(argv[i]);
        namelen = (int)strlen(name);
        if (raw && sqlite3_value_type(argv[i + 1]) == SQLITE_BLOB) {
            vallen = sqlite3_value_bytes(argv[i + 1]);
        } else {
            vallen = (int)strlen((const char *)sqlite3_value_text(argv[i + 1]));
        }
        len += 2 + json_escaped_len(name, namelen); /* "name" */
        len += 1; /* : */
        if (raw) {
            len += vallen; /* raw JSON/number */
        } else {
            const char *val = (const char *)sqlite3_value_text(argv[i + 1]);
            len += 2 + json_escaped_len(val, vallen); /* "val" */
        }
    }
    if (nattrs > 1) len += nattrs - 1; /* commas */
    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    int written = 0;
    out[pos++] = '{';
    for (i = 0; i < argc; i += 2) {
        const char *name;
        int namelen, vallen;
        int raw;
        if (sqlite3_value_type(argv[i + 1]) == SQLITE_NULL) continue;
        if (written++ > 0) out[pos++] = ',';
        /* Check raw BEFORE sqlite3_value_text (which coerces BLOB→TEXT) */
        raw = jsx_is_raw(argv[i + 1]);
        name = (const char *)sqlite3_value_text(argv[i]);
        namelen = (int)strlen(name);
        out[pos++] = '"';
        json_escape_to(name, namelen, out, &pos);
        out[pos++] = '"';
        out[pos++] = ':';
        if (raw) {
            /* JSON/numeric value — emit raw */
            const char *val;
            if (sqlite3_value_type(argv[i + 1]) == SQLITE_BLOB) {
                val = (const char *)sqlite3_value_blob(argv[i + 1]);
                vallen = sqlite3_value_bytes(argv[i + 1]);
            } else {
                val = (const char *)sqlite3_value_text(argv[i + 1]);
                vallen = (int)strlen(val);
            }
            memcpy(out + pos, val, vallen);
            pos += vallen;
        } else {
            /* Plain text value — emit as JSON string */
            const char *val = (const char *)sqlite3_value_text(argv[i + 1]);
            vallen = (int)strlen(val);
            out[pos++] = '"';
            json_escape_to(val, vallen, out, &pos);
            out[pos++] = '"';
        }
    }
    out[pos++] = '}';
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

// ── sqldeep_json_object / sqldeep_json_array / sqldeep_json ─────────
//
// Drop-in replacements for json_object/json_array/json that handle BLOB
// values from the sqldeep XML/JSONML/JSX ecosystem.  All return BLOBs
// so structured values survive through views, CTEs, and subqueries.
//
// BLOB discrimination (no SQLite subtypes involved):
//   BLOB starting with '<'  → XML markup, quote as JSON string
//   BLOB not starting with '<' → JSON, inline raw
//   TEXT                     → quote as JSON string
//   INTEGER / FLOAT          → emit as JSON number
//   NULL                     → emit JSON null

/* Append a sqldeep value to a JSON output buffer. */
static void json_append_value(sqlite3_value *v, char *out, int *pos) {
    int t = sqlite3_value_type(v);
    if (t == SQLITE_NULL) {
        memcpy(out + *pos, "null", 4); *pos += 4;
    } else if (t == SQLITE_INTEGER || t == SQLITE_FLOAT) {
        const char *s = (const char *)sqlite3_value_text(v);
        int slen = (int)strlen(s);
        memcpy(out + *pos, s, slen); *pos += slen;
    } else if (t == SQLITE_BLOB) {
        int blen = sqlite3_value_bytes(v);
        const char *b = (const char *)sqlite3_value_blob(v);
        if (blen > 0 && b[0] == '<') {
            /* XML BLOB — quote as JSON string */
            out[(*pos)++] = '"';
            json_escape_to(b, blen, out, pos);
            out[(*pos)++] = '"';
        } else {
            /* JSON BLOB — inline raw */
            memcpy(out + *pos, b, blen); *pos += blen;
        }
    } else {
        /* TEXT — quote as JSON string */
        const char *s = (const char *)sqlite3_value_text(v);
        int slen = (int)strlen(s);
        out[(*pos)++] = '"';
        json_escape_to(s, slen, out, pos);
        out[(*pos)++] = '"';
    }
}

/* Calculate the JSON output length for a value. */
static int json_value_len(sqlite3_value *v) {
    int t = sqlite3_value_type(v);
    if (t == SQLITE_NULL) return 4; /* null */
    if (t == SQLITE_INTEGER || t == SQLITE_FLOAT)
        return (int)strlen((const char *)sqlite3_value_text(v));
    if (t == SQLITE_BLOB) {
        int blen = sqlite3_value_bytes(v);
        const char *b = (const char *)sqlite3_value_blob(v);
        if (blen > 0 && b[0] == '<')
            return 2 + json_escaped_len(b, blen); /* "..." */
        return blen; /* raw JSON */
    }
    /* TEXT */
    {
        const char *s = (const char *)sqlite3_value_text(v);
        int slen = (int)strlen(s);
        return 2 + json_escaped_len(s, slen); /* "..." */
    }
}

/* sqldeep_json_object(key1, val1, key2, val2, ...) */
static void sd_json_object(sqlite3_context *ctx, int argc,
                             sqlite3_value **argv) {
    if (argc % 2 != 0) {
        sqlite3_result_error(ctx, "sqldeep_json_object requires even number of args", -1);
        return;
    }
    int len = 2; /* {} */
    int nfields = 0;
    for (int i = 0; i < argc; i += 2) {
        const char *key = (const char *)sqlite3_value_text(argv[i]);
        int klen = (int)strlen(key);
        nfields++;
        len += 2 + json_escaped_len(key, klen); /* "key" */
        len += 1; /* : */
        len += json_value_len(argv[i + 1]);
    }
    if (nfields > 1) len += nfields - 1; /* commas */

    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    out[pos++] = '{';
    for (int i = 0; i < argc; i += 2) {
        if (i > 0) out[pos++] = ',';
        const char *key = (const char *)sqlite3_value_text(argv[i]);
        int klen = (int)strlen(key);
        out[pos++] = '"';
        json_escape_to(key, klen, out, &pos);
        out[pos++] = '"';
        out[pos++] = ':';
        json_append_value(argv[i + 1], out, &pos);
    }
    out[pos++] = '}';
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

/* sqldeep_json_array(val1, val2, ...) */
static void sd_json_array(sqlite3_context *ctx, int argc,
                            sqlite3_value **argv) {
    int len = 2; /* [] */
    for (int i = 0; i < argc; ++i)
        len += json_value_len(argv[i]);
    if (argc > 1) len += argc - 1; /* commas */

    char *out = (char *)sqlite3_malloc(len + 1);
    if (!out) { sqlite3_result_error_nomem(ctx); return; }
    int pos = 0;
    out[pos++] = '[';
    for (int i = 0; i < argc; ++i) {
        if (i > 0) out[pos++] = ',';
        json_append_value(argv[i], out, &pos);
    }
    out[pos++] = ']';
    out[pos] = '\0';
    sqlite3_result_blob(ctx, out, pos, sqlite3_free);
}

/* sqldeep_json_group_array(val) — aggregate */
/* Collects values into a JSON array, handling BLOBs like sd_json_array. */

static void sd_json_group_array_step(sqlite3_context *ctx, int argc,
                                      sqlite3_value **argv) {
    (void)argc;
    if (sqlite3_value_type(argv[0]) == SQLITE_NULL) return;
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, sizeof(*p));
    if (!p) return;
    if (p->len > 0) xml_agg_append(p, ",", 1);

    /* Compute the JSON representation and append it. */
    int vlen = json_value_len(argv[0]);
    /* Ensure capacity */
    if (p->len + vlen >= p->cap) {
        int newcap = (p->cap + vlen) * 2 + 64;
        p->buf = (char *)sqlite3_realloc(p->buf, newcap);
        p->cap = newcap;
    }
    json_append_value(argv[0], p->buf, &p->len);
}

static void sd_json_group_array_final(sqlite3_context *ctx) {
    XmlAggCtx *p = (XmlAggCtx *)sqlite3_aggregate_context(ctx, 0);
    if (!p || !p->buf || p->len == 0) {
        sqlite3_result_blob(ctx, "[]", 2, SQLITE_STATIC);
        return;
    }
    /* Wrap in [ ... ] */
    int total = 1 + p->len + 1;
    char *out = (char *)sqlite3_malloc(total + 1);
    if (!out) {
        sqlite3_free(p->buf);
        sqlite3_result_error_nomem(ctx);
        return;
    }
    out[0] = '[';
    memcpy(out + 1, p->buf, p->len);
    out[1 + p->len] = ']';
    out[total] = '\0';
    sqlite3_free(p->buf);
    p->buf = NULL;
    p->len = 0;
    sqlite3_result_blob(ctx, out, total, sqlite3_free);
}

/* sqldeep_json(text) — returns text content as a BLOB so it is
   recognised as structured JSON by other sqldeep functions. */
static void sd_json(sqlite3_context *ctx, int argc, sqlite3_value **argv) {
    (void)argc;
    if (sqlite3_value_type(argv[0]) == SQLITE_NULL) {
        sqlite3_result_null(ctx);
        return;
    }
    const char *s = (const char *)sqlite3_value_text(argv[0]);
    int slen = (int)strlen(s);
    sqlite3_result_blob(ctx, s, slen, SQLITE_TRANSIENT);
}

// ── Public registration ───────────────────────────────────────────���─

int sqldeep_register_sqlite(sqlite3 *db) {
    int rc;
    rc = sqlite3_create_function(db, "xml_element", -1, SQLITE_UTF8,
                                 0, sd_xml_element, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "xml_attrs", -1, SQLITE_UTF8,
                                 0, sd_xml_attrs, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "xml_agg", 1, SQLITE_UTF8,
                                 0, 0, sd_xml_agg_step, sd_xml_agg_final);
    if (rc != SQLITE_OK) return rc;
    /* All functions use pure BLOB protocol — no SQLITE_SUBTYPE needed. */
    rc = sqlite3_create_function(db, "xml_element_jsonml", -1, SQLITE_UTF8,
                                 0, sd_xml_element_jsonml, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "xml_attrs_jsonml", -1, SQLITE_UTF8,
                                 0, sd_xml_attrs_jsonml, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "jsonml_agg", 1, SQLITE_UTF8,
                                 0, 0, sd_jsonml_agg_step, sd_jsonml_agg_final);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "xml_element_jsx", -1, SQLITE_UTF8,
                                 0, sd_xml_element_jsonml, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "xml_attrs_jsx", -1, SQLITE_UTF8,
                                 0, sd_xml_attrs_jsx, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "jsx_agg", 1, SQLITE_UTF8,
                                 0, 0, sd_jsonml_agg_step, sd_jsonml_agg_final);
    if (rc != SQLITE_OK) return rc;
    /* Custom JSON functions — return BLOBs, handle BLOB values from
       XML/JSONML/JSX.  No SQLITE_SUBTYPE needed (pure BLOB protocol). */
    rc = sqlite3_create_function(db, "sqldeep_json", 1, SQLITE_UTF8,
                                 0, sd_json, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "sqldeep_json_object", -1, SQLITE_UTF8,
                                 0, sd_json_object, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "sqldeep_json_array", -1, SQLITE_UTF8,
                                 0, sd_json_array, 0, 0);
    if (rc != SQLITE_OK) return rc;
    rc = sqlite3_create_function(db, "sqldeep_json_group_array", 1,
                                 SQLITE_UTF8,
                                 0, 0, sd_json_group_array_step,
                                 sd_json_group_array_final);
    return rc;
}
} // extern "C"
