// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

#ifndef WIREGUARD_KIT_C_H
#define WIREGUARD_KIT_C_H

#include <stdint.h>
#include <stddef.h>

#include "key.h"
#include "x25519.h"

/* From <sys/kern_control.h> */
#define WG_CTLIOCGINFO 0xc0644e03UL
struct wg_ctl_info {
    uint32_t   ctl_id;
    char        ctl_name[96];
};
struct wg_sockaddr_ctl {
    uint8_t      sc_len;
    uint8_t      sc_family;
    uint16_t   ss_sysaddr;
    uint32_t   sc_id;
    uint32_t   sc_unit;
    uint32_t   sc_reserved[5];
};

/* Darwin kernel ABI, shared by all supported Apple architectures. */
_Static_assert(sizeof(struct wg_ctl_info) == 100, "wg_ctl_info ABI size");
_Static_assert(offsetof(struct wg_ctl_info, ctl_name) == 4, "wg_ctl_info ABI offset");
_Static_assert(sizeof(struct wg_sockaddr_ctl) == 32, "wg_sockaddr_ctl ABI size");
_Static_assert(_Alignof(struct wg_sockaddr_ctl) == 4, "wg_sockaddr_ctl ABI alignment");
_Static_assert(offsetof(struct wg_sockaddr_ctl, sc_id) == 4, "wg_sockaddr_ctl ABI offset");
_Static_assert(offsetof(struct wg_sockaddr_ctl, sc_reserved) == 12, "wg_sockaddr_ctl ABI offset");

#endif
