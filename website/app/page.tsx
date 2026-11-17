import { REPO_URL } from "@/lib/site";

const features = [
  {
    title: "One asset, no issuer",
    body: "The chain moves one asset, Gath. There is no issuer and no admin key. The mainnet parameters are in the binary.",
  },
  {
    title: "Node, miner, wallet, and terminal",
    body: "This repository is the full node, the CPU miner, the command-line wallet, and a terminal interface.",
  },
  {
    title: "Keys stay on this machine",
    body: "The recovery phrase is a BIP39 mnemonic. The words alone restore the wallet. The passphrase only encrypts the wallet file on this machine.",
  },
  {
    title: "Open source",
    body: "The software is released under the MIT licence. Fork it if you want to run your own network.",
  },
];

export default function Page() {
  return (
    <div className="page">
      <a className="skip" href="#main">
        Skip to content
      </a>

      <header className="header">
        <a className="wordmark" href="#main">
          Gath
        </a>
        <a className="header-link" href={REPO_URL} rel="noreferrer">
          GitHub
        </a>
      </header>

      <main id="main">
        <p className="notice" role="status">
          Gath is no longer maintained. The public seed node is offline. Fork it if you want
          to run your own network.
        </p>

        <section className="hero">
          <h1>A proof-of-work currency.</h1>
          <p className="lead">
            This repository is the full node, the CPU miner, the command-line wallet, and a
            terminal interface. The chain moves one asset, Gath. There is no issuer and no
            admin key.
          </p>
          <div className="actions">
            <a className="btn btn-primary" href="#install">
              Get started
            </a>
            <a className="btn btn-secondary" href={REPO_URL} rel="noreferrer">
              GitHub
            </a>
          </div>
        </section>

        <section className="features" aria-labelledby="features-heading">
          <h2 id="features-heading">What it is</h2>
          <ul>
            {features.map((item) => (
              <li key={item.title}>
                <h3>{item.title}</h3>
                <p>{item.body}</p>
              </li>
            ))}
          </ul>
        </section>

        <section className="install" id="install" aria-labelledby="install-heading">
          <h2 id="install-heading">Install</h2>
          <p>
            On macOS and Linux, the installer checks for Git and Go 1.24 or newer, clones this
            repository, and asks you to confirm. It then builds <code>gath</code> and puts it
            on your PATH. Run <code>gath</code> to open the terminal on mainnet.
          </p>
          <pre>
            <code>{`curl -fsSL https://raw.githubusercontent.com/gath-org/gath/main/scripts/install.sh | sh`}</code>
          </pre>
          <p>From a checkout:</p>
          <pre>
            <code>{`git clone https://github.com/gath-org/gath.git
cd gath
go build -o bin/gath ./cmd/gath`}</code>
          </pre>
          <p>
            <code>gath</code> with no arguments opens the terminal on mainnet. From a
            published commit:
          </p>
          <pre>
            <code>{`go install github.com/gath-org/gath/cmd/gath@latest`}</code>
          </pre>
        </section>
      </main>

      <footer className="footer">
        <p>
          <a href={`${REPO_URL}/blob/main/LICENSE`} rel="noreferrer">
            MIT licence
          </a>
          <span aria-hidden="true"> · </span>
          <a href={REPO_URL} rel="noreferrer">
            GitHub
          </a>
        </p>
      </footer>
    </div>
  );
}
