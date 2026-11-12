"use client";

import { useEffect, useRef, useState } from "react";
import type { Film } from "@/lib/film/engine";
import { CAPTIONS } from "@/lib/film/timeline";
import { asset, REPO_LABEL, REPO_URL } from "@/lib/site";

type Phase = "film" | "final";

export default function Gath() {
  const canvas = useRef<HTMLCanvasElement>(null);
  const hud = useRef<HTMLDivElement>(null);
  const counter = useRef<HTMLDivElement>(null);
  const film = useRef<Film | null>(null);
  const reduced = useRef(false);
  const [phase, setPhase] = useState<Phase>("film");
  const [caption, setCaption] = useState(-1);
  const [gl, setGl] = useState<"wait" | "ready" | "fail">("wait");
  const [clean, setClean] = useState(false);

  useEffect(() => {
    let disposed = false;
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    reduced.current = mq.matches;
    const q = new URLSearchParams(window.location.search);
    const freeze = q.get("t"); // QA: ?t=<seconds> freezes the film at that moment
    setClean(q.has("clean"));

    import("@/lib/film/engine").then(({ createFilm }) => {
      if (disposed || !canvas.current || !hud.current) return;
      const f = createFilm({
        canvas: canvas.current,
        hud: hud.current,
        counter: counter.current,
        onCaption: setCaption,
        onFinal: () => setPhase("final"),
        onFilm: () => setPhase("film"),
      });
      if (!f) {
        setGl("fail");
        setPhase("final");
        return;
      }
      film.current = f;
      setGl("ready");
      if (freeze !== null) f.freeze(Math.max(0, parseFloat(freeze) || 0));
      else if (reduced.current) {
        setPhase("final");
        f.still();
      } else f.play();
    });

    const onChange = () => {
      reduced.current = mq.matches;
      const f = film.current;
      if (!f) return;
      if (mq.matches) {
        setPhase("final");
        setCaption(-1);
        f.still();
      } else f.play();
    };
    mq.addEventListener("change", onChange);
    return () => {
      disposed = true;
      mq.removeEventListener("change", onChange);
      film.current?.dispose();
      film.current = null;
    };
  }, []);

  return (
    <main className="stage" data-phase={phase} data-gl={gl} data-clean={clean ? "" : undefined}>
      {/* Static still for no-JS visitors and browsers without WebGL; only fetched when needed. */}
      <noscript>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img className="fallback" src={asset("/gath-landscape.webp")} alt="" width={1600} height={1000} />
      </noscript>
      {gl === "fail" && (
        // eslint-disable-next-line @next/next/no-img-element
        <img className="fallback" src={asset("/gath-landscape.webp")} alt="" aria-hidden="true" width={1600} height={1000} />
      )}
      <canvas ref={canvas} className="gl" aria-hidden="true" />
      <div ref={hud} className="hud-layer" aria-hidden="true" />

      {/* Film overlay */}
      <div className="film-ui caption-row" aria-hidden="true">
        <p className="caption m-0" key={caption}>
          {caption >= 0 ? CAPTIONS[caption][2] : ""}
        </p>
      </div>
      <div ref={counter} className="film-ui counter" aria-hidden="true">
        <span data-n className="counter-n">0</span>
        <span className="counter-l">BLOCKS · HEAVIEST WORK</span>
      </div>
      {/* Settled overlay */}
      <div className="final">
        <h1 className="final-ui title m-0">
          <span aria-hidden="true">GATH</span>
          <span className="sr-only">Gath</span>
        </h1>
        <p className="final-ui d1 tagline m-0">A standalone proof-of-work currency. No issuer, no admin key, no premine.</p>
        <div className="final-row">
          {/* Repo link is a placeholder until the repository is published. */}
          <a href={REPO_URL} className="final-ui d2 repo">
            {REPO_LABEL} <span aria-hidden="true">↗</span>
          </a>
          <p className="final-ui d3 status m-0">Open-source software in development. Not a public network yet. MIT License.</p>
        </div>
      </div>

      <section className="sr-only" aria-label="How Gath works">
        <h2>How Gath works</h2>
        <ol>
          <li>Gath has its own chain. There is no issuer, no admin key and no premine; the genesis block pays nothing.</li>
          <li>Anyone can run a node and mine. Consensus is permissionless SHA3-256 proof of work, with a block about every 45 seconds. The chain with the most cumulative work is the active chain.</li>
          <li>New Gath is created only as a block subsidy: 5 Gath per block at first, halving every 2,100,000 blocks. The hard cap is 20,999,999.727 Gath.</li>
          <li>Keys stay on your device. A 24-word recovery phrase restores the wallet. Addresses start with gath.</li>
          <li>Transfers are signed with ML-DSA-44. The fee is at least 1 atom per byte; 1 Gath is 100,000,000 atoms.</li>
          <li>Every node verifies every block and transaction. There are no smart contracts; the base layer only moves Gath.</li>
        </ol>
      </section>
    </main>
  );
}
