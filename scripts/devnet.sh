#!/usr/bin/env bash
# Start three devnet nodes and send Gath both ways between two wallets.
set -euo pipefail
cd "$(dirname "$0")/.."

export GATH_WALLET_PASSPHRASE="${GATH_WALLET_PASSPHRASE:-devnet-passphrase}"
if [[ ${#GATH_WALLET_PASSPHRASE} -lt 8 ]]; then
  echo "GATH_WALLET_PASSPHRASE must be at least 8 characters" >&2
  exit 1
fi

BIN="${BIN:-$PWD/bin/gath}"
DATA="$PWD/devnet-data"
rm -rf "$DATA"
mkdir -p "$DATA"
go build -o "$BIN" ./cmd/gath

"$BIN" wallet create --network devnet --file "$DATA/a.json" --json > "$DATA/a.create.json"
"$BIN" wallet create --network devnet --file "$DATA/b.json" --json > "$DATA/b.create.json"

A=$(python3 -c 'import json; print(json.load(open("devnet-data/a.create.json"))["address"])')
B=$(python3 -c 'import json; print(json.load(open("devnet-data/b.create.json"))["address"])')
echo "wallet A $A"
echo "wallet B $B"

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT

start_node() {
  local name=$1 listen=$2 rpc=$3
  shift 3
  "$BIN" node --network devnet --datadir "$DATA/$name" \
    --listen "$listen" --rpc "$rpc" --external "$listen" \
    "$@" > "$DATA/$name.log" 2>&1 &
  pids+=("$!")
}

start_node n1 127.0.0.1:19733 127.0.0.1:19734 --connect 127.0.0.1:19743 --connect 127.0.0.1:19753
start_node n2 127.0.0.1:19743 127.0.0.1:19744 --connect 127.0.0.1:19733 --connect 127.0.0.1:19753
start_node n3 127.0.0.1:19753 127.0.0.1:19754 --connect 127.0.0.1:19733 --connect 127.0.0.1:19743

python3 - "$A" "$B" <<'PY'
import json, sys, time, urllib.request
A, B = sys.argv[1], sys.argv[2]
N1, N2, N3 = "http://127.0.0.1:19734", "http://127.0.0.1:19744", "http://127.0.0.1:19754"

def call(method, url, body=None, timeout=180):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)

def get(url):
    return call("GET", url)

def post(url, body):
    return call("POST", url, body)

def wait(label, fn, seconds=30):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            if fn():
                return
        except Exception as exc:
            last = exc
        time.sleep(0.1)
    raise SystemExit(f"timed out waiting for {label}: {last}")

def status(base):
    return get(base + "/v1/status")

def balance(base, addr):
    return get(base + "/v1/address/" + addr)

wait("node 1", lambda: status(N1)["height"] == 0)
wait("peers", lambda: status(N1)["peers"] >= 1 and status(N3)["peers"] >= 1)
print("nodes connected")

post(N1 + "/v1/generate", {"address": A, "blocks": 100})
wait("height 100 on node 3", lambda: status(N3)["height"] >= 100)
print("mined 100 blocks, node 3 height", status(N3)["height"])

before = int(balance(N1, A)["spendable_atoms"])
print("A spendable before send", before)
PY

"$BIN" wallet send --file "$DATA/a.json" --rpc http://127.0.0.1:19734 --to "$B" --amount 1 --json > "$DATA/send-a.json"
echo "A sent 1 Gath, txid $(python3 -c 'import json; print(json.load(open("devnet-data/send-a.json"))["txid"])')"

"$BIN" miner --rpc http://127.0.0.1:19734 --address "$A" --blocks 1 --threads 1 > "$DATA/miner.out"
echo "miner confirmed $(cat "$DATA/miner.out")"

python3 - "$A" "$B" <<'PY'
import json, sys, time, urllib.request
A, B = sys.argv[1], sys.argv[2]
N1, N2, N3 = "http://127.0.0.1:19734", "http://127.0.0.1:19744", "http://127.0.0.1:19754"

def get(url):
    with urllib.request.urlopen(url, timeout=10) as resp:
        return json.load(resp)

def post(url, body):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.load(resp)

def wait(label, fn, seconds=30):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            if fn():
                return
        except Exception as exc:
            last = exc
        time.sleep(0.1)
    raise SystemExit(f"timed out waiting for {label}: {last}")

b = get(N3 + "/v1/address/" + B)
if b["confirmed_atoms"] != "100000000":
    raise SystemExit(f"B did not receive 1 Gath on node 3: {b}")
print("node 3 sees B balance", b["confirmed"])
PY

"$BIN" wallet send --file "$DATA/b.json" --rpc http://127.0.0.1:19754 --to "$A" --amount 0.25 --json > "$DATA/send-b.json"
TXB=$(python3 -c 'import json; print(json.load(open("devnet-data/send-b.json"))["txid"])')
echo "B sent 0.25 Gath via node 3, txid $TXB"

python3 - "$A" "$B" "$TXB" <<'PY'
import json, sys, time, urllib.request
A, B, TXB = sys.argv[1], sys.argv[2], sys.argv[3]
N1, N2, N3 = "http://127.0.0.1:19734", "http://127.0.0.1:19744", "http://127.0.0.1:19754"

def get(url):
    with urllib.request.urlopen(url, timeout=10) as resp:
        return json.load(resp)

def post(url, body):
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req, timeout=180) as resp:
        return json.load(resp)

def wait(label, fn, seconds=30):
    deadline = time.time() + seconds
    last = None
    while time.time() < deadline:
        try:
            if fn():
                return
        except Exception as exc:
            last = exc
        time.sleep(0.1)
    raise SystemExit(f"timed out waiting for {label}: {last}")

wait("tx on node 2", lambda: TXB in get(N2 + "/v1/mempool")["txids"])
post(N2 + "/v1/generate", {"address": A, "blocks": 1})
wait("return payment on node 1", lambda: any(u["atoms"] == "25000000" for u in get(N1 + "/v1/address/" + A)["utxos"]))
back = get(N1 + "/v1/address/" + B)
if int(back["confirmed_atoms"]) >= 100_000_000:
    raise SystemExit(f"B still has the full Gath on node 1: {back}")
if int(back["confirmed_atoms"]) == 0:
    raise SystemExit(f"B lost the change: {back}")
print("node 1 sees the 0.25 Gath return and B's change", back["confirmed"])
print("devnet exchange ok")
PY
