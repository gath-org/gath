package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/argon2"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/keys"
)

const (
	fileVersion = 1
	kdfName     = "argon2id"
	argonTime   = 1
	argonMemory = 32 * 1024
	argonThread = 1
	plainMagic  = "GATHSEED"
)

// Wallet is the index-0 key for one network. The 24-word phrase is the only
// secret required to restore it. The file passphrase encrypts the local copy
// and is not part of the backup.
type Wallet struct {
	network string
	hrp     string
	genesis consensus.Hash
	key     *keys.Key
	seed    []byte
}

// Create makes a new 24-word wallet, writes an encrypted file, and returns
// the phrase once. The phrase is not stored in the file.
func Create(path, network, passphrase string) (phrase string, w *Wallet, err error) {
	entropy := make([]byte, 32)
	if _, err = rand.Read(entropy); err != nil {
		return "", nil, err
	}
	phrase, err = bip39.NewMnemonic(entropy)
	if err != nil {
		return "", nil, err
	}
	w, err = fromPhrase(phrase)
	if err != nil {
		return "", nil, err
	}
	if err = w.finish(network); err != nil {
		return "", nil, err
	}
	if err = w.write(path, passphrase); err != nil {
		return "", nil, err
	}
	return phrase, w, nil
}

// Restore rebuilds a wallet from a BIP39 phrase and writes an encrypted file.
// Any valid BIP39 length is accepted. The BIP39 passphrase is empty: the
// words alone are the backup.
func Restore(path, network, phrase, passphrase string) (*Wallet, error) {
	w, err := fromPhrase(phrase)
	if err != nil {
		return nil, err
	}
	if err := w.finish(network); err != nil {
		return nil, err
	}
	if err := w.write(path, passphrase); err != nil {
		return nil, err
	}
	return w, nil
}

// Load decrypts a wallet file.
func Load(path, passphrase string) (*Wallet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f stored
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("wallet file: %w", err)
	}
	if f.Version != fileVersion || f.KDF != kdfName {
		return nil, errors.New("unsupported wallet file")
	}
	seed, err := f.decrypt(passphrase)
	if err != nil {
		return nil, err
	}
	key, err := keys.Derive(seed, 0)
	if err != nil {
		return nil, err
	}
	w := &Wallet{network: f.Network, seed: seed, key: key}
	if err := w.finish(f.Network); err != nil {
		return nil, err
	}
	return w, nil
}

func fromPhrase(phrase string) (*Wallet, error) {
	if !bip39.IsMnemonicValid(phrase) {
		return nil, errors.New("invalid recovery phrase")
	}
	seed, err := bip39.NewSeedWithErrorChecking(phrase, "")
	if err != nil {
		return nil, err
	}
	key, err := keys.Derive(seed, 0)
	if err != nil {
		return nil, err
	}
	return &Wallet{seed: seed, key: key}, nil
}

func (w *Wallet) finish(network string) error {
	var net consensus.Network
	switch network {
	case "mainnet":
		net = consensus.Mainnet
	case "devnet":
		net = consensus.Devnet
	default:
		return fmt.Errorf("unknown network %q", network)
	}
	p := consensus.ByNetwork(net)
	w.network = p.Name
	w.hrp = p.HRP
	w.genesis = p.GenesisHash()
	return nil
}

// Network is "mainnet" or "devnet".
func (w *Wallet) Network() string { return w.network }

// Address is the bech32m address of index 0.
func (w *Wallet) Address() (string, error) {
	return address.Encode(w.hrp, w.key.PKH())
}

// PKH is the public key hash paid by a transaction output.
func (w *Wallet) PKH() consensus.Hash { return w.key.PKH() }

// Coin is one confirmed output the wallet might spend.
type Coin struct {
	Outpoint consensus.Outpoint
	Value    uint64
	Height   uint64
	Coinbase bool
}

// BuildPayment selects coins and signs a payment of amount atoms to payTo.
// feeRate is atoms per serialized byte. tipHeight is the current chain tip.
// The transaction spends in the next block, so maturity is judged at tipHeight+1.
// Coins should already exclude outputs reserved by the mempool.
func (w *Wallet) BuildPayment(payTo consensus.Hash, amount, feeRate, tipHeight uint64, coins []Coin) (*consensus.Tx, error) {
	if amount < consensus.MinOutputAtoms {
		return nil, errors.New("amount is below the dust minimum")
	}
	if feeRate < consensus.MinFeePerByte {
		return nil, errors.New("fee rate is below the consensus minimum")
	}
	spendHeight := tipHeight + 1
	var usable []Coin
	for _, c := range coins {
		if c.Coinbase && (spendHeight < c.Height || spendHeight-c.Height < consensus.CoinbaseMaturity) {
			continue
		}
		usable = append(usable, c)
	}
	sortCoins(usable)
	var picked []Coin
	var sum uint64
	for _, c := range usable {
		picked = append(picked, c)
		if sum > ^uint64(0)-c.Value {
			return nil, errors.New("input value overflow")
		}
		sum += c.Value
		tx, fee, ok, err := w.assemble(payTo, amount, feeRate, sum, picked)
		if err != nil {
			return nil, err
		}
		if ok {
			_ = fee
			return tx, nil
		}
	}
	return nil, errors.New("insufficient funds")
}

func (w *Wallet) assemble(payTo consensus.Hash, amount, feeRate, sum uint64, picked []Coin) (*consensus.Tx, uint64, bool, error) {
	for _, outputs := range []int{2, 1} {
		fee := uint64(txSize(len(picked), outputs)) * feeRate
		if sum < amount || sum-amount < fee {
			continue
		}
		change := sum - amount - fee
		if outputs == 2 && change < consensus.MinOutputAtoms {
			continue
		}
		if outputs == 1 && change != 0 {
			// The leftover is smaller than a change output. It becomes fee.
			fee += change
			change = 0
		}
		tx := &consensus.Tx{
			Version:  consensus.TxVersion,
			LockTime: consensus.LockTimeMust,
		}
		for _, c := range picked {
			tx.Inputs = append(tx.Inputs, consensus.TxIn{
				Prev:     c.Outpoint,
				Sequence: consensus.SequenceMust,
			})
		}
		tx.Outputs = append(tx.Outputs, consensus.TxOut{Value: amount, PKH: payTo})
		if change > 0 {
			tx.Outputs = append(tx.Outputs, consensus.TxOut{Value: change, PKH: w.key.PKH()})
		}
		pub := w.key.PublicKey()
		for i := range tx.Inputs {
			msg := consensus.Sighash(tx, i, picked[i].Value, w.key.PKH(), w.genesis)
			sig, err := w.key.Sign(msg[:])
			if err != nil {
				return nil, 0, false, err
			}
			tx.Inputs[i].PubKey = pub
			tx.Inputs[i].Signature = sig
		}
		if got := uint64(len(tx.Bytes())) * feeRate; fee < got {
			return nil, 0, false, errors.New("fee does not cover the signed transaction")
		}
		return tx, fee, true, nil
	}
	return nil, 0, false, nil
}

func txSize(inputs, outputs int) int {
	tx := &consensus.Tx{Version: consensus.TxVersion, LockTime: consensus.LockTimeMust}
	pub := make([]byte, mldsa44.PublicKeySize)
	sig := make([]byte, mldsa44.SignatureSize)
	for i := 0; i < inputs; i++ {
		tx.Inputs = append(tx.Inputs, consensus.TxIn{
			Sequence:  consensus.SequenceMust,
			PubKey:    pub,
			Signature: sig,
		})
	}
	for i := 0; i < outputs; i++ {
		tx.Outputs = append(tx.Outputs, consensus.TxOut{Value: 1, PKH: consensus.Hash{1}})
	}
	return len(tx.Bytes())
}

func sortCoins(coins []Coin) {
	for i := 1; i < len(coins); i++ {
		j := i
		for j > 0 && lessCoin(coins[j], coins[j-1]) {
			coins[j], coins[j-1] = coins[j-1], coins[j]
			j--
		}
	}
}

func lessCoin(a, b Coin) bool {
	if a.Value != b.Value {
		return a.Value > b.Value
	}
	if a.Outpoint.Hash != b.Outpoint.Hash {
		return a.Outpoint.Hash.String() < b.Outpoint.Hash.String()
	}
	return a.Outpoint.Index < b.Outpoint.Index
}

func (w *Wallet) write(path, passphrase string) error {
	if len(passphrase) < 8 {
		return errors.New("passphrase must be at least 8 characters")
	}
	f, err := encrypt(w.network, w.seed, passphrase)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

type stored struct {
	Version    int    `json:"version"`
	Network    string `json:"network"`
	KDF        string `json:"kdf"`
	Time       uint32 `json:"time"`
	Memory     uint32 `json:"memory"`
	Threads    uint8  `json:"threads"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func encrypt(network string, seed []byte, passphrase string) (*stored, error) {
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThread, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain := append([]byte(plainMagic), seed...)
	ct := gcm.Seal(nil, nonce, plain, []byte(network))
	for i := range plain {
		plain[i] = 0
	}
	for i := range key {
		key[i] = 0
	}
	return &stored{
		Version:    fileVersion,
		Network:    network,
		KDF:        kdfName,
		Time:       argonTime,
		Memory:     argonMemory,
		Threads:    argonThread,
		Salt:       hex.EncodeToString(salt),
		Nonce:      hex.EncodeToString(nonce),
		Ciphertext: hex.EncodeToString(ct),
	}, nil
}

func (f *stored) decrypt(passphrase string) ([]byte, error) {
	salt, err := hex.DecodeString(f.Salt)
	if err != nil {
		return nil, err
	}
	nonce, err := hex.DecodeString(f.Nonce)
	if err != nil {
		return nil, err
	}
	ct, err := hex.DecodeString(f.Ciphertext)
	if err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(passphrase), salt, f.Time, f.Memory, f.Threads, 32)
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, []byte(f.Network))
	if err != nil {
		return nil, errors.New("wrong passphrase or damaged wallet file")
	}
	if len(plain) != len(plainMagic)+64 || string(plain[:len(plainMagic)]) != plainMagic {
		return nil, errors.New("damaged wallet file")
	}
	seed := append([]byte(nil), plain[len(plainMagic):]...)
	for i := range plain {
		plain[i] = 0
	}
	return seed, nil
}
