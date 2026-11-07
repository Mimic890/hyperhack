// Post page: votes (with ALTCHA), contents, copy buttons, sharing and the metrics beacon.
// The page works without this script.
(() => {
  const article = document.querySelector("article.post[data-slug]");
  if (!article) return;
  const slug = article.dataset.slug;
  const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  const post = (url, body, keepalive) =>
    fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), keepalive });

  // Votes: click → challenge → solve → vote. Clicking the pressed button removes the vote.
  const votes = article.querySelector(".votes");
  const status = votes?.querySelector("[role=status]");
  // The solver needs crypto.subtle (secure contexts only); without it the buttons stay disabled.
  if (votes && globalThis.hyperhackAltcha && globalThis.crypto?.subtle) {
    const buttons = votes.querySelectorAll("button[data-vote]");
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
            const n = b.querySelector(".n");
            const value = String(b.dataset.vote === "like" ? res.likes : res.dislikes);
            if (n.textContent === value) return;
            n.textContent = value;
            // The motion rules animate .changed; the reflow restarts it on a second change.
            n.classList.remove("changed");
            void n.offsetWidth;
            n.classList.add("changed");
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

  // Contents: the active item is the last heading above the line at 35% of the window.
  // An item for h3 or h4 also marks its parent h2.
  const heads = [...article.querySelectorAll(".prose h2[id], .prose h3[id], .prose h4[id]")];
  const items = article.querySelectorAll(".toc a");
  if (items.length && "IntersectionObserver" in globalThis) {
    let active;
    const mark = () => {
      const line = innerHeight * 0.35;
      const cur = heads.findLast((h) => h.getBoundingClientRect().top <= line);
      if (cur === active) return;
      active = cur;
      items.forEach((a) => {
        const on = !!cur && a.getAttribute("href") === "#" + cur.id;
        if (on) a.setAttribute("aria-current", "location");
        else a.removeAttribute("aria-current");
        a.classList.remove("parent");
        if (!on) return;
        let li = a.parentElement;
        while (li && !li.classList.contains("l2")) li = li.previousElementSibling;
        li?.firstElementChild.classList.add("parent");
      });
    };
    const io = new IntersectionObserver(mark, { rootMargin: "-72px 0px -65% 0px" });
    heads.forEach((h) => io.observe(h));
  }

  // The clipboard API exists only in secure contexts; without it these controls do not appear.
  const clip = navigator.clipboard;
  const pageURL = () => location.href.split("#")[0];
  if (clip) {
    const uiLang = document.querySelector(".topbar")?.lang;
    article.querySelectorAll(".code-head").forEach((head) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "copy";
      btn.lang = uiLang;
      btn.setAttribute("aria-live", "polite");
      btn.textContent = article.dataset.copy;
      let timer;
      btn.addEventListener("click", async () => {
        try {
          await clip.writeText(head.nextElementSibling.textContent);
        } catch {
          return;
        }
        copies++; // writeText fires no copy event
        btn.textContent = article.dataset.copied;
        btn.classList.add("done");
        clearTimeout(timer);
        timer = setTimeout(() => {
          btn.textContent = article.dataset.copy;
          btn.classList.remove("done");
        }, 1500);
      });
      head.append(btn);
    });
    // A click on a ::before marker targets the heading, left of its box.
    article.querySelector(".prose")?.addEventListener("click", (e) => {
      const h = e.target;
      if (h.matches(".prose > h2[id], .prose > h3[id]") && e.clientX < h.getBoundingClientRect().left) {
        clip.writeText(pageURL() + "#" + h.id).catch(() => {});
      }
    });
  }

  // The post title shows in the top bar only after the page title has scrolled under it.
  const bar = document.querySelector(".topbar");
  const h1 = article.querySelector("h1");
  if (bar && h1 && "IntersectionObserver" in window) {
    new IntersectionObserver(([e]) => {
      bar.classList.toggle("past-title", !e.isIntersecting && e.boundingClientRect.top < bar.offsetHeight);
    }, { rootMargin: `-${bar.offsetHeight}px 0px 0px 0px` }).observe(h1);
  }

  const share = article.querySelector(".share");
  if (share && (navigator.share || clip)) {
    share.hidden = false;
    const shareStatus = votes.querySelector(".share-status");
    let shareTimer;
    share.addEventListener("click", async () => {
      if (navigator.share) {
        try {
          await navigator.share({ title: article.querySelector("h1").textContent, url: pageURL() });
          return;
        } catch (e) {
          if (e.name === "AbortError") return; // the reader closed the share sheet
        }
      }
      try {
        await clip.writeText(pageURL());
        // The icon turns into a check mark; the text is only for screen readers.
        shareStatus.textContent = votes.dataset.linkCopied;
        share.classList.add("done");
        clearTimeout(shareTimer);
        shareTimer = setTimeout(() => {
          share.classList.remove("done");
          shareStatus.textContent = "";
        }, 1500);
      } catch {
        // nothing to copy with: no message
      }
    });
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
