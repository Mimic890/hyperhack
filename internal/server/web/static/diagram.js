// Draws ```mermaid code blocks as diagrams with Pele (pele.js, loaded just before).
// Without JS, or when a diagram has an error, the code block stays.
(() => {
  if (!window.pele) return;
  // Pele writes style="…" attributes, which the CSP blocks in markup, even in DOMParser.
  // Styles set through CSSOM are allowed, so the attribute is renamed before parsing and
  // moved to CSSOM after. Pele escapes " in text, so ` style="` only starts an attribute.
  const toNode = (svg) => {
    const doc = new DOMParser().parseFromString(svg.replaceAll(' style="', ' data-pele-style="'), "image/svg+xml");
    const root = doc.documentElement;
    if (root.localName !== "svg") throw new Error("not an SVG");
    document.adoptNode(root);
    for (const el of [root, ...root.querySelectorAll("[data-pele-style]")]) {
      if (!el.hasAttribute("data-pele-style")) continue;
      el.style.cssText = el.getAttribute("data-pele-style");
      el.removeAttribute("data-pele-style");
    }
    return root;
  };
  for (const head of document.querySelectorAll('.prose .code-head[data-lang="mermaid"]')) {
    const block = head.parentElement;
    const code = block.querySelector("code");
    if (!code) continue;
    try {
      pele.render(code.textContent); // a broken diagram stays code: mount's error box sets a style attribute
    } catch {
      continue;
    }
    const fig = document.createElement("div");
    fig.className = "diagram";
    block.before(fig);
    let failed = false;
    // No zoom: its buttons set style attributes, which the CSP blocks.
    const m = pele.mount(fig, code.textContent, { zoom: false, sanitize: toNode, onError: () => { failed = true; } });
    if (failed || !fig.querySelector("svg")) {
      m.destroy();
      fig.remove();
    } else {
      block.remove();
    }
  }
})();
