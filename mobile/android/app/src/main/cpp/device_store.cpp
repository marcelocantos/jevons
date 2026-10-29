// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
#include <jni.h>
#include <mutex>
#include <string>
#include <tuple>
#include <sqlpipe.h>

namespace {
std::mutex store_mutex;
constexpr auto schema = "CREATE TABLE kv (id INTEGER PRIMARY KEY, key TEXT, value TEXT)";

std::string utf(JNIEnv* env, jstring text) {
    const char* bytes = env->GetStringUTFChars(text, nullptr);
    std::string result(bytes);
    env->ReleaseStringUTFChars(text, bytes);
    return result;
}

// SQL text literals are hex-encoded so a payload can never be interpreted as SQL.
std::string literal(const std::string& text) {
    static constexpr char digits[] = "0123456789ABCDEF";
    std::string hex;
    hex.reserve(text.size() * 2);
    for (unsigned char ch : text) { hex += digits[ch >> 4]; hex += digits[ch & 15]; }
    return "CAST(X'" + hex + "' AS TEXT)";
}

void throw_java(JNIEnv* env, const std::exception& error) {
    auto type = env->FindClass("java/lang/IllegalStateException");
    env->ThrowNew(type, error.what());
}

} // namespace

extern "C" JNIEXPORT jstring JNICALL
Java_com_canticode_jevons_1mobile_widget_DeviceStore_getNative(JNIEnv* env, jobject, jstring path, jstring key) {
    try {
        std::lock_guard lock(store_mutex);
        sqlpipe::Database db(utf(env, path), schema);
        auto rows = db.query("SELECT value FROM kv WHERE key=" + literal(utf(env, key)));
        if (rows.rows.empty()) return nullptr;
        return env->NewStringUTF(std::get<std::string>(rows.rows[0][0]).c_str());
    } catch (const std::exception& e) { throw_java(env, e); return nullptr; }
}

extern "C" JNIEXPORT void JNICALL
Java_com_canticode_jevons_1mobile_widget_DeviceStore_putNative(JNIEnv* env, jobject, jstring path, jstring key, jstring value) {
    try {
        std::lock_guard lock(store_mutex);
        sqlpipe::Database db(utf(env, path), schema);
        db.exec(std::string("INSERT INTO kv (id,key,value) VALUES (") +
            "1," + literal(utf(env, key)) + "," + literal(utf(env, value)) + ") "
            "ON CONFLICT(id) DO UPDATE SET key=excluded.key,value=excluded.value");
    } catch (const std::exception& e) { throw_java(env, e); }
}

extern "C" JNIEXPORT void JNICALL
Java_com_canticode_jevons_1mobile_widget_DeviceStore_putReadingNative(JNIEnv* env, jobject, jstring path, jstring payload, jlong timestamp) {
    try {
        std::lock_guard lock(store_mutex);
        sqlpipe::Database db(utf(env, path), schema);
        db.exec("BEGIN IMMEDIATE");
        try {
            for (const auto& [id, key, value] : {
                std::tuple<int, std::string, std::string>{2, "widget_payload", utf(env, payload)},
                std::tuple<int, std::string, std::string>{3, "widget_saved_at_ms", std::to_string(timestamp)}
            }) {
                db.exec(std::string("INSERT INTO kv (id,key,value) VALUES (") + std::to_string(id) + "," + literal(key) + "," + literal(value) + ") "
                    "ON CONFLICT(id) DO UPDATE SET value=excluded.value");
            }
            db.exec("COMMIT");
        } catch (...) { db.exec("ROLLBACK"); throw; }
    } catch (const std::exception& e) { throw_java(env, e); }
}
