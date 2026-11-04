// Updates search results while typing. The form works the same without this script.
(() => {
  const form = document.getElementById("search");
  const results = document.getElementById("results");
  const status = document.getElementById("status");
  if (!form || !results || !status) return;
  let timer, ctrl;
  const run = () => {
    const url = "/search?" + new URLSearchParams(new FormData(form));
    if (ctrl) ctrl.abort();
    ctrl = new AbortController();
    fetch(url, { signal: ctrl.signal })
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
    timer = setTimeout(run, 250);
  });
})();
