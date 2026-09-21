/* SPDX-License-Identifier: MIT */
#include <sys/kern_control.h>
#include <sys/ioctl.h>
#include "WireGuardKitC.h"

_Static_assert(WG_CTLIOCGINFO == CTLIOCGINFO, "ioctl command differs from SDK");
_Static_assert(sizeof(struct wg_ctl_info) == sizeof(struct ctl_info), "ctl_info size differs from SDK");
_Static_assert(_Alignof(struct wg_ctl_info) == _Alignof(struct ctl_info), "ctl_info alignment differs from SDK");
_Static_assert(offsetof(struct wg_ctl_info, ctl_name) == offsetof(struct ctl_info, ctl_name), "ctl_name offset differs from SDK");
_Static_assert(sizeof(struct wg_sockaddr_ctl) == sizeof(struct sockaddr_ctl), "sockaddr_ctl size differs from SDK");
_Static_assert(_Alignof(struct wg_sockaddr_ctl) == _Alignof(struct sockaddr_ctl), "sockaddr_ctl alignment differs from SDK");
#define CHECK_OFFSET(field) _Static_assert(offsetof(struct wg_sockaddr_ctl, field) == offsetof(struct sockaddr_ctl, field), "SDK offset: " #field)
CHECK_OFFSET(sc_len);
CHECK_OFFSET(sc_family);
CHECK_OFFSET(ss_sysaddr);
CHECK_OFFSET(sc_id);
CHECK_OFFSET(sc_unit);
CHECK_OFFSET(sc_reserved);
int main(void) { return 0; }
