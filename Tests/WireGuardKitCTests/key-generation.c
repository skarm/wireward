/* SPDX-License-Identifier: MIT */
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "x25519.h"

static int calls;
static int fail_random;
/* Compile x25519.c with -DCCRandomGenerateBytes=wg_test_random. */
int wg_test_random(void *bytes, size_t count)
{
    ++calls;
    memset(bytes, 0xaa, count);
    return fail_random ? -4300 : 0;
}
#define CHECK(condition) do { if (!(condition)) { fprintf(stderr, "line %d: %s\n", __LINE__, #condition); return 1; } } while (0)
int main(void)
{
    uint8_t key[32] = {0};
    CHECK(curve25519_generate_private_key(key) == 0);
    CHECK(calls == 1); /* Must call RNG even with NDEBUG. */
    CHECK(key[0] == 0xa8 && key[31] == 0x6a);
    for (int i = 1; i < 31; ++i) CHECK(key[i] == 0xaa);
    fail_random = 1;
    CHECK(curve25519_generate_private_key(key) == -4300);
    CHECK(calls == 2);
    for (int i = 0; i < 32; ++i) CHECK(key[i] == 0);
    return 0;
}
