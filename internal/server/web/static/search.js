// Updates search results while typing. The form works the same without this script.
(() => {
  const form = document.getElementById("search");
  const results = document.getElementById("results");
  const status = document.getElementById("status");
  if (!form || !results || !status) return;
  let timer, settle, ctrl;
  // "live" requests update the results while typing and are not stored as queries.
  // After a pause the query is sent once more as "final", so only settled queries count.
  const run = (mode) => {
    const url = "/search?" + new URLSearchParams(new FormData(form));
    if (ctrl) ctrl.abort();
    ctrl = new AbortController();
    fetch(url, { signal: ctrl.signal, headers: { "X-Search": mode } })
      .then((r) => (r.ok ? r.text() : Promise.reject(r.status)))
      .then((html) => {
        const doc = new DOMParser().parseFromString(html, "text/html");
        const next = doc.getElementById("results");
        if (!next) return;
        results.replaceChildren(...next.childNodes);
        status.textContent = doc.getElementById("status").textContent;
        document.title = doc.title;
        history.replaceState(null, "", url);
      })
      .catch(() => {}); // aborted or failed: keep the current results
  };
  form.addEventListener("input", () => {
    clearTimeout(timer);
    clearTimeout(settle);
    timer = setTimeout(() => run("live"), 250);
    settle = setTimeout(() => run("final"), 1500);
  });
})();
