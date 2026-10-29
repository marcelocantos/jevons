cmake_minimum_required(VERSION 3.22)
project(jevons_store LANGUAGES C CXX)
set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
add_library(jevons_store SHARED device_store.cpp sqlpipe.cpp sqlite3.c lz4.c
    deepparser/arena.c deepparser/liteparser.c deepparser/lp_tokenize.c
    deepparser/lp_unparse.c deepparser/parse.c)
target_include_directories(jevons_store PRIVATE . include deepparser)
target_compile_definitions(jevons_store PRIVATE SQLITE_ENABLE_SESSION SQLITE_ENABLE_PREUPDATE_HOOK SQLITE_ENABLE_DESERIALIZE)
target_compile_options(jevons_store PRIVATE -w)
