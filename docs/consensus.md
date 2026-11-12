# Gath consensus

This is the milestone 1 rule set. A node that applies these rules to the same blocks reaches the same UTXO set. Policy that is not in this document (mempool expiry, peer limits, the RPC API) can differ between implementations.

Integers are unsigned and little-endian. Hashes are 32 bytes. Display order is the natural big-endian hex of the SHA3-256 digest. The proof-of-work integer is that same byte string interpreted as a big-endian integer.

## Units

One Gath is 100,000,000 atoms. Values are uint64. A value that would overflow uint64 is invalid.

## Block header

88 bytes:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 4 | version, must be 1 |
| 4 | 32 | previous block hash |
| 36 | 32 | merkle root |
| 68 | 8 | timestamp, unix seconds |
| 76 | 4 | compact target |
| 80 | 8 | nonce |

The proof-of-work hash is SHA3-256 of these 88 bytes. It must be less than or equal to the target. The nonce is last so a miner can rewrite the final 8 bytes in place.

## Compact target

The compact encoding is Bitcoin's nBits. The high bit of the 24-bit mantissa is a sign bit. Negative encodings and the Bitcoin overflow cases are invalid. A zero target is invalid.

Mainnet's easiest target is `0x1d200000`, which is 2^229. Devnet's is `0x207fffff`. A target above the network pow limit is invalid.

Work for one block is `floor((2^256 − target − 1) / (target + 1))`. Cumulative work is the sum along the branch. The heavier sum is the active chain. An equal sum does not replace the tip.

## Merkle root

The merkle root is a binary SHA3-256 tree of the transaction ids, in block order. An odd level duplicates its last element. One transaction is its own root.

## Time

The median time past is the median of up to 11 ancestor timestamps, including the parent. Sort them ascending and take index `len/2`. The block timestamp must be strictly greater than that median. If the node is checking the future limit, the timestamp must be at most two hours ahead of the node's clock.

## Difficulty

The genesis block and the block directly above it use the network pow limit.

Later blocks use relative ASERT. Let the parent interval be `parent.time − grandparent.time` (it may be negative). The next target is

```
parentTarget * 2^((interval − 45) / 12960)
```

evaluated with the integer aserti3-2d cubic approximation:

```
exponent = ((interval − spacing) * 65536) / halfLife
```

Division truncates toward zero. `shifts` is the arithmetic right shift of `exponent` by 16. `frac` is the low 16 bits. With `f = frac`,

```
factor = 65536 + ((195766423245049*f + 971821376*f^2 + 5127*f^3 + 2^47) >> 48)
next = parentTarget * factor
```

then shift `next` by `shifts − 16` (right if negative, left if positive). Clamp to `[1, powLimit]`. An exponent whose absolute shift exceeds 512 clamps to the same boundary. The time delta fed to the exponent is rejected outside ±2^46 seconds.

An on-schedule interval of 45 seconds leaves the parent target unchanged. One half-life early halves it. One half-life late doubles it, until the pow limit.

The half-life is 12,960 seconds, which is 288 blocks of 45 seconds.

## Subsidy

Height 0 pays 0. For height `h > 0`,

```
era = (h − 1) / 2_100_000
subsidy = 500_000_000 >> era
```

using an integer shift. Era 0 through era 28 pay a non-zero subsidy. Height 60,900,001 and later pay 0. The sum of every subsidy is 2,099,999,972,700,000 atoms.

## Transactions

A transaction is version 1 and lock time 0. Each input sequence is `0xffffffff`. Inputs and outputs are each limited to 1,024. A transaction is at most 100,000 bytes. A block is at most 4,000,000 bytes including the header, and at most 50,000 transactions.

The coinbase is the first transaction. It has one input whose previous hash is 32 zero bytes and whose previous index is `0xffffffff`. Its script is between 4 and 100 bytes. The first 4 bytes are the block height as a little-endian uint32 and must equal the height of the block. The coinbase witness is empty. A coinbase may have zero outputs, which claims nothing. A non-coinbase transaction has at least one output.

Every output value is non-zero. A non-coinbase output is at least 4,000 atoms. A coinbase output may be smaller.

A non-coinbase input has an empty script, a 1,312-byte ML-DSA-44 public key, and a 2,420-byte signature. The SHA3-256 of the public key must equal the public-key hash of the output it spends. No input may repeat an outpoint.

The transaction id is SHA3-256 of the witness-stripped encoding: version, inputs (previous hash, index, script, sequence), outputs (value, 32-byte public-key hash), lock time. The public key and signature are not part of the id.

The full encoding inserts the public key and the signature, each as a length-prefixed byte string, after the sequence of each input.

Variable-length integers are Bitcoin's compact size. A non-minimal encoding is invalid. Length-prefixed strings use that integer. A decoder rejects trailing bytes.

## Signature

The signed message is SHA3-256 of:

- genesis hash
- version and lock time
- SHA3-256 of every previous outpoint, in order
- SHA3-256 of every sequence, in order
- SHA3-256 of every output, in order
- input index as uint32
- value of the spent output as uint64
- public-key hash of the spent output
- sequence of this input

ML-DSA-44 verifies that message with the FIPS 204 context string `gath-v1`. Signing in this implementation is deterministic.

## Connecting a block

Transactions after the coinbase are applied in order. A transaction may spend an output created earlier in the same block. Coinbase outputs are created last, so they cannot be spent in their own block.

A coinbase output created at height `h` can be spent in a block at height `h + 100` or later. Spending it earlier is invalid.

The fee of a non-coinbase transaction is the sum of its input values minus the sum of its output values. Outputs must not exceed inputs. The fee must be at least 1 atom per byte of the full serialization, witness included.

The sum of the coinbase outputs must be less than or equal to the subsidy plus the fees of the other transactions in the block. A greater sum is invalid. Unclaimed subsidy is not added to a later block.

The same transaction id must not already exist in the active chain.

## Genesis

Genesis time is 2026-11-10 00:00:00 UTC (unix 1794268800). The genesis coinbase pays nothing. Its script is a height of 0 followed by a network tag. Mainnet and devnet use different tags, so their genesis hashes differ. The genesis nonce is part of the parameter set and the header meets the network pow limit.

## Chain selection

A block whose parent is unknown is not accepted (the node may remember it as an orphan; that memory is not consensus). A stored block is not applied twice.

To apply a block that is not on the active tip, undo the active chain back to the common ancestor, replay the side branch with the undo data that was stored with those blocks, and connect the new block. The undo data records the outputs each non-coinbase input spent. Replaying checks that those outputs are the ones still present. It does not check signatures again. The original connection checked them.

If the new cumulative work is strictly greater, that branch becomes the tip and the UTXO set, the transaction index, and the height index are updated in one database transaction. Otherwise the block and its undo are stored and the tip stays.

## Addresses

An address is bech32m (BIP 350), witness version 1. The human-readable part is `gath` on mainnet and `gathd` on devnet. The data is the 32-byte SHA3-256 of an ML-DSA-44 public key. Mixed case and a bech32 (not bech32m) checksum are invalid.

The key itself is not recovered from the address. The wallet derives ML-DSA-44 seed material as

```
SHAKE256("gath-mldsa44-v1" || 64-byte BIP39 seed || index as uint32 little-endian)
```

taking the first 32 bytes. The BIP39 passphrase is empty. Index 0 is the address this milestone uses. Derivation is not a consensus rule. Spending is valid for any public key that hashes to the output being spent.
