package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/gath-org/gath/internal/address"
	"github.com/gath-org/gath/internal/amount"
	"github.com/gath-org/gath/internal/client"
	"github.com/gath-org/gath/internal/consensus"
	"github.com/gath-org/gath/internal/miner"
	"github.com/gath-org/gath/internal/node"
	"github.com/gath-org/gath/internal/version"
	"github.com/gath-org/gath/internal/wallet"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "node":
		err = cmdNode(os.Args[2:])
	case "miner":
		err = cmdMiner(os.Args[2:])
	case "wallet":
		err = cmdWallet(os.Args[2:])
	case "version":
		fmt.Printf("gath %s protocol %d\n", version.Software, version.Protocol)
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gath: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Gath is a standalone proof-of-work currency.

Usage:
  gath node --network devnet --datadir ./data
  gath miner --rpc http://127.0.0.1:19734 --address <addr> --blocks 1
  gath wallet create --network devnet --file wallet.json
  gath wallet restore --network devnet --file wallet.json --phrase "<words>"
  gath wallet address --file wallet.json
  gath wallet balance --file wallet.json --rpc http://127.0.0.1:19734
  gath wallet send --file wallet.json --rpc http://127.0.0.1:19734 --to <addr> --amount 1
  gath version

The wallet file passphrase is --passphrase, or GATH_WALLET_PASSPHRASE, or a prompt.
The recovery phrase alone restores the wallet. The file passphrase does not.
`)
}

type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ",") }

func (s *stringsFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func cmdNode(args []string) error {
	fs := flag.NewFlagSet("node", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	network := fs.String("network", "mainnet", "mainnet or devnet")
	datadir := fs.String("datadir", "", "directory for the chain database")
	listen := fs.String("listen", "", "peer listen address (default 127.0.0.1 and the network port)")
	rpc := fs.String("rpc", "", "JSON API listen address (default 127.0.0.1 and the network port)")
	external := fs.String("external", "", "address to advertise to peers")
	mine := fs.Bool("mine", false, "mine on this node")
	mineTo := fs.String("mine-to", "", "address that receives this node's coinbase")
	threads := fs.Int("threads", runtime.NumCPU(), "miner threads, used with --mine")
	var connect stringsFlag
	fs.Var(&connect, "connect", "peer address to keep dialing (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *datadir == "" {
		return fmt.Errorf("--datadir is required")
	}
	params := consensus.ByNetwork(mustNet(*network))
	if *listen == "" {
		*listen = fmt.Sprintf("127.0.0.1:%d", params.P2PPort)
	}
	if *rpc == "" {
		*rpc = fmt.Sprintf("127.0.0.1:%d", params.RPCPort)
	}
	n, err := node.Start(context.Background(), node.Config{
		Network:  *network,
		DataDir:  *datadir,
		Listen:   *listen,
		RPC:      *rpc,
		External: *external,
		Connect:  connect,
		Mine:     *mine,
		MineTo:   *mineTo,
		Threads:  *threads,
	}, nil)
	if err != nil {
		return err
	}
	defer n.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	return nil
}

func mustNet(name string) consensus.Network {
	switch name {
	case "devnet":
		return consensus.Devnet
	default:
		return consensus.Mainnet
	}
}

func cmdMiner(args []string) error {
	fs := flag.NewFlagSet("miner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rpc := fs.String("rpc", "http://127.0.0.1:9734", "node JSON API")
	addr := fs.String("address", "", "coinbase address")
	blocks := fs.Int("blocks", 1, "number of blocks to mine")
	threads := fs.Int("threads", runtime.NumCPU(), "hashing threads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *addr == "" {
		return fmt.Errorf("--address is required")
	}
	if *blocks < 1 || *blocks > 500 {
		return fmt.Errorf("--blocks must be from 1 to 500")
	}
	c := client.New(*rpc)
	ctx := context.Background()
	for i := 0; i < *blocks; i++ {
		block, err := c.Template(ctx, *addr)
		if err != nil {
			return err
		}
		if err := miner.Solve(ctx, block, *threads); err != nil {
			return err
		}
		if err := c.SubmitBlock(ctx, block); err != nil {
			return err
		}
		fmt.Printf("%s\n", block.Hash())
	}
	return nil
}

func cmdWallet(args []string) error {
	if len(args) < 1 {
		usage()
		return fmt.Errorf("wallet command is required")
	}
	switch args[0] {
	case "create":
		return walletCreate(args[1:])
	case "restore":
		return walletRestore(args[1:])
	case "address":
		return walletAddress(args[1:])
	case "balance":
		return walletBalance(args[1:])
	case "send":
		return walletSend(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown wallet command %q", args[0])
	}
}

func walletFlags(name string) (*flag.FlagSet, *string, *string, *string, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "wallet.json", "wallet file")
	pass := fs.String("passphrase", "", "file passphrase (or GATH_WALLET_PASSPHRASE)")
	network := fs.String("network", "mainnet", "mainnet or devnet")
	asJSON := fs.Bool("json", false, "print JSON")
	return fs, file, pass, network, asJSON
}

func walletCreate(args []string) error {
	fs, file, pass, network, asJSON := walletFlags("create")
	if err := fs.Parse(args); err != nil {
		return err
	}
	phrasePass, err := passphrase(*pass)
	if err != nil {
		return err
	}
	words, w, err := wallet.Create(*file, *network, phrasePass)
	if err != nil {
		return err
	}
	addr, err := w.Address()
	if err != nil {
		return err
	}
	return printWallet(*asJSON, map[string]string{
		"phrase":  words,
		"address": addr,
		"file":    *file,
		"network": w.Network(),
	}, fmt.Sprintf("address %s\nfile %s\nphrase %s\n", addr, *file, words))
}

func walletRestore(args []string) error {
	fs, file, pass, network, asJSON := walletFlags("restore")
	words := fs.String("phrase", "", "recovery phrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *words == "" {
		return fmt.Errorf("--phrase is required")
	}
	phrasePass, err := passphrase(*pass)
	if err != nil {
		return err
	}
	w, err := wallet.Restore(*file, *network, *words, phrasePass)
	if err != nil {
		return err
	}
	addr, err := w.Address()
	if err != nil {
		return err
	}
	return printWallet(*asJSON, map[string]string{
		"address": addr,
		"file":    *file,
		"network": w.Network(),
	}, fmt.Sprintf("address %s\nfile %s\n", addr, *file))
}

func walletAddress(args []string) error {
	fs, file, pass, _, asJSON := walletFlags("address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	w, err := loadWallet(*file, *pass)
	if err != nil {
		return err
	}
	addr, err := w.Address()
	if err != nil {
		return err
	}
	return printWallet(*asJSON, map[string]string{
		"address": addr,
		"network": w.Network(),
	}, addr+"\n")
}

func walletBalance(args []string) error {
	fs, file, pass, _, asJSON := walletFlags("balance")
	rpc := fs.String("rpc", "", "node JSON API")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rpc == "" {
		return fmt.Errorf("--rpc is required")
	}
	w, err := loadWallet(*file, *pass)
	if err != nil {
		return err
	}
	addr, err := w.Address()
	if err != nil {
		return err
	}
	c := client.New(*rpc)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if st.Network != w.Network() {
		return fmt.Errorf("wallet is %s and the node is %s", w.Network(), st.Network)
	}
	info, err := c.Address(ctx, addr)
	if err != nil {
		return err
	}
	return printWallet(*asJSON, map[string]any{
		"address":         addr,
		"network":         w.Network(),
		"spendable":       info.Spendable,
		"spendable_atoms": info.SpendableAtoms,
		"confirmed":       info.Confirmed,
		"confirmed_atoms": info.ConfirmedAtoms,
		"height":          st.Height,
	}, fmt.Sprintf("spendable %s\nconfirmed %s\n", info.Spendable, info.Confirmed))
}

func walletSend(args []string) error {
	fs, file, pass, _, asJSON := walletFlags("send")
	rpc := fs.String("rpc", "", "node JSON API")
	to := fs.String("to", "", "destination address")
	amt := fs.String("amount", "", "amount in Gath")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rpc == "" || *to == "" || *amt == "" {
		return fmt.Errorf("--rpc, --to, and --amount are required")
	}
	atoms, err := amount.Parse(*amt)
	if err != nil {
		return err
	}
	w, err := loadWallet(*file, *pass)
	if err != nil {
		return err
	}
	c := client.New(*rpc)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if st.Network != w.Network() {
		return fmt.Errorf("wallet is %s and the node is %s", w.Network(), st.Network)
	}
	params := consensus.ByNetwork(mustNet(w.Network()))
	hrp, payTo, err := address.Decode(*to)
	if err != nil {
		return err
	}
	if hrp != params.HRP {
		return fmt.Errorf("destination address is for %q", hrp)
	}
	from, err := w.Address()
	if err != nil {
		return err
	}
	info, err := c.Address(ctx, from)
	if err != nil {
		return err
	}
	rate, err := c.FeeRate(ctx)
	if err != nil {
		return err
	}
	var coins []wallet.Coin
	for _, u := range info.UTXOs {
		if !u.Mature || u.Reserved {
			continue
		}
		value, err := parseAtoms(u.Atoms)
		if err != nil {
			return err
		}
		id, err := consensus.ParseHash(u.TxID)
		if err != nil {
			return err
		}
		coins = append(coins, wallet.Coin{
			Outpoint: consensus.Outpoint{Hash: id, Index: u.Index},
			Value:    value,
			Height:   u.Height,
			Coinbase: u.Coinbase,
		})
	}
	tx, err := w.BuildPayment(payTo, atoms, rate, st.Height, coins)
	if err != nil {
		return err
	}
	txid, err := c.SubmitTx(ctx, tx)
	if err != nil {
		return err
	}
	return printWallet(*asJSON, map[string]string{
		"txid":   txid,
		"amount": amount.Format(atoms),
		"to":     *to,
	}, fmt.Sprintf("txid %s\n", txid))
}

func loadWallet(file, passFlag string) (*wallet.Wallet, error) {
	pass, err := passphrase(passFlag)
	if err != nil {
		return nil, err
	}
	return wallet.Load(file, pass)
}

func passphrase(flagValue string) (string, error) {
	pass := flagValue
	if pass == "" {
		pass = os.Getenv("GATH_WALLET_PASSPHRASE")
	}
	if pass == "" {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return "", fmt.Errorf("set --passphrase or GATH_WALLET_PASSPHRASE")
		}
		fmt.Fprint(os.Stderr, "Wallet passphrase: ")
		raw, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		pass = string(raw)
	}
	if len(pass) < 8 {
		return "", fmt.Errorf("passphrase must be at least 8 characters")
	}
	return pass, nil
}

func parseAtoms(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("atoms: %w", err)
	}
	return n, nil
}

func printWallet(asJSON bool, v any, text string) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(v)
	}
	fmt.Fprint(os.Stdout, text)
	return nil
}
