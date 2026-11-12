package consensus

// MerkleRoot is the SHA3-256 binary Merkle root of the transaction IDs.
// An odd level duplicates its last element, the same rule as Bitcoin.
// A single transaction is its own root. An empty list returns the zero hash,
// which no valid block uses because every block has a coinbase.
func MerkleRoot(ids []Hash) Hash {
	if len(ids) == 0 {
		return Hash{}
	}
	layer := make([]Hash, len(ids))
	copy(layer, ids)
	for len(layer) > 1 {
		if len(layer)%2 == 1 {
			layer = append(layer, layer[len(layer)-1])
		}
		next := make([]Hash, 0, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			var buf [64]byte
			copy(buf[:32], layer[i][:])
			copy(buf[32:], layer[i+1][:])
			next = append(next, SHA3(buf[:]))
		}
		layer = next
	}
	return layer[0]
}
