// Names for the list <-> post view transition. CSP forbids inline styles, so they are set
// through the CSSOM. Not deferred: pagereveal fires before the first frame.
(() => {
  const slugOf = (url) => {
    try {
      return /^\/posts\/([a-z0-9-]{1,100})$/.exec(new URL(url).pathname)?.[1];
    } catch {
      return undefined;
    }
  };
  const inView = (el) => {
    const r = el.getBoundingClientRect();
    return r.bottom > 0 && r.top < innerHeight;
  };
  const name = (vt, slug) => {
    if (!vt || !slug) return;
    const post = document.querySelector("article.post[data-slug]");
    const named = [];
    if (post?.dataset.slug === slug) {
      // The tags and metadata get a name too: a named cover would paint over them otherwise.
      named.push([post.querySelector(".post-head"), "post-head"], [post.querySelector("h1"), "title-" + slug]);
      named.push([document.querySelector(".banner"), "cover-" + slug, "cover"]);
    } else {
      const has = `:has(a[href="/posts/${slug}"])`;
      // Block boxes only: a name on a wrapped inline link would skip the whole transition.
      const title = document.querySelector(`.featured h2${has}, .row h3${has}, .card h2${has}, a.post-card[href="/posts/${slug}"] strong`);
      named.push([title, "title-" + slug], [title?.closest(".featured")?.querySelector("img"), "cover-" + slug, "cover"]);
    }
    const set = named.filter(([el]) => el && inView(el));
    for (const [el, n, cls] of set) {
      el.style.viewTransitionName = n;
      if (cls) el.style.viewTransitionClass = cls;
    }
    // A page restored from the back-forward cache must not keep them.
    const clear = () => {
      for (const [el] of set) {
        el.style.viewTransitionName = "";
        el.style.viewTransitionClass = "";
      }
    };
    vt.finished.then(clear, clear);
  };
  addEventListener("pageswap", (e) => name(e.viewTransition, slugOf(e.activation?.entry?.url) || slugOf(location.href)));
  addEventListener("pagereveal", (e) => name(e.viewTransition, slugOf(location.href) || slugOf(globalThis.navigation?.activation?.from?.url)));
})();
