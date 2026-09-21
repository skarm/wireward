#ifndef X25519_H
#define X25519_H

#include <stdint.h>

void curve25519_derive_public_key(unsigned char public_key[32], const unsigned char private_key[32]);
/* Returns 0 on success or the CommonCrypto RNG error. Clears output on failure. */
int32_t curve25519_generate_private_key(unsigned char private_key[32]);

#endif
