# Milestone 1

Gath is for someone who wants to hold and send money with no issuer on the other side of the transaction. The job of this milestone is to make that possible on one machine: run a node, create a wallet from a phrase, mine, and pay another wallet across more than one node.

## Invariant

Nobody can mint outside the subsidy schedule, freeze a coin, or change a rule with a key.

The checks that pin that down:

- The genesis block has no outputs.
- The sum of every block subsidy equals `MaxSupplyAtoms` (20,999,999.727 Gath). The 0.273 Gath gap from 21,000,000 is the atoms integer halving drops. The schedule is the cap.
- A coinbase that pays more than the subsidy plus fees is rejected.
- A bad ML-DSA signature is rejected.
- The JSON API has no mint, freeze, or admin method. Mining does real proof of work.

## What a person can do

Create or restore a wallet from the command line, read an address and a balance, send Gath, and run a node that validates blocks, keeps a UTXO set, exchanges blocks and transactions with peers, and reorgs to a heavier chain. The wallet talks only to this node's JSON API. Empty, loading, and error paths on the API are JSON errors with an HTTP status. A missing block or transaction is 404. A consensus rejection is 400.

## How it fails

A consensus bug can fork the people who run this code. A launch with almost no hashrate lets one machine pull ahead for a while; relative ASERT plus the median-time rule bounds that, and it does not mint a backlog of free blocks. A stolen wallet file still needs its passphrase, and a stolen phrase needs nothing else. An eclipse can hide the heaviest chain. A clock more than two hours off rejects honest blocks. A reorg deeper than the caps in the node is stored only when it is already heavier, and only up to 10,000 blocks. Forgetting the file passphrase does not lose the coins: the phrase restores the key. This code has not been audited.

## Decisions

| Topic | Choice |
| --- | --- |
| Consensus | Permissionless SHA3-256 proof of work. The header hash is the proof. Heaviest cumulative work wins. Equal work does not replace the tip. |
| Interval | ASERT aims at 45 seconds. The mainnet starting target is `0x1d200000` (2^229). On this class of machine SHA3-256 of the 88-byte header is about 3.5 million hashes per second per core, so the first block is expected in about 40 seconds on one core. Devnet uses `0x207fffff` so tests finish immediately. |
| Difficulty | Relative ASERT (aserti3-2d, integer cubic). The parent target and the parent interval (parent time minus grandparent time) are the inputs. The block above genesis uses the pow limit. Absolute ASERT anchored at genesis was rejected: a node started long after the genesis timestamp would otherwise mine a long stretch of minimum-difficulty blocks and take a large fraction of the supply. |
| Supply | 1 Gath = 10^8 atoms, uint64 only. Subsidy starts at 5 Gath and halves every 2,100,000 blocks (3 × 365 days at 45 seconds) by an integer right shift. Genesis pays nothing. There is no premine. |
| Ledger | UTXO. The transaction id is SHA3-256 of the witness-stripped transaction, so a signature cannot change the id. |
| Fees | At least 1 atom per serialized byte, witness included. Non-coinbase outputs are at least 4,000 atoms. Coinbase outputs may be smaller so a 1-atom tail subsidy can be claimed. The wallet uses the consensus minimum fee. |
| Keys | ML-DSA-44 (FIPS 204, Cloudflare circl), category 2, matching SHA3-256. Signatures are 2,420 bytes and public keys are 1,312 bytes. Signing is deterministic. The context string is `gath-v1`. |
| Address | Bech32m witness version 1. Human-readable part `gath` on mainnet and `gathd` on devnet. The payload is SHA3-256 of the public key. |
| Recovery | BIP39, 24 words by default, any valid length accepted. The BIP39 passphrase is empty, so the words alone restore the key. Index 0 is the only address in this milestone. The local file is Argon2id plus AES-256-GCM. That passphrase is not part of the backup. |
| Block | Version 1. Lock time 0. Sequence `0xffffffff`. Coinbase script starts with the block height as a little-endian uint32. Median time uses 11 ancestors. A timestamp may be at most two hours ahead of the node's clock. |
| Maturity | A coinbase can be spent at height `created + 100` or later. |
| Size | Blocks up to 4,000,000 bytes. A simple payment is about 4 KB, so a full block is on the order of a thousand payments. |
| Network | Magic `GATH` / `GATD`. Peers exchange version, addresses, inventories, blocks, and transactions. Mainnet listens for peers on all IPv4 interfaces. The API is localhost JSON and has no authentication. Generating blocks is mining, not an admin mint, and one request is capped at 500 blocks. |

Relative ASERT still allows a bounded fast-mine. A block every second tightens the target by about 0.24% (2^((1−45)/12960)). Median time and the two-hour future limit stop a miner from stamping arbitrary timestamps. That is a head start, not an open mint.

## Known gaps

- No security audit.
- One public mainnet bootnode is published (`104.207.90.38:9733`). It started at genesis and may have few or no peers. There is no DNS seed name. `--connect` still adds peers, and address gossip is unchanged.
- No headers-first initial download and no transaction orphan pool.
- No replace-by-fee. The mempool is first-seen, capped, and not written to disk.
- A deep side chain is validated before it is stored. Proof of work makes that expensive on mainnet. It is cheap on devnet.
- The RPC port is unauthenticated. It is meant for localhost.
- Reorgs deeper than 10,000 blocks are rejected. Side branches more than 64 blocks off the tip are stored only when they are already heavier.
- The wallet uses a single address index.
- Address history is a walk of the active chain, not an index. That is fine for devnet and short chains.
- Secret wiping is best-effort. ML-DSA key material inside the library is not guaranteed to be erased.
- This is not a launched public network. The integer cap is 20,999,999.727 Gath, not a round 21 million.

## Next

1. An external security review of the consensus and signature code before any value is at stake.
2. A public testnet with bootnodes and a fixed genesis, so more than one operator can run the same chain.
3. A light wallet that checks headers and its own transactions without storing the full UTXO set. The wallet in this milestone uses a full node.
