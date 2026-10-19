// Lean compiler output
// Module: JevonsInvariant.FailClosed
// Imports: public import Init public meta import Init
#include <lean/lean.h>
#if defined(__clang__)
#pragma clang diagnostic ignored "-Wunused-parameter"
#pragma clang diagnostic ignored "-Wunused-label"
#elif defined(__GNUC__) && !defined(__CLANG__)
#pragma GCC diagnostic ignored "-Wunused-parameter"
#pragma GCC diagnostic ignored "-Wunused-label"
#pragma GCC diagnostic ignored "-Wunused-but-set-variable"
#endif
#ifdef __cplusplus
extern "C" {
#endif
lean_object* lean_nat_to_int(lean_object*);
lean_object* l_Bool_repr___redArg(uint8_t);
lean_object* lean_string_length(lean_object*);
uint8_t lean_nat_dec_eq(lean_object*, lean_object*);
lean_object* l_Repr_addAppParen(lean_object*, lean_object*);
uint8_t lean_nat_dec_le(lean_object*, lean_object*);
lean_object* l_String_quote(lean_object*);
uint8_t lean_string_dec_eq(lean_object*, lean_object*);
uint8_t lean_nat_dec_le(lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq___boxed(lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest___boxed(lean_object*, lean_object*);
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 3, .m_capacity = 3, .m_length = 2, .m_data = "{ "};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__0_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__1_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 9, .m_capacity = 9, .m_length = 8, .m_data = "schemaOk"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__1 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__1_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__2_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__1_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__2 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__2_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__3_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)(((size_t)(0) << 1) | 1)),((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__2_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__3 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__3_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__4_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 5, .m_capacity = 5, .m_length = 4, .m_data = " := "};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__4 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__4_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__5_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__4_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__5 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__5_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__6_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__3_value),((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__5_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__6 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__6_value;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__8_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 2, .m_capacity = 2, .m_length = 1, .m_data = ","};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__8 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__8_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__9_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__8_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__9 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__9_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__10_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 12, .m_capacity = 12, .m_length = 11, .m_data = "hasNetGrant"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__10 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__10_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__11_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__10_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__11 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__11_value;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__13_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 3, .m_capacity = 3, .m_length = 2, .m_data = " }"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__13 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__13_value;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__16_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__0_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__16 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__16_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__17_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__13_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__17 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__17_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___boxed(lean_object*, lean_object*);
static const lean_closure_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_closure_object) + sizeof(void*)*0, .m_other = 0, .m_tag = 245}, .m_fun = (void*)lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___boxed, .m_arity = 2, .m_num_fixed = 0, .m_objs = {} };
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest___closed__0_value;
LEAN_EXPORT const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest___closed__0_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorIdx(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorIdx___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim(lean_object*, lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___boxed(lean_object*, lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ok_elim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ok_elim(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_err_elim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_err_elim(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq___boxed(lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult___boxed(lean_object*, lean_object*);
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 37, .m_capacity = 37, .m_length = 36, .m_data = "Jevons.Writ.FailClosed.LoadResult.ok"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__0_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__1_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__0_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__1 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__1_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__2_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__1_value),((lean_object*)(((size_t)(1) << 1) | 1))}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__2 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__2_value;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3;
static lean_once_cell_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once = LEAN_ONCE_CELL_INITIALIZER;
static lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__5_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 38, .m_capacity = 38, .m_length = 37, .m_data = "Jevons.Writ.FailClosed.LoadResult.err"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__5 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__5_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__6_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__5_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__6 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__6_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__7_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__6_value),((lean_object*)(((size_t)(1) << 1) | 1))}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__7 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__7_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___boxed(lean_object*, lean_object*);
static const lean_closure_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_closure_object) + sizeof(void*)*0, .m_other = 0, .m_tag = 245}, .m_fun = (void*)lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___boxed, .m_arity = 2, .m_num_fixed = 0, .m_objs = {} };
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult___closed__0_value;
LEAN_EXPORT const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult___closed__0_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(uint8_t);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim(lean_object*, lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___boxed(lean_object*, lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___redArg___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim(lean_object*, uint8_t, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___boxed(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ofNat(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ofNat___boxed(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqMalform(uint8_t, uint8_t);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqMalform___boxed(lean_object*, lean_object*);
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 44, .m_capacity = 44, .m_length = 43, .m_data = "Jevons.Writ.FailClosed.Malform.unknownField"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__0_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__1_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__0_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__1 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__1_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__2_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 41, .m_capacity = 41, .m_length = 40, .m_data = "Jevons.Writ.FailClosed.Malform.badSchema"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__2 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__2_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__3_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__2_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__3 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__3_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__4_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 41, .m_capacity = 41, .m_length = 40, .m_data = "Jevons.Writ.FailClosed.Malform.emptyGlob"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__4 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__4_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__5_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__4_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__5 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__5_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__6_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 46, .m_capacity = 46, .m_length = 45, .m_data = "Jevons.Writ.FailClosed.Malform.negativeBudget"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__6 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__6_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__7_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__6_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__7 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__7_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__8_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 45, .m_capacity = 45, .m_length = 44, .m_data = "Jevons.Writ.FailClosed.Malform.emptyNetGrant"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__8 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__8_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__9_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__8_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__9 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__9_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__10_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 41, .m_capacity = 41, .m_length = 40, .m_data = "Jevons.Writ.FailClosed.Malform.multiJSON"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__10 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__10_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__11_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__10_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__11 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__11_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr(uint8_t, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___boxed(lean_object*, lean_object*);
static const lean_closure_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_closure_object) + sizeof(void*)*0, .m_other = 0, .m_tag = 245}, .m_fun = (void*)lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___boxed, .m_arity = 2, .m_num_fixed = 0, .m_objs = {} };
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform___closed__0_value;
LEAN_EXPORT const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform___closed__0_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorIdx(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorIdx___boxed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim(lean_object*, lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___boxed(lean_object*, lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_valid_elim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_valid_elim(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_malformed_elim___redArg(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_malformed_elim(lean_object*, lean_object*, lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq___boxed(lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource___boxed(lean_object*, lean_object*);
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 36, .m_capacity = 36, .m_length = 35, .m_data = "Jevons.Writ.FailClosed.Source.valid"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__0_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__1_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__0_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__1 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__1_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__2_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__1_value),((lean_object*)(((size_t)(1) << 1) | 1))}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__2 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__2_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__3_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 40, .m_capacity = 40, .m_length = 39, .m_data = "Jevons.Writ.FailClosed.Source.malformed"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__3 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__3_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__4_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 3}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__3_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__4 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__4_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__5_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*2 + 0, .m_other = 2, .m_tag = 5}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__4_value),((lean_object*)(((size_t)(1) << 1) | 1))}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__5 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__5_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___boxed(lean_object*, lean_object*);
static const lean_closure_object lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_closure_object) + sizeof(void*)*0, .m_other = 0, .m_tag = 245}, .m_fun = (void*)lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___boxed, .m_arity = 2, .m_num_fixed = 0, .m_objs = {} };
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource___closed__0_value;
LEAN_EXPORT const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource___closed__0_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__0_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 7, .m_capacity = 7, .m_length = 6, .m_data = "schema"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__0 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__0_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__1_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 1}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__0_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__1 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__1_value;
static const lean_string_object lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__2_value = {.m_header = {.m_rc = 0, .m_cs_sz = 0, .m_other = 0, .m_tag = 249}, .m_size = 10, .m_capacity = 10, .m_length = 9, .m_data = "malformed"};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__2 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__2_value;
static const lean_ctor_object lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__3_value = {.m_header = {.m_rc = 0, .m_cs_sz = sizeof(lean_ctor_object) + sizeof(void*)*1 + 0, .m_other = 1, .m_tag = 1}, .m_objs = {((lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__2_value)}};
static const lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__3 = (const lean_object*)&lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__3_value;
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_isMalformed(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_isMalformed___boxed(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_mayExecute(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_mayExecute___boxed(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg___boxed(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___boxed(lean_object*, lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg(lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg___boxed(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___boxed(lean_object*, lean_object*);
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_asOption(lean_object*);
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(lean_object* v_x_1_, lean_object* v_x_2_){
_start:
{
uint8_t v_schemaOk_3_; uint8_t v_hasNetGrant_4_; uint8_t v_schemaOk_5_; uint8_t v_hasNetGrant_6_; uint8_t v___y_8_; 
v_schemaOk_3_ = lean_ctor_get_uint8(v_x_1_, 0);
v_hasNetGrant_4_ = lean_ctor_get_uint8(v_x_1_, 1);
v_schemaOk_5_ = lean_ctor_get_uint8(v_x_2_, 0);
v_hasNetGrant_6_ = lean_ctor_get_uint8(v_x_2_, 1);
if (v_schemaOk_5_ == 0)
{
if (v_schemaOk_3_ == 0)
{
uint8_t v___x_9_; 
v___x_9_ = 1;
v___y_8_ = v___x_9_;
goto v___jp_7_;
}
else
{
return v_schemaOk_5_;
}
}
else
{
if (v_schemaOk_3_ == 0)
{
return v_schemaOk_3_;
}
else
{
v___y_8_ = v_schemaOk_3_;
goto v___jp_7_;
}
}
v___jp_7_:
{
if (v_hasNetGrant_6_ == 0)
{
if (v_hasNetGrant_4_ == 0)
{
return v___y_8_;
}
else
{
return v_hasNetGrant_6_;
}
}
else
{
return v_hasNetGrant_4_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq___boxed(lean_object* v_x_10_, lean_object* v_x_11_){
_start:
{
uint8_t v_res_12_; lean_object* v_r_13_; 
v_res_12_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(v_x_10_, v_x_11_);
lean_dec_ref(v_x_11_);
lean_dec_ref(v_x_10_);
v_r_13_ = lean_box(v_res_12_);
return v_r_13_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest(lean_object* v_x_14_, lean_object* v_x_15_){
_start:
{
uint8_t v___x_16_; 
v___x_16_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(v_x_14_, v_x_15_);
return v___x_16_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest___boxed(lean_object* v_x_17_, lean_object* v_x_18_){
_start:
{
uint8_t v_res_19_; lean_object* v_r_20_; 
v_res_19_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest(v_x_17_, v_x_18_);
lean_dec_ref(v_x_18_);
lean_dec_ref(v_x_17_);
v_r_20_ = lean_box(v_res_19_);
return v_r_20_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7(void){
_start:
{
lean_object* v___x_34_; lean_object* v___x_35_; 
v___x_34_ = lean_unsigned_to_nat(12u);
v___x_35_ = lean_nat_to_int(v___x_34_);
return v___x_35_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12(void){
_start:
{
lean_object* v___x_42_; lean_object* v___x_43_; 
v___x_42_ = lean_unsigned_to_nat(15u);
v___x_43_ = lean_nat_to_int(v___x_42_);
return v___x_43_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14(void){
_start:
{
lean_object* v___x_45_; lean_object* v___x_46_; 
v___x_45_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__0));
v___x_46_ = lean_string_length(v___x_45_);
return v___x_46_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15(void){
_start:
{
lean_object* v___x_47_; lean_object* v___x_48_; 
v___x_47_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__14);
v___x_48_ = lean_nat_to_int(v___x_47_);
return v___x_48_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(lean_object* v_x_53_){
_start:
{
uint8_t v_schemaOk_54_; uint8_t v_hasNetGrant_55_; lean_object* v___x_56_; lean_object* v___x_57_; lean_object* v___x_58_; lean_object* v___x_59_; lean_object* v___x_60_; uint8_t v___x_61_; lean_object* v___x_62_; lean_object* v___x_63_; lean_object* v___x_64_; lean_object* v___x_65_; lean_object* v___x_66_; lean_object* v___x_67_; lean_object* v___x_68_; lean_object* v___x_69_; lean_object* v___x_70_; lean_object* v___x_71_; lean_object* v___x_72_; lean_object* v___x_73_; lean_object* v___x_74_; lean_object* v___x_75_; lean_object* v___x_76_; lean_object* v___x_77_; lean_object* v___x_78_; lean_object* v___x_79_; lean_object* v___x_80_; lean_object* v___x_81_; lean_object* v___x_82_; 
v_schemaOk_54_ = lean_ctor_get_uint8(v_x_53_, 0);
v_hasNetGrant_55_ = lean_ctor_get_uint8(v_x_53_, 1);
v___x_56_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__5));
v___x_57_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__6));
v___x_58_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__7);
v___x_59_ = l_Bool_repr___redArg(v_schemaOk_54_);
v___x_60_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_60_, 0, v___x_58_);
lean_ctor_set(v___x_60_, 1, v___x_59_);
v___x_61_ = 0;
v___x_62_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_62_, 0, v___x_60_);
lean_ctor_set_uint8(v___x_62_, sizeof(void*)*1, v___x_61_);
v___x_63_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_63_, 0, v___x_57_);
lean_ctor_set(v___x_63_, 1, v___x_62_);
v___x_64_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__9));
v___x_65_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_65_, 0, v___x_63_);
lean_ctor_set(v___x_65_, 1, v___x_64_);
v___x_66_ = lean_box(1);
v___x_67_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_67_, 0, v___x_65_);
lean_ctor_set(v___x_67_, 1, v___x_66_);
v___x_68_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__11));
v___x_69_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_69_, 0, v___x_67_);
lean_ctor_set(v___x_69_, 1, v___x_68_);
v___x_70_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_70_, 0, v___x_69_);
lean_ctor_set(v___x_70_, 1, v___x_56_);
v___x_71_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__12);
v___x_72_ = l_Bool_repr___redArg(v_hasNetGrant_55_);
v___x_73_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_73_, 0, v___x_71_);
lean_ctor_set(v___x_73_, 1, v___x_72_);
v___x_74_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_74_, 0, v___x_73_);
lean_ctor_set_uint8(v___x_74_, sizeof(void*)*1, v___x_61_);
v___x_75_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_75_, 0, v___x_70_);
lean_ctor_set(v___x_75_, 1, v___x_74_);
v___x_76_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__15);
v___x_77_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__16));
v___x_78_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_78_, 0, v___x_77_);
lean_ctor_set(v___x_78_, 1, v___x_75_);
v___x_79_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___closed__17));
v___x_80_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_80_, 0, v___x_78_);
lean_ctor_set(v___x_80_, 1, v___x_79_);
v___x_81_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_81_, 0, v___x_76_);
lean_ctor_set(v___x_81_, 1, v___x_80_);
v___x_82_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_82_, 0, v___x_81_);
lean_ctor_set_uint8(v___x_82_, sizeof(void*)*1, v___x_61_);
return v___x_82_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg___boxed(lean_object* v_x_83_){
_start:
{
lean_object* v_res_84_; 
v_res_84_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(v_x_83_);
lean_dec_ref(v_x_83_);
return v_res_84_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr(lean_object* v_x_85_, lean_object* v_prec_86_){
_start:
{
lean_object* v___x_87_; 
v___x_87_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(v_x_85_);
return v___x_87_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___boxed(lean_object* v_x_88_, lean_object* v_prec_89_){
_start:
{
lean_object* v_res_90_; 
v_res_90_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr(v_x_88_, v_prec_89_);
lean_dec(v_prec_89_);
lean_dec_ref(v_x_88_);
return v_res_90_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorIdx(lean_object* v_x_93_){
_start:
{
if (lean_obj_tag(v_x_93_) == 0)
{
lean_object* v___x_94_; 
v___x_94_ = lean_unsigned_to_nat(0u);
return v___x_94_;
}
else
{
lean_object* v___x_95_; 
v___x_95_ = lean_unsigned_to_nat(1u);
return v___x_95_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorIdx___boxed(lean_object* v_x_96_){
_start:
{
lean_object* v_res_97_; 
v_res_97_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorIdx(v_x_96_);
lean_dec_ref(v_x_96_);
return v_res_97_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(lean_object* v_t_98_, lean_object* v_k_99_){
_start:
{
lean_object* v_m_100_; lean_object* v___x_101_; 
v_m_100_ = lean_ctor_get(v_t_98_, 0);
lean_inc_ref(v_m_100_);
lean_dec_ref(v_t_98_);
v___x_101_ = lean_apply_1(v_k_99_, v_m_100_);
return v___x_101_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim(lean_object* v_motive_102_, lean_object* v_ctorIdx_103_, lean_object* v_t_104_, lean_object* v_h_105_, lean_object* v_k_106_){
_start:
{
lean_object* v___x_107_; 
v___x_107_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(v_t_104_, v_k_106_);
return v___x_107_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___boxed(lean_object* v_motive_108_, lean_object* v_ctorIdx_109_, lean_object* v_t_110_, lean_object* v_h_111_, lean_object* v_k_112_){
_start:
{
lean_object* v_res_113_; 
v_res_113_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim(v_motive_108_, v_ctorIdx_109_, v_t_110_, v_h_111_, v_k_112_);
lean_dec(v_ctorIdx_109_);
return v_res_113_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ok_elim___redArg(lean_object* v_t_114_, lean_object* v_ok_115_){
_start:
{
lean_object* v___x_116_; 
v___x_116_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(v_t_114_, v_ok_115_);
return v___x_116_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ok_elim(lean_object* v_motive_117_, lean_object* v_t_118_, lean_object* v_h_119_, lean_object* v_ok_120_){
_start:
{
lean_object* v___x_121_; 
v___x_121_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(v_t_118_, v_ok_120_);
return v___x_121_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_err_elim___redArg(lean_object* v_t_122_, lean_object* v_err_123_){
_start:
{
lean_object* v___x_124_; 
v___x_124_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(v_t_122_, v_err_123_);
return v___x_124_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_err_elim(lean_object* v_motive_125_, lean_object* v_t_126_, lean_object* v_h_127_, lean_object* v_err_128_){
_start:
{
lean_object* v___x_129_; 
v___x_129_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_LoadResult_ctorElim___redArg(v_t_126_, v_err_128_);
return v___x_129_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq(lean_object* v_x_130_, lean_object* v_x_131_){
_start:
{
if (lean_obj_tag(v_x_130_) == 0)
{
if (lean_obj_tag(v_x_131_) == 0)
{
lean_object* v_m_132_; lean_object* v_m_133_; uint8_t v___x_134_; 
v_m_132_ = lean_ctor_get(v_x_130_, 0);
v_m_133_ = lean_ctor_get(v_x_131_, 0);
v___x_134_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(v_m_132_, v_m_133_);
return v___x_134_;
}
else
{
uint8_t v___x_135_; 
v___x_135_ = 0;
return v___x_135_;
}
}
else
{
if (lean_obj_tag(v_x_131_) == 0)
{
uint8_t v___x_136_; 
v___x_136_ = 0;
return v___x_136_;
}
else
{
lean_object* v_reason_137_; lean_object* v_reason_138_; uint8_t v___x_139_; 
v_reason_137_ = lean_ctor_get(v_x_130_, 0);
v_reason_138_ = lean_ctor_get(v_x_131_, 0);
v___x_139_ = lean_string_dec_eq(v_reason_137_, v_reason_138_);
return v___x_139_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq___boxed(lean_object* v_x_140_, lean_object* v_x_141_){
_start:
{
uint8_t v_res_142_; lean_object* v_r_143_; 
v_res_142_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq(v_x_140_, v_x_141_);
lean_dec_ref(v_x_141_);
lean_dec_ref(v_x_140_);
v_r_143_ = lean_box(v_res_142_);
return v_r_143_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult(lean_object* v_x_144_, lean_object* v_x_145_){
_start:
{
uint8_t v___x_146_; 
v___x_146_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult_decEq(v_x_144_, v_x_145_);
return v___x_146_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult___boxed(lean_object* v_x_147_, lean_object* v_x_148_){
_start:
{
uint8_t v_res_149_; lean_object* v_r_150_; 
v_res_149_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqLoadResult(v_x_147_, v_x_148_);
lean_dec_ref(v_x_148_);
lean_dec_ref(v_x_147_);
v_r_150_ = lean_box(v_res_149_);
return v_r_150_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3(void){
_start:
{
lean_object* v___x_157_; lean_object* v___x_158_; 
v___x_157_ = lean_unsigned_to_nat(2u);
v___x_158_ = lean_nat_to_int(v___x_157_);
return v___x_158_;
}
}
static lean_object* _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4(void){
_start:
{
lean_object* v___x_159_; lean_object* v___x_160_; 
v___x_159_ = lean_unsigned_to_nat(1u);
v___x_160_ = lean_nat_to_int(v___x_159_);
return v___x_160_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr(lean_object* v_x_167_, lean_object* v_prec_168_){
_start:
{
if (lean_obj_tag(v_x_167_) == 0)
{
lean_object* v_m_169_; lean_object* v___y_171_; lean_object* v___x_179_; uint8_t v___x_180_; 
v_m_169_ = lean_ctor_get(v_x_167_, 0);
lean_inc_ref(v_m_169_);
lean_dec_ref_known(v_x_167_, 1);
v___x_179_ = lean_unsigned_to_nat(1024u);
v___x_180_ = lean_nat_dec_le(v___x_179_, v_prec_168_);
if (v___x_180_ == 0)
{
lean_object* v___x_181_; 
v___x_181_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_171_ = v___x_181_;
goto v___jp_170_;
}
else
{
lean_object* v___x_182_; 
v___x_182_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_171_ = v___x_182_;
goto v___jp_170_;
}
v___jp_170_:
{
lean_object* v___x_172_; lean_object* v___x_173_; lean_object* v___x_174_; lean_object* v___x_175_; uint8_t v___x_176_; lean_object* v___x_177_; lean_object* v___x_178_; 
v___x_172_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__2));
v___x_173_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(v_m_169_);
lean_dec_ref(v_m_169_);
v___x_174_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_174_, 0, v___x_172_);
lean_ctor_set(v___x_174_, 1, v___x_173_);
lean_inc(v___y_171_);
v___x_175_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_175_, 0, v___y_171_);
lean_ctor_set(v___x_175_, 1, v___x_174_);
v___x_176_ = 0;
v___x_177_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_177_, 0, v___x_175_);
lean_ctor_set_uint8(v___x_177_, sizeof(void*)*1, v___x_176_);
v___x_178_ = l_Repr_addAppParen(v___x_177_, v_prec_168_);
return v___x_178_;
}
}
else
{
lean_object* v_reason_183_; lean_object* v___x_185_; uint8_t v_isShared_186_; uint8_t v_isSharedCheck_203_; 
v_reason_183_ = lean_ctor_get(v_x_167_, 0);
v_isSharedCheck_203_ = !lean_is_exclusive(v_x_167_);
if (v_isSharedCheck_203_ == 0)
{
v___x_185_ = v_x_167_;
v_isShared_186_ = v_isSharedCheck_203_;
goto v_resetjp_184_;
}
else
{
lean_inc(v_reason_183_);
lean_dec(v_x_167_);
v___x_185_ = lean_box(0);
v_isShared_186_ = v_isSharedCheck_203_;
goto v_resetjp_184_;
}
v_resetjp_184_:
{
lean_object* v___y_188_; lean_object* v___x_199_; uint8_t v___x_200_; 
v___x_199_ = lean_unsigned_to_nat(1024u);
v___x_200_ = lean_nat_dec_le(v___x_199_, v_prec_168_);
if (v___x_200_ == 0)
{
lean_object* v___x_201_; 
v___x_201_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_188_ = v___x_201_;
goto v___jp_187_;
}
else
{
lean_object* v___x_202_; 
v___x_202_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_188_ = v___x_202_;
goto v___jp_187_;
}
v___jp_187_:
{
lean_object* v___x_189_; lean_object* v___x_190_; lean_object* v___x_192_; 
v___x_189_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__7));
v___x_190_ = l_String_quote(v_reason_183_);
if (v_isShared_186_ == 0)
{
lean_ctor_set_tag(v___x_185_, 3);
lean_ctor_set(v___x_185_, 0, v___x_190_);
v___x_192_ = v___x_185_;
goto v_reusejp_191_;
}
else
{
lean_object* v_reuseFailAlloc_198_; 
v_reuseFailAlloc_198_ = lean_alloc_ctor(3, 1, 0);
lean_ctor_set(v_reuseFailAlloc_198_, 0, v___x_190_);
v___x_192_ = v_reuseFailAlloc_198_;
goto v_reusejp_191_;
}
v_reusejp_191_:
{
lean_object* v___x_193_; lean_object* v___x_194_; uint8_t v___x_195_; lean_object* v___x_196_; lean_object* v___x_197_; 
v___x_193_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_193_, 0, v___x_189_);
lean_ctor_set(v___x_193_, 1, v___x_192_);
lean_inc(v___y_188_);
v___x_194_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_194_, 0, v___y_188_);
lean_ctor_set(v___x_194_, 1, v___x_193_);
v___x_195_ = 0;
v___x_196_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_196_, 0, v___x_194_);
lean_ctor_set_uint8(v___x_196_, sizeof(void*)*1, v___x_195_);
v___x_197_ = l_Repr_addAppParen(v___x_196_, v_prec_168_);
return v___x_197_;
}
}
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___boxed(lean_object* v_x_204_, lean_object* v_prec_205_){
_start:
{
lean_object* v_res_206_; 
v_res_206_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr(v_x_204_, v_prec_205_);
lean_dec(v_prec_205_);
return v_res_206_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(uint8_t v_x_209_){
_start:
{
switch(v_x_209_)
{
case 0:
{
lean_object* v___x_210_; 
v___x_210_ = lean_unsigned_to_nat(0u);
return v___x_210_;
}
case 1:
{
lean_object* v___x_211_; 
v___x_211_ = lean_unsigned_to_nat(1u);
return v___x_211_;
}
case 2:
{
lean_object* v___x_212_; 
v___x_212_ = lean_unsigned_to_nat(2u);
return v___x_212_;
}
case 3:
{
lean_object* v___x_213_; 
v___x_213_ = lean_unsigned_to_nat(3u);
return v___x_213_;
}
case 4:
{
lean_object* v___x_214_; 
v___x_214_ = lean_unsigned_to_nat(4u);
return v___x_214_;
}
default: 
{
lean_object* v___x_215_; 
v___x_215_ = lean_unsigned_to_nat(5u);
return v___x_215_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx___boxed(lean_object* v_x_216_){
_start:
{
uint8_t v_x_boxed_217_; lean_object* v_res_218_; 
v_x_boxed_217_ = lean_unbox(v_x_216_);
v_res_218_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(v_x_boxed_217_);
return v_res_218_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___redArg(lean_object* v_k_219_){
_start:
{
lean_inc(v_k_219_);
return v_k_219_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___redArg___boxed(lean_object* v_k_220_){
_start:
{
lean_object* v_res_221_; 
v_res_221_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___redArg(v_k_220_);
lean_dec(v_k_220_);
return v_res_221_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim(lean_object* v_motive_222_, lean_object* v_ctorIdx_223_, uint8_t v_t_224_, lean_object* v_h_225_, lean_object* v_k_226_){
_start:
{
lean_inc(v_k_226_);
return v_k_226_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim___boxed(lean_object* v_motive_227_, lean_object* v_ctorIdx_228_, lean_object* v_t_229_, lean_object* v_h_230_, lean_object* v_k_231_){
_start:
{
uint8_t v_t_boxed_232_; lean_object* v_res_233_; 
v_t_boxed_232_ = lean_unbox(v_t_229_);
v_res_233_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorElim(v_motive_227_, v_ctorIdx_228_, v_t_boxed_232_, v_h_230_, v_k_231_);
lean_dec(v_k_231_);
lean_dec(v_ctorIdx_228_);
return v_res_233_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___redArg(lean_object* v_unknownField_234_){
_start:
{
lean_inc(v_unknownField_234_);
return v_unknownField_234_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___redArg___boxed(lean_object* v_unknownField_235_){
_start:
{
lean_object* v_res_236_; 
v_res_236_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___redArg(v_unknownField_235_);
lean_dec(v_unknownField_235_);
return v_res_236_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim(lean_object* v_motive_237_, uint8_t v_t_238_, lean_object* v_h_239_, lean_object* v_unknownField_240_){
_start:
{
lean_inc(v_unknownField_240_);
return v_unknownField_240_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim___boxed(lean_object* v_motive_241_, lean_object* v_t_242_, lean_object* v_h_243_, lean_object* v_unknownField_244_){
_start:
{
uint8_t v_t_boxed_245_; lean_object* v_res_246_; 
v_t_boxed_245_ = lean_unbox(v_t_242_);
v_res_246_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_unknownField_elim(v_motive_241_, v_t_boxed_245_, v_h_243_, v_unknownField_244_);
lean_dec(v_unknownField_244_);
return v_res_246_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___redArg(lean_object* v_badSchema_247_){
_start:
{
lean_inc(v_badSchema_247_);
return v_badSchema_247_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___redArg___boxed(lean_object* v_badSchema_248_){
_start:
{
lean_object* v_res_249_; 
v_res_249_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___redArg(v_badSchema_248_);
lean_dec(v_badSchema_248_);
return v_res_249_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim(lean_object* v_motive_250_, uint8_t v_t_251_, lean_object* v_h_252_, lean_object* v_badSchema_253_){
_start:
{
lean_inc(v_badSchema_253_);
return v_badSchema_253_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim___boxed(lean_object* v_motive_254_, lean_object* v_t_255_, lean_object* v_h_256_, lean_object* v_badSchema_257_){
_start:
{
uint8_t v_t_boxed_258_; lean_object* v_res_259_; 
v_t_boxed_258_ = lean_unbox(v_t_255_);
v_res_259_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_badSchema_elim(v_motive_254_, v_t_boxed_258_, v_h_256_, v_badSchema_257_);
lean_dec(v_badSchema_257_);
return v_res_259_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___redArg(lean_object* v_emptyGlob_260_){
_start:
{
lean_inc(v_emptyGlob_260_);
return v_emptyGlob_260_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___redArg___boxed(lean_object* v_emptyGlob_261_){
_start:
{
lean_object* v_res_262_; 
v_res_262_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___redArg(v_emptyGlob_261_);
lean_dec(v_emptyGlob_261_);
return v_res_262_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim(lean_object* v_motive_263_, uint8_t v_t_264_, lean_object* v_h_265_, lean_object* v_emptyGlob_266_){
_start:
{
lean_inc(v_emptyGlob_266_);
return v_emptyGlob_266_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim___boxed(lean_object* v_motive_267_, lean_object* v_t_268_, lean_object* v_h_269_, lean_object* v_emptyGlob_270_){
_start:
{
uint8_t v_t_boxed_271_; lean_object* v_res_272_; 
v_t_boxed_271_ = lean_unbox(v_t_268_);
v_res_272_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyGlob_elim(v_motive_267_, v_t_boxed_271_, v_h_269_, v_emptyGlob_270_);
lean_dec(v_emptyGlob_270_);
return v_res_272_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___redArg(lean_object* v_negativeBudget_273_){
_start:
{
lean_inc(v_negativeBudget_273_);
return v_negativeBudget_273_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___redArg___boxed(lean_object* v_negativeBudget_274_){
_start:
{
lean_object* v_res_275_; 
v_res_275_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___redArg(v_negativeBudget_274_);
lean_dec(v_negativeBudget_274_);
return v_res_275_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim(lean_object* v_motive_276_, uint8_t v_t_277_, lean_object* v_h_278_, lean_object* v_negativeBudget_279_){
_start:
{
lean_inc(v_negativeBudget_279_);
return v_negativeBudget_279_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim___boxed(lean_object* v_motive_280_, lean_object* v_t_281_, lean_object* v_h_282_, lean_object* v_negativeBudget_283_){
_start:
{
uint8_t v_t_boxed_284_; lean_object* v_res_285_; 
v_t_boxed_284_ = lean_unbox(v_t_281_);
v_res_285_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_negativeBudget_elim(v_motive_280_, v_t_boxed_284_, v_h_282_, v_negativeBudget_283_);
lean_dec(v_negativeBudget_283_);
return v_res_285_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___redArg(lean_object* v_emptyNetGrant_286_){
_start:
{
lean_inc(v_emptyNetGrant_286_);
return v_emptyNetGrant_286_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___redArg___boxed(lean_object* v_emptyNetGrant_287_){
_start:
{
lean_object* v_res_288_; 
v_res_288_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___redArg(v_emptyNetGrant_287_);
lean_dec(v_emptyNetGrant_287_);
return v_res_288_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim(lean_object* v_motive_289_, uint8_t v_t_290_, lean_object* v_h_291_, lean_object* v_emptyNetGrant_292_){
_start:
{
lean_inc(v_emptyNetGrant_292_);
return v_emptyNetGrant_292_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim___boxed(lean_object* v_motive_293_, lean_object* v_t_294_, lean_object* v_h_295_, lean_object* v_emptyNetGrant_296_){
_start:
{
uint8_t v_t_boxed_297_; lean_object* v_res_298_; 
v_t_boxed_297_ = lean_unbox(v_t_294_);
v_res_298_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_emptyNetGrant_elim(v_motive_293_, v_t_boxed_297_, v_h_295_, v_emptyNetGrant_296_);
lean_dec(v_emptyNetGrant_296_);
return v_res_298_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___redArg(lean_object* v_multiJSON_299_){
_start:
{
lean_inc(v_multiJSON_299_);
return v_multiJSON_299_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___redArg___boxed(lean_object* v_multiJSON_300_){
_start:
{
lean_object* v_res_301_; 
v_res_301_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___redArg(v_multiJSON_300_);
lean_dec(v_multiJSON_300_);
return v_res_301_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim(lean_object* v_motive_302_, uint8_t v_t_303_, lean_object* v_h_304_, lean_object* v_multiJSON_305_){
_start:
{
lean_inc(v_multiJSON_305_);
return v_multiJSON_305_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim___boxed(lean_object* v_motive_306_, lean_object* v_t_307_, lean_object* v_h_308_, lean_object* v_multiJSON_309_){
_start:
{
uint8_t v_t_boxed_310_; lean_object* v_res_311_; 
v_t_boxed_310_ = lean_unbox(v_t_307_);
v_res_311_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_multiJSON_elim(v_motive_306_, v_t_boxed_310_, v_h_308_, v_multiJSON_309_);
lean_dec(v_multiJSON_309_);
return v_res_311_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ofNat(lean_object* v_n_312_){
_start:
{
lean_object* v___x_313_; uint8_t v___x_314_; 
v___x_313_ = lean_unsigned_to_nat(2u);
v___x_314_ = lean_nat_dec_le(v_n_312_, v___x_313_);
if (v___x_314_ == 0)
{
lean_object* v___x_315_; uint8_t v___x_316_; 
v___x_315_ = lean_unsigned_to_nat(3u);
v___x_316_ = lean_nat_dec_le(v_n_312_, v___x_315_);
if (v___x_316_ == 0)
{
lean_object* v___x_317_; uint8_t v___x_318_; 
v___x_317_ = lean_unsigned_to_nat(4u);
v___x_318_ = lean_nat_dec_le(v_n_312_, v___x_317_);
if (v___x_318_ == 0)
{
uint8_t v___x_319_; 
v___x_319_ = 5;
return v___x_319_;
}
else
{
uint8_t v___x_320_; 
v___x_320_ = 4;
return v___x_320_;
}
}
else
{
uint8_t v___x_321_; 
v___x_321_ = 3;
return v___x_321_;
}
}
else
{
lean_object* v___x_322_; uint8_t v___x_323_; 
v___x_322_ = lean_unsigned_to_nat(0u);
v___x_323_ = lean_nat_dec_le(v_n_312_, v___x_322_);
if (v___x_323_ == 0)
{
lean_object* v___x_324_; uint8_t v___x_325_; 
v___x_324_ = lean_unsigned_to_nat(1u);
v___x_325_ = lean_nat_dec_le(v_n_312_, v___x_324_);
if (v___x_325_ == 0)
{
uint8_t v___x_326_; 
v___x_326_ = 2;
return v___x_326_;
}
else
{
uint8_t v___x_327_; 
v___x_327_ = 1;
return v___x_327_;
}
}
else
{
uint8_t v___x_328_; 
v___x_328_ = 0;
return v___x_328_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ofNat___boxed(lean_object* v_n_329_){
_start:
{
uint8_t v_res_330_; lean_object* v_r_331_; 
v_res_330_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ofNat(v_n_329_);
lean_dec(v_n_329_);
v_r_331_ = lean_box(v_res_330_);
return v_r_331_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqMalform(uint8_t v_x_332_, uint8_t v_y_333_){
_start:
{
lean_object* v___x_334_; lean_object* v___x_335_; uint8_t v___x_336_; 
v___x_334_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(v_x_332_);
v___x_335_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(v_y_333_);
v___x_336_ = lean_nat_dec_eq(v___x_334_, v___x_335_);
lean_dec(v___x_335_);
lean_dec(v___x_334_);
return v___x_336_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqMalform___boxed(lean_object* v_x_337_, lean_object* v_y_338_){
_start:
{
uint8_t v_x_20__boxed_339_; uint8_t v_y_21__boxed_340_; uint8_t v_res_341_; lean_object* v_r_342_; 
v_x_20__boxed_339_ = lean_unbox(v_x_337_);
v_y_21__boxed_340_ = lean_unbox(v_y_338_);
v_res_341_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqMalform(v_x_20__boxed_339_, v_y_21__boxed_340_);
v_r_342_ = lean_box(v_res_341_);
return v_r_342_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr(uint8_t v_x_361_, lean_object* v_prec_362_){
_start:
{
lean_object* v___y_364_; lean_object* v___y_371_; lean_object* v___y_378_; lean_object* v___y_385_; lean_object* v___y_392_; lean_object* v___y_399_; 
switch(v_x_361_)
{
case 0:
{
lean_object* v___x_405_; uint8_t v___x_406_; 
v___x_405_ = lean_unsigned_to_nat(1024u);
v___x_406_ = lean_nat_dec_le(v___x_405_, v_prec_362_);
if (v___x_406_ == 0)
{
lean_object* v___x_407_; 
v___x_407_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_364_ = v___x_407_;
goto v___jp_363_;
}
else
{
lean_object* v___x_408_; 
v___x_408_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_364_ = v___x_408_;
goto v___jp_363_;
}
}
case 1:
{
lean_object* v___x_409_; uint8_t v___x_410_; 
v___x_409_ = lean_unsigned_to_nat(1024u);
v___x_410_ = lean_nat_dec_le(v___x_409_, v_prec_362_);
if (v___x_410_ == 0)
{
lean_object* v___x_411_; 
v___x_411_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_371_ = v___x_411_;
goto v___jp_370_;
}
else
{
lean_object* v___x_412_; 
v___x_412_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_371_ = v___x_412_;
goto v___jp_370_;
}
}
case 2:
{
lean_object* v___x_413_; uint8_t v___x_414_; 
v___x_413_ = lean_unsigned_to_nat(1024u);
v___x_414_ = lean_nat_dec_le(v___x_413_, v_prec_362_);
if (v___x_414_ == 0)
{
lean_object* v___x_415_; 
v___x_415_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_378_ = v___x_415_;
goto v___jp_377_;
}
else
{
lean_object* v___x_416_; 
v___x_416_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_378_ = v___x_416_;
goto v___jp_377_;
}
}
case 3:
{
lean_object* v___x_417_; uint8_t v___x_418_; 
v___x_417_ = lean_unsigned_to_nat(1024u);
v___x_418_ = lean_nat_dec_le(v___x_417_, v_prec_362_);
if (v___x_418_ == 0)
{
lean_object* v___x_419_; 
v___x_419_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_385_ = v___x_419_;
goto v___jp_384_;
}
else
{
lean_object* v___x_420_; 
v___x_420_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_385_ = v___x_420_;
goto v___jp_384_;
}
}
case 4:
{
lean_object* v___x_421_; uint8_t v___x_422_; 
v___x_421_ = lean_unsigned_to_nat(1024u);
v___x_422_ = lean_nat_dec_le(v___x_421_, v_prec_362_);
if (v___x_422_ == 0)
{
lean_object* v___x_423_; 
v___x_423_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_392_ = v___x_423_;
goto v___jp_391_;
}
else
{
lean_object* v___x_424_; 
v___x_424_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_392_ = v___x_424_;
goto v___jp_391_;
}
}
default: 
{
lean_object* v___x_425_; uint8_t v___x_426_; 
v___x_425_ = lean_unsigned_to_nat(1024u);
v___x_426_ = lean_nat_dec_le(v___x_425_, v_prec_362_);
if (v___x_426_ == 0)
{
lean_object* v___x_427_; 
v___x_427_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_399_ = v___x_427_;
goto v___jp_398_;
}
else
{
lean_object* v___x_428_; 
v___x_428_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_399_ = v___x_428_;
goto v___jp_398_;
}
}
}
v___jp_363_:
{
lean_object* v___x_365_; lean_object* v___x_366_; uint8_t v___x_367_; lean_object* v___x_368_; lean_object* v___x_369_; 
v___x_365_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__1));
lean_inc(v___y_364_);
v___x_366_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_366_, 0, v___y_364_);
lean_ctor_set(v___x_366_, 1, v___x_365_);
v___x_367_ = 0;
v___x_368_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_368_, 0, v___x_366_);
lean_ctor_set_uint8(v___x_368_, sizeof(void*)*1, v___x_367_);
v___x_369_ = l_Repr_addAppParen(v___x_368_, v_prec_362_);
return v___x_369_;
}
v___jp_370_:
{
lean_object* v___x_372_; lean_object* v___x_373_; uint8_t v___x_374_; lean_object* v___x_375_; lean_object* v___x_376_; 
v___x_372_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__3));
lean_inc(v___y_371_);
v___x_373_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_373_, 0, v___y_371_);
lean_ctor_set(v___x_373_, 1, v___x_372_);
v___x_374_ = 0;
v___x_375_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_375_, 0, v___x_373_);
lean_ctor_set_uint8(v___x_375_, sizeof(void*)*1, v___x_374_);
v___x_376_ = l_Repr_addAppParen(v___x_375_, v_prec_362_);
return v___x_376_;
}
v___jp_377_:
{
lean_object* v___x_379_; lean_object* v___x_380_; uint8_t v___x_381_; lean_object* v___x_382_; lean_object* v___x_383_; 
v___x_379_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__5));
lean_inc(v___y_378_);
v___x_380_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_380_, 0, v___y_378_);
lean_ctor_set(v___x_380_, 1, v___x_379_);
v___x_381_ = 0;
v___x_382_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_382_, 0, v___x_380_);
lean_ctor_set_uint8(v___x_382_, sizeof(void*)*1, v___x_381_);
v___x_383_ = l_Repr_addAppParen(v___x_382_, v_prec_362_);
return v___x_383_;
}
v___jp_384_:
{
lean_object* v___x_386_; lean_object* v___x_387_; uint8_t v___x_388_; lean_object* v___x_389_; lean_object* v___x_390_; 
v___x_386_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__7));
lean_inc(v___y_385_);
v___x_387_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_387_, 0, v___y_385_);
lean_ctor_set(v___x_387_, 1, v___x_386_);
v___x_388_ = 0;
v___x_389_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_389_, 0, v___x_387_);
lean_ctor_set_uint8(v___x_389_, sizeof(void*)*1, v___x_388_);
v___x_390_ = l_Repr_addAppParen(v___x_389_, v_prec_362_);
return v___x_390_;
}
v___jp_391_:
{
lean_object* v___x_393_; lean_object* v___x_394_; uint8_t v___x_395_; lean_object* v___x_396_; lean_object* v___x_397_; 
v___x_393_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__9));
lean_inc(v___y_392_);
v___x_394_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_394_, 0, v___y_392_);
lean_ctor_set(v___x_394_, 1, v___x_393_);
v___x_395_ = 0;
v___x_396_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_396_, 0, v___x_394_);
lean_ctor_set_uint8(v___x_396_, sizeof(void*)*1, v___x_395_);
v___x_397_ = l_Repr_addAppParen(v___x_396_, v_prec_362_);
return v___x_397_;
}
v___jp_398_:
{
lean_object* v___x_400_; lean_object* v___x_401_; uint8_t v___x_402_; lean_object* v___x_403_; lean_object* v___x_404_; 
v___x_400_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___closed__11));
lean_inc(v___y_399_);
v___x_401_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_401_, 0, v___y_399_);
lean_ctor_set(v___x_401_, 1, v___x_400_);
v___x_402_ = 0;
v___x_403_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_403_, 0, v___x_401_);
lean_ctor_set_uint8(v___x_403_, sizeof(void*)*1, v___x_402_);
v___x_404_ = l_Repr_addAppParen(v___x_403_, v_prec_362_);
return v___x_404_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr___boxed(lean_object* v_x_429_, lean_object* v_prec_430_){
_start:
{
uint8_t v_x_329__boxed_431_; lean_object* v_res_432_; 
v_x_329__boxed_431_ = lean_unbox(v_x_429_);
v_res_432_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr(v_x_329__boxed_431_, v_prec_430_);
lean_dec(v_prec_430_);
return v_res_432_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorIdx(lean_object* v_x_435_){
_start:
{
if (lean_obj_tag(v_x_435_) == 0)
{
lean_object* v___x_436_; 
v___x_436_ = lean_unsigned_to_nat(0u);
return v___x_436_;
}
else
{
lean_object* v___x_437_; 
v___x_437_ = lean_unsigned_to_nat(1u);
return v___x_437_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorIdx___boxed(lean_object* v_x_438_){
_start:
{
lean_object* v_res_439_; 
v_res_439_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorIdx(v_x_438_);
lean_dec_ref(v_x_438_);
return v_res_439_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(lean_object* v_t_440_, lean_object* v_k_441_){
_start:
{
if (lean_obj_tag(v_t_440_) == 0)
{
lean_object* v_m_442_; lean_object* v___x_443_; 
v_m_442_ = lean_ctor_get(v_t_440_, 0);
lean_inc_ref(v_m_442_);
lean_dec_ref_known(v_t_440_, 1);
v___x_443_ = lean_apply_1(v_k_441_, v_m_442_);
return v___x_443_;
}
else
{
uint8_t v_kind_444_; lean_object* v___x_445_; lean_object* v___x_446_; 
v_kind_444_ = lean_ctor_get_uint8(v_t_440_, 0);
lean_dec_ref_known(v_t_440_, 0);
v___x_445_ = lean_box(v_kind_444_);
v___x_446_ = lean_apply_1(v_k_441_, v___x_445_);
return v___x_446_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim(lean_object* v_motive_447_, lean_object* v_ctorIdx_448_, lean_object* v_t_449_, lean_object* v_h_450_, lean_object* v_k_451_){
_start:
{
lean_object* v___x_452_; 
v___x_452_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(v_t_449_, v_k_451_);
return v___x_452_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___boxed(lean_object* v_motive_453_, lean_object* v_ctorIdx_454_, lean_object* v_t_455_, lean_object* v_h_456_, lean_object* v_k_457_){
_start:
{
lean_object* v_res_458_; 
v_res_458_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim(v_motive_453_, v_ctorIdx_454_, v_t_455_, v_h_456_, v_k_457_);
lean_dec(v_ctorIdx_454_);
return v_res_458_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_valid_elim___redArg(lean_object* v_t_459_, lean_object* v_valid_460_){
_start:
{
lean_object* v___x_461_; 
v___x_461_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(v_t_459_, v_valid_460_);
return v___x_461_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_valid_elim(lean_object* v_motive_462_, lean_object* v_t_463_, lean_object* v_h_464_, lean_object* v_valid_465_){
_start:
{
lean_object* v___x_466_; 
v___x_466_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(v_t_463_, v_valid_465_);
return v___x_466_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_malformed_elim___redArg(lean_object* v_t_467_, lean_object* v_malformed_468_){
_start:
{
lean_object* v___x_469_; 
v___x_469_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(v_t_467_, v_malformed_468_);
return v___x_469_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_malformed_elim(lean_object* v_motive_470_, lean_object* v_t_471_, lean_object* v_h_472_, lean_object* v_malformed_473_){
_start:
{
lean_object* v___x_474_; 
v___x_474_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Source_ctorElim___redArg(v_t_471_, v_malformed_473_);
return v___x_474_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq(lean_object* v_x_475_, lean_object* v_x_476_){
_start:
{
if (lean_obj_tag(v_x_475_) == 0)
{
if (lean_obj_tag(v_x_476_) == 0)
{
lean_object* v_m_477_; lean_object* v_m_478_; uint8_t v___x_479_; 
v_m_477_ = lean_ctor_get(v_x_475_, 0);
v_m_478_ = lean_ctor_get(v_x_476_, 0);
v___x_479_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqManifest_decEq(v_m_477_, v_m_478_);
return v___x_479_;
}
else
{
uint8_t v___x_480_; 
v___x_480_ = 0;
return v___x_480_;
}
}
else
{
if (lean_obj_tag(v_x_476_) == 0)
{
uint8_t v___x_481_; 
v___x_481_ = 0;
return v___x_481_;
}
else
{
uint8_t v_kind_482_; uint8_t v_kind_483_; lean_object* v___x_484_; lean_object* v___x_485_; uint8_t v___x_486_; 
v_kind_482_ = lean_ctor_get_uint8(v_x_475_, 0);
v_kind_483_ = lean_ctor_get_uint8(v_x_476_, 0);
v___x_484_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(v_kind_482_);
v___x_485_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_Malform_ctorIdx(v_kind_483_);
v___x_486_ = lean_nat_dec_eq(v___x_484_, v___x_485_);
lean_dec(v___x_485_);
lean_dec(v___x_484_);
return v___x_486_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq___boxed(lean_object* v_x_487_, lean_object* v_x_488_){
_start:
{
uint8_t v_res_489_; lean_object* v_r_490_; 
v_res_489_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq(v_x_487_, v_x_488_);
lean_dec_ref(v_x_488_);
lean_dec_ref(v_x_487_);
v_r_490_ = lean_box(v_res_489_);
return v_r_490_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource(lean_object* v_x_491_, lean_object* v_x_492_){
_start:
{
uint8_t v___x_493_; 
v___x_493_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource_decEq(v_x_491_, v_x_492_);
return v___x_493_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource___boxed(lean_object* v_x_494_, lean_object* v_x_495_){
_start:
{
uint8_t v_res_496_; lean_object* v_r_497_; 
v_res_496_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instDecidableEqSource(v_x_494_, v_x_495_);
lean_dec_ref(v_x_495_);
lean_dec_ref(v_x_494_);
v_r_497_ = lean_box(v_res_496_);
return v_r_497_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr(lean_object* v_x_510_, lean_object* v_prec_511_){
_start:
{
if (lean_obj_tag(v_x_510_) == 0)
{
lean_object* v_m_512_; lean_object* v___y_514_; lean_object* v___x_522_; uint8_t v___x_523_; 
v_m_512_ = lean_ctor_get(v_x_510_, 0);
v___x_522_ = lean_unsigned_to_nat(1024u);
v___x_523_ = lean_nat_dec_le(v___x_522_, v_prec_511_);
if (v___x_523_ == 0)
{
lean_object* v___x_524_; 
v___x_524_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_514_ = v___x_524_;
goto v___jp_513_;
}
else
{
lean_object* v___x_525_; 
v___x_525_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_514_ = v___x_525_;
goto v___jp_513_;
}
v___jp_513_:
{
lean_object* v___x_515_; lean_object* v___x_516_; lean_object* v___x_517_; lean_object* v___x_518_; uint8_t v___x_519_; lean_object* v___x_520_; lean_object* v___x_521_; 
v___x_515_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__2));
v___x_516_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprManifest_repr___redArg(v_m_512_);
v___x_517_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_517_, 0, v___x_515_);
lean_ctor_set(v___x_517_, 1, v___x_516_);
lean_inc(v___y_514_);
v___x_518_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_518_, 0, v___y_514_);
lean_ctor_set(v___x_518_, 1, v___x_517_);
v___x_519_ = 0;
v___x_520_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_520_, 0, v___x_518_);
lean_ctor_set_uint8(v___x_520_, sizeof(void*)*1, v___x_519_);
v___x_521_ = l_Repr_addAppParen(v___x_520_, v_prec_511_);
return v___x_521_;
}
}
else
{
uint8_t v_kind_526_; lean_object* v___y_528_; lean_object* v___x_537_; uint8_t v___x_538_; 
v_kind_526_ = lean_ctor_get_uint8(v_x_510_, 0);
v___x_537_ = lean_unsigned_to_nat(1024u);
v___x_538_ = lean_nat_dec_le(v___x_537_, v_prec_511_);
if (v___x_538_ == 0)
{
lean_object* v___x_539_; 
v___x_539_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__3);
v___y_528_ = v___x_539_;
goto v___jp_527_;
}
else
{
lean_object* v___x_540_; 
v___x_540_ = lean_obj_once(&lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4, &lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4_once, _init_lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprLoadResult_repr___closed__4);
v___y_528_ = v___x_540_;
goto v___jp_527_;
}
v___jp_527_:
{
lean_object* v___x_529_; lean_object* v___x_530_; lean_object* v___x_531_; lean_object* v___x_532_; lean_object* v___x_533_; uint8_t v___x_534_; lean_object* v___x_535_; lean_object* v___x_536_; 
v___x_529_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___closed__5));
v___x_530_ = lean_unsigned_to_nat(1024u);
v___x_531_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprMalform_repr(v_kind_526_, v___x_530_);
v___x_532_ = lean_alloc_ctor(5, 2, 0);
lean_ctor_set(v___x_532_, 0, v___x_529_);
lean_ctor_set(v___x_532_, 1, v___x_531_);
lean_inc(v___y_528_);
v___x_533_ = lean_alloc_ctor(4, 2, 0);
lean_ctor_set(v___x_533_, 0, v___y_528_);
lean_ctor_set(v___x_533_, 1, v___x_532_);
v___x_534_ = 0;
v___x_535_ = lean_alloc_ctor(6, 1, 1);
lean_ctor_set(v___x_535_, 0, v___x_533_);
lean_ctor_set_uint8(v___x_535_, sizeof(void*)*1, v___x_534_);
v___x_536_ = l_Repr_addAppParen(v___x_535_, v_prec_511_);
return v___x_536_;
}
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr___boxed(lean_object* v_x_541_, lean_object* v_prec_542_){
_start:
{
lean_object* v_res_543_; 
v_res_543_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_instReprSource_repr(v_x_541_, v_prec_542_);
lean_dec(v_prec_542_);
lean_dec_ref(v_x_541_);
return v_res_543_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_parse(lean_object* v_x_552_){
_start:
{
if (lean_obj_tag(v_x_552_) == 0)
{
lean_object* v_m_553_; lean_object* v___x_555_; uint8_t v_isShared_556_; uint8_t v_isSharedCheck_562_; 
v_m_553_ = lean_ctor_get(v_x_552_, 0);
v_isSharedCheck_562_ = !lean_is_exclusive(v_x_552_);
if (v_isSharedCheck_562_ == 0)
{
v___x_555_ = v_x_552_;
v_isShared_556_ = v_isSharedCheck_562_;
goto v_resetjp_554_;
}
else
{
lean_inc(v_m_553_);
lean_dec(v_x_552_);
v___x_555_ = lean_box(0);
v_isShared_556_ = v_isSharedCheck_562_;
goto v_resetjp_554_;
}
v_resetjp_554_:
{
uint8_t v_schemaOk_557_; 
v_schemaOk_557_ = lean_ctor_get_uint8(v_m_553_, 0);
if (v_schemaOk_557_ == 0)
{
lean_object* v___x_558_; 
lean_del_object(v___x_555_);
lean_dec_ref(v_m_553_);
v___x_558_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__1));
return v___x_558_;
}
else
{
lean_object* v___x_560_; 
if (v_isShared_556_ == 0)
{
v___x_560_ = v___x_555_;
goto v_reusejp_559_;
}
else
{
lean_object* v_reuseFailAlloc_561_; 
v_reuseFailAlloc_561_ = lean_alloc_ctor(0, 1, 0);
lean_ctor_set(v_reuseFailAlloc_561_, 0, v_m_553_);
v___x_560_ = v_reuseFailAlloc_561_;
goto v_reusejp_559_;
}
v_reusejp_559_:
{
return v___x_560_;
}
}
}
}
else
{
lean_object* v___x_563_; 
lean_dec_ref_known(v_x_552_, 0);
v___x_563_ = ((lean_object*)(lp_t863_x2dlean_Jevons_Writ_FailClosed_parse___closed__3));
return v___x_563_;
}
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_isMalformed(lean_object* v_x_564_){
_start:
{
if (lean_obj_tag(v_x_564_) == 0)
{
uint8_t v___x_565_; 
v___x_565_ = 0;
return v___x_565_;
}
else
{
uint8_t v___x_566_; 
v___x_566_ = 1;
return v___x_566_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_isMalformed___boxed(lean_object* v_x_567_){
_start:
{
uint8_t v_res_568_; lean_object* v_r_569_; 
v_res_568_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_isMalformed(v_x_567_);
lean_dec_ref(v_x_567_);
v_r_569_ = lean_box(v_res_568_);
return v_r_569_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_mayExecute(lean_object* v_x_570_){
_start:
{
if (lean_obj_tag(v_x_570_) == 0)
{
uint8_t v___x_571_; 
v___x_571_ = 1;
return v___x_571_;
}
else
{
uint8_t v___x_572_; 
v___x_572_ = 0;
return v___x_572_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_mayExecute___boxed(lean_object* v_x_573_){
_start:
{
uint8_t v_res_574_; lean_object* v_r_575_; 
v_res_574_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_mayExecute(v_x_573_);
lean_dec_ref(v_x_573_);
v_r_575_ = lean_box(v_res_574_);
return v_r_575_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg(lean_object* v_x_576_){
_start:
{
if (lean_obj_tag(v_x_576_) == 0)
{
uint8_t v___x_577_; 
v___x_577_ = 0;
return v___x_577_;
}
else
{
lean_object* v_val_578_; uint8_t v_hasNetGrant_579_; 
v_val_578_ = lean_ctor_get(v_x_576_, 0);
v_hasNetGrant_579_ = lean_ctor_get_uint8(v_val_578_, 1);
return v_hasNetGrant_579_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg___boxed(lean_object* v_x_580_){
_start:
{
uint8_t v_res_581_; lean_object* v_r_582_; 
v_res_581_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg(v_x_580_);
lean_dec(v_x_580_);
v_r_582_ = lean_box(v_res_581_);
return v_r_582_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed(lean_object* v_x_583_, lean_object* v_x_584_){
_start:
{
uint8_t v___x_585_; 
v___x_585_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___redArg(v_x_583_);
return v___x_585_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed___boxed(lean_object* v_x_586_, lean_object* v_x_587_){
_start:
{
uint8_t v_res_588_; lean_object* v_r_589_; 
v_res_588_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_netAllowed(v_x_586_, v_x_587_);
lean_dec_ref(v_x_587_);
lean_dec(v_x_586_);
v_r_589_ = lean_box(v_res_588_);
return v_r_589_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg(lean_object* v_x_590_){
_start:
{
if (lean_obj_tag(v_x_590_) == 0)
{
uint8_t v___x_591_; 
v___x_591_ = 0;
return v___x_591_;
}
else
{
uint8_t v___x_592_; 
v___x_592_ = 1;
return v___x_592_;
}
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg___boxed(lean_object* v_x_593_){
_start:
{
uint8_t v_res_594_; lean_object* v_r_595_; 
v_res_594_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg(v_x_593_);
lean_dec(v_x_593_);
v_r_595_ = lean_box(v_res_594_);
return v_r_595_;
}
}
LEAN_EXPORT uint8_t lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed(lean_object* v_x_596_, lean_object* v_x_597_){
_start:
{
uint8_t v___x_598_; 
v___x_598_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___redArg(v_x_596_);
return v___x_598_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed___boxed(lean_object* v_x_599_, lean_object* v_x_600_){
_start:
{
uint8_t v_res_601_; lean_object* v_r_602_; 
v_res_601_ = lp_t863_x2dlean_Jevons_Writ_FailClosed_fsAllowed(v_x_599_, v_x_600_);
lean_dec_ref(v_x_600_);
lean_dec(v_x_599_);
v_r_602_ = lean_box(v_res_601_);
return v_r_602_;
}
}
LEAN_EXPORT lean_object* lp_t863_x2dlean_Jevons_Writ_FailClosed_asOption(lean_object* v_x_603_){
_start:
{
if (lean_obj_tag(v_x_603_) == 0)
{
lean_object* v_m_604_; lean_object* v___x_606_; uint8_t v_isShared_607_; uint8_t v_isSharedCheck_611_; 
v_m_604_ = lean_ctor_get(v_x_603_, 0);
v_isSharedCheck_611_ = !lean_is_exclusive(v_x_603_);
if (v_isSharedCheck_611_ == 0)
{
v___x_606_ = v_x_603_;
v_isShared_607_ = v_isSharedCheck_611_;
goto v_resetjp_605_;
}
else
{
lean_inc(v_m_604_);
lean_dec(v_x_603_);
v___x_606_ = lean_box(0);
v_isShared_607_ = v_isSharedCheck_611_;
goto v_resetjp_605_;
}
v_resetjp_605_:
{
lean_object* v___x_609_; 
if (v_isShared_607_ == 0)
{
lean_ctor_set_tag(v___x_606_, 1);
v___x_609_ = v___x_606_;
goto v_reusejp_608_;
}
else
{
lean_object* v_reuseFailAlloc_610_; 
v_reuseFailAlloc_610_ = lean_alloc_ctor(1, 1, 0);
lean_ctor_set(v_reuseFailAlloc_610_, 0, v_m_604_);
v___x_609_ = v_reuseFailAlloc_610_;
goto v_reusejp_608_;
}
v_reusejp_608_:
{
return v___x_609_;
}
}
}
else
{
lean_object* v___x_612_; 
lean_dec_ref_known(v_x_603_, 1);
v___x_612_ = lean_box(0);
return v___x_612_;
}
}
}
lean_object* initialize_Init(uint8_t builtin);
lean_object* initialize_Init(uint8_t builtin);
void lean_initialize_runtime_module();
static bool _G_initialized = false;
LEAN_EXPORT lean_object* initialize_t863_x2dlean_JevonsInvariant_FailClosed(uint8_t builtin) {
lean_object * res;
if (_G_initialized) return lean_io_result_mk_ok(lean_box(0));
_G_initialized = true;
lean_initialize_runtime_module();
res = initialize_Init(builtin);
if (lean_io_result_is_error(res)) return res;
lean_dec_ref(res);
res = initialize_Init(builtin);
if (lean_io_result_is_error(res)) return res;
lean_dec_ref(res);
return lean_io_result_mk_ok(lean_box(0));
}
#ifdef __cplusplus
}
#endif
