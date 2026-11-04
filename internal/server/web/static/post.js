// Post page: votes (with ALTCHA) and the metrics beacon. The page works without this script.
(() => {
  const article = document.querySelector("article.post[data-slug]");
  if (!article) return;
  const slug = article.dataset.slug;
  const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  const post = (url, body, keepalive) =>
    fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), keepalive });

  // Votes: click → challenge → solve → vote. Clicking the pressed button removes the vote.
  const votes = article.querySelector(".votes");
  // The solver needs crypto.subtle (secure contexts only); without it the buttons stay disabled.
  if (votes && globalThis.hyperhackAltcha && globalThis.crypto?.subtle) {
    const buttons = votes.querySelectorAll("button[data-vote]");
    const status = votes.querySelector("[role=status]");
    const busy = (on) => {
      votes.setAttribute("aria-busy", String(on));
      buttons.forEach((b) => (b.disabled = on));
    };
    buttons.forEach((btn) => {
      btn.disabled = false;
      btn.addEventListener("click", async () => {
        const vote = btn.getAttribute("aria-pressed") === "true" ? "none" : btn.dataset.vote;
        busy(true);
        status.textContent = "";
        try {
          const r1 = await fetch("/api/altcha");
          if (!r1.ok) throw new Error(String(r1.status));
          const altcha = await globalThis.hyperhackAltcha.solve(await r1.json());
          const r2 = await post("/api/vote", { slug, vote, altcha }, false);
          if (!r2.ok) throw new Error(String(r2.status));
          const res = await r2.json();
          buttons.forEach((b) => {
            b.setAttribute("aria-pressed", String(b.dataset.vote === res.vote));
            b.querySelector(".n").textContent = b.dataset.vote === "like" ? res.likes : res.dislikes;
          });
        } catch {
          status.textContent = votes.dataset.error;
        } finally {
          busy(false);
        }
      });
    });
  }

  // Beacon: on every hide, send what is new since the last send. Every delta carries
  // the same random view id; the server sums time per view and counts the view once.
  const view = hex(crypto.getRandomValues(new Uint8Array(16)));
  let first = true;
  let visibleMs = 0;
  let since = document.visibilityState === "visible" ? performance.now() : null;
  let copies = 0;
  const depth = new Set();
  const outbound = [];

  const source = () => {
    if (!document.referrer) return "direct";
    let host;
    try {
      host = new URL(document.referrer).hostname.toLowerCase();
    } catch {
      return "other";
    }
    if (host === location.hostname) return "internal";
    const known = [
      ["telegram", /(^|\.)(t\.me|telegram\.(org|me))$/],
      ["google", /(^|\.)google\.[a-z.]+$/],
      ["yandex", /(^|\.)(yandex\.[a-z.]+|ya\.ru)$/],
      ["bing", /(^|\.)bing\.com$/],
      ["github", /(^|\.)github\.com$/],
    ];
    for (const [name, re] of known) if (re.test(host)) return name;
    return "other";
  };
  const device = () => (innerWidth < 768 ? "mobile" : innerWidth < 1024 ? "tablet" : "desktop");
  const theme = () =>
    document.documentElement.dataset.theme || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const lang = () => {
    const l = document.querySelector(".topbar")?.lang;
    return l === "ru" ? "ru" : "en";
  };

  const context = { source: source(), device: device(), lang: lang(), theme: theme() }; // as at page load

  if ("IntersectionObserver" in globalThis) {
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        depth.add(Number(e.target.dataset.depth));
        io.unobserve(e.target);
      }
    });
    article.querySelectorAll(".depth").forEach((m) => io.observe(m));
  }

  const onClick = (e) => {
    const a = e.target.closest && e.target.closest(".prose a[href]");
    if (!a || outbound.length >= 20) return;
    try {
      const u = new URL(a.href);
      if ((u.protocol === "https:" || u.protocol === "http:") && u.hostname !== location.hostname) outbound.push(u.hostname.toLowerCase());
    } catch {
      // not a URL
    }
  };
  article.addEventListener("click", onClick);
  article.addEventListener("auxclick", onClick);
  document.addEventListener("copy", () => {
    const n = document.getSelection()?.anchorNode;
    const el = n && (n.nodeType === 1 ? n : n.parentElement);
    if (el && el.closest("pre, code") && article.contains(el)) copies++;
  });

  const flush = () => {
    if (since !== null) {
      const now = performance.now();
      visibleMs += now - since;
      since = document.visibilityState === "visible" ? now : null;
    }
    const time = Math.floor(visibleMs / 1000);
    visibleMs -= time * 1000;
    if (!first && time === 0 && depth.size === 0 && outbound.length === 0 && copies === 0) return;
    const b = { slug, view, first, time: Math.min(time, 14400), depth: [...depth], outbound: outbound.splice(0), copies: Math.min(copies, 100) };
    if (first) Object.assign(b, context);
    first = false;
    depth.clear();
    copies = 0;
    post("/api/beacon", b, true).catch(() => {});
  };
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flush();
    else since = performance.now();
  });
  addEventListener("pagehide", flush);
})();
