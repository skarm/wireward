/* SPDX-License-Identifier: MIT
 * Copyright (C) 2018-2023 WireGuard LLC. All Rights Reserved.
 */
#ifndef WIREGUARD_H
#define WIREGUARD_H

#include <stdint.h>

#define WG_APPLE_ABI_VERSION 2

#ifdef __cplusplus
extern "C" {
#endif

/* All inputs are borrowed for the duration of the call. Settings must be valid
 * NUL-terminated UTF-8 UAPI text without embedded NULs. NULL settings are rejected.
 * Handles and operations are thread-safe; operations on one handle are serialized.
 * A successful start returns a nonnegative handle; other operations return 0.
 * Errors are negative Darwin errno values (including WireGuard IPC errors).
 * Closed/stale handles return -EBADF and are never reused in this process.
 * The caller owns tun_fd throughout; the backend owns a duplicate, closed on
 * every failure or stop. Rebuild all consumers when changing this ABI.
 */

typedef void (*logger_fn_t)(void * _Nullable context, int32_t level, const char * _Nonnull message);
/* Callbacks may arrive concurrently on arbitrary threads. message is borrowed
 * until callback return. The callback MUST NOT synchronously call any wg* API:
 * it may run while backend/logger locks are held. wgSetLogger waits for old
 * callbacks to finish, so their context can be released after this call returns.
 * Passing NULL disables logging. The caller retains context until replacement.
 */
void wgSetLogger(void * _Nullable context, logger_fn_t _Nullable logger_fn);
int32_t wgTurnOn(const char * _Nullable settings, int32_t tun_fd);
int64_t wgTurnOff(int32_t handle);
/* An unsuccessful IPC update may have partially applied; stop the tunnel. */
int64_t wgSetConfig(int32_t handle, const char * _Nullable settings);
/* Sets *settings to NULL on failure. On success the caller owns the string,
 * which includes private key material. Release with wgFreeString. */
int64_t wgGetConfig(int32_t handle, char * _Nullable * _Nullable settings);
/* Success means a retry worker was accepted/coalesced, not connectivity restored.
 * At most one worker per handle retries ten times, 500ms apart. Stop cancels and
 * joins it; final asynchronous failure is reported through the logger. */
int64_t wgBumpSockets(int32_t handle);
int64_t wgDisableSomeRoamingForBrokenMobileSemantics(int32_t handle);
/* Returned version is owned by the caller. */
char * _Nonnull wgVersion(void);
/* Accepts NULL. Each owned result must be released exactly once. */
void wgFreeString(char * _Nullable value);

#ifdef __cplusplus
}
#endif
#endif
