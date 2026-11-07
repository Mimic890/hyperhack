// Search: the palette on every page and live results on the search page.
// Without this script the search links and the form on /search do the same job.
(() => {
  // "live" requests update the results while typing and are not stored as queries.
  // After a pause the query is sent once more as "final", so only settled queries count.
  const liveSearch = (getURL, show) => {
    let timer, settle, ctrl;
    const run = (mode) => {
      const url = getURL();
      if (ctrl) ctrl.abort();
      ctrl = new AbortController();
      fetch(url, { signal: ctrl.signal, headers: { "X-Search": mode } })
        .then((r) => (r.ok ? r.text() : Promise.reject(r.status)))
        .then((html) => show(new DOMParser().parseFromString(html, "text/html"), url))
        .catch(() => {}); // aborted or failed: keep the current results
    };
    const stop = () => {
      clearTimeout(timer);
      clearTimeout(settle);
      if (ctrl) ctrl.abort();
    };
    const typed = () => {
      stop();
      timer = setTimeout(() => run("live"), 250);
      settle = setTimeout(() => run("final"), 1500);
    };
    return { typed, stop };
  };

  // The search page.
  const form = document.getElementById("search");
  const results = document.getElementById("results");
  const status = document.getElementById("status");
  if (form && results && status) {
    const live = liveSearch(
      () => "/search?" + new URLSearchParams(new FormData(form)),
      (doc, url) => {
        const next = doc.getElementById("results");
        if (!next) return;
        results.replaceChildren(...next.childNodes);
        status.textContent = doc.getElementById("status").textContent;
        document.title = doc.title;
        history.replaceState(null, "", url);
      },
    );
    form.addEventListener("input", live.typed);
  }

  // The palette.
  const dialog = document.getElementById("palette");
  if (!dialog || !dialog.showModal) return;
  const input = dialog.querySelector("input");
  const list = dialog.querySelector("[role=listbox]");
  const chips = dialog.querySelector(".palette-chips");
  const paletteStatus = dialog.querySelector("[role=status]");
  let tags = [];
  try {
    tags = JSON.parse(document.getElementById("tag-data")?.textContent || "[]") || []; // a site without tags sends null
  } catch {
    // no suggestions
  }
  const known = new Set(tags.map((t) => t.Name));
  let options = [];
  let selected = -1;

  const select = (i) => {
    options[selected]?.setAttribute("aria-selected", "false");
    selected = i;
    const el = options[i];
    if (el) {
      el.setAttribute("aria-selected", "true");
      el.scrollIntoView({ block: "nearest" });
      input.setAttribute("aria-activedescendant", el.id);
    } else {
      input.removeAttribute("aria-activedescendant");
    }
  };
  const setOptions = (els) => {
    options = els;
    selected = -1;
    els.forEach((el, i) => {
      el.id = "palette-option-" + i;
      el.setAttribute("role", "option");
      el.setAttribute("aria-selected", "false");
    });
    list.replaceChildren(...els);
    input.setAttribute("aria-expanded", String(els.length > 0));
    select(els.length ? 0 : -1);
  };

  // A result: the title with its marks, the date and the text of a hit from /search.
  const hitOption = (hit) => {
    const a = document.createElement("a");
    a.className = "hit";
    a.href = hit.querySelector("h2 a").getAttribute("href");
    const title = document.createElement("span");
    title.className = "hit-title";
    title.append(...hit.querySelector("h2 a").cloneNode(true).childNodes);
    a.append(title);
    for (const part of hit.querySelectorAll(".meta, .desc")) a.append(document.importNode(part, true));
    return a;
  };

  const live = liveSearch(
    () => "/search?" + new URLSearchParams({ q: input.value }),
    (doc) => {
      setOptions([...doc.querySelectorAll("#results article")].map(hitOption));
      const empty = doc.querySelector("#results .empty");
      if (empty) list.append(document.importNode(empty, true));
      paletteStatus.textContent = doc.getElementById("status")?.textContent || "";
    },
  );

  // The #tag token before the caret: start offset and the typed part, or null.
  const typedTag = () => {
    const before = input.value.slice(0, input.selectionStart);
    const m = /(?:^|\s)#([^\s#]*)$/.exec(before);
    return m ? { start: before.length - m[1].length - 1, prefix: m[1].toLowerCase() } : null;
  };

  const complete = (name) => {
    const typed = typedTag();
    if (!typed) return;
    const rest = input.value.slice(input.selectionStart).replace(/^\S*\s*/, "");
    const head = input.value.slice(0, typed.start) + "#" + name + " ";
    input.value = head + rest;
    input.setSelectionRange(head.length, head.length);
    input.focus();
    update();
  };

  const tagOption = (tag) => {
    const el = document.createElement("div");
    el.className = "tag-option";
    const hash = document.createElement("span");
    hash.className = "hash";
    hash.textContent = "#";
    const count = document.createElement("span");
    count.className = "n";
    count.textContent = tag.Posts;
    el.append(hash, tag.Name, count);
    el.addEventListener("click", () => complete(tag.Name));
    return el;
  };

  const showChips = (typed) => {
    let value = input.value;
    if (typed) value = value.slice(0, typed.start) + value.slice(input.selectionStart).replace(/^\S*/, "");
    const names = new Set(value.toLowerCase().split(/\s+/).filter((t) => t[0] === "#" && known.has(t.slice(1))).map((t) => t.slice(1)));
    chips.replaceChildren(
      ...[...names].map((name) => {
        const li = document.createElement("li");
        li.className = "tag";
        const hash = document.createElement("span");
        hash.textContent = "#";
        li.append(hash, name);
        return li;
      }),
    );
    chips.hidden = names.size === 0;
  };

  const update = () => {
    const typed = typedTag();
    showChips(typed);
    const suggestions = typed ? tags.filter((t) => t.Name.startsWith(typed.prefix)) : [];
    if (suggestions.length) {
      live.stop();
      paletteStatus.textContent = "";
      setOptions(suggestions.map(tagOption));
    } else if (!input.value.trim()) {
      live.stop();
      paletteStatus.textContent = "";
      setOptions([]);
    } else {
      live.typed();
    }
  };

  const open = () => {
    if (!dialog.open) dialog.showModal();
    input.focus();
    input.select();
  };

  input.addEventListener("input", update);
  input.addEventListener("keydown", (e) => {
    if (e.isComposing) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      // The cycle includes "nothing selected": Enter then opens the results page.
      const n = options.length + 1;
      select(((selected + 1 + (e.key === "ArrowDown" ? 1 : -1) + n) % n) - 1);
    } else if (e.key === "Enter" && options[selected]) {
      e.preventDefault();
      options[selected].click();
    } else if (e.key === "Escape") {
      e.preventDefault(); // in a search field Esc would only clear the text
      dialog.close();
    }
  });
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close(); // the backdrop
  });
  dialog.addEventListener("close", live.stop);

  addEventListener("keydown", (e) => {
    const typing = e.target.closest?.("input, textarea, select, [contenteditable]");
    // e.code: the same keys on a Russian layout.
    const shortcut =
      ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.code === "KeyK") ||
      (!typing && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && (e.key === "/" || e.code === "Slash"));
    if (!shortcut) return;
    e.preventDefault();
    open();
  });
  document.querySelectorAll('.tools a[href="/search"]').forEach((a) => {
    a.addEventListener("click", (e) => {
      if (e.ctrlKey || e.metaKey || e.shiftKey) return; // open in a new tab or window
      e.preventDefault();
      open();
    });
  });
})();
