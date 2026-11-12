package keys

import (
	"bytes"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"

	"github.com/gath-org/gath/internal/consensus"
)

func TestDeriveSign(t *testing.T) {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	a, err := Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Derive(seed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.PublicKey(), b.PublicKey()) || a.PKH() != b.PKH() {
		t.Fatal("derivation is not deterministic")
	}
	c, err := Derive(seed, 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.PKH() == c.PKH() {
		t.Fatal("index does not change the key")
	}
	msg := []byte("gath")
	sig, err := a.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != mldsa44.SignatureSize || len(a.PublicKey()) != mldsa44.PublicKeySize {
		t.Fatalf("sizes sig %d pub %d", len(sig), len(a.PublicKey()))
	}
	var pk mldsa44.PublicKey
	if err := pk.UnmarshalBinary(a.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if !mldsa44.Verify(&pk, msg, []byte(consensus.SigContext), sig) {
		t.Fatal("signature did not verify")
	}
	sig[0] ^= 0xff
	if mldsa44.Verify(&pk, msg, []byte(consensus.SigContext), sig) {
		t.Fatal("tampered signature verified")
	}
	if _, err := Derive(seed[:32], 0); err == nil {
		t.Fatal("short seed accepted")
	}
}
