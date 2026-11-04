// ALTCHA v2 solver, the same algorithm as altcha-lib DeriveKeySHA with a uint32 counter:
// key = SHA-256(salt || nonce || counter as big-endian uint32), repeated `cost` times,
// first `keyLength` bytes; solved when the hex key starts with `keyPrefix`.
(() => {
  const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  const unhex = (s) => new Uint8Array((s.match(/../g) || []).map((h) => parseInt(h, 16)));

  async function derive(data, cost, length) {
    let d = await crypto.subtle.digest("SHA-256", data);
    for (let i = 1; i < cost; i++) d = await crypto.subtle.digest("SHA-256", d);
    return new Uint8Array(d).slice(0, length);
  }

  // solve returns the payload for the server: the challenge as received plus the solution.
  async function solve(challenge, maxCounter = 10000000) {
    const p = challenge.parameters;
    if (p.algorithm !== "SHA-256") throw new Error("unsupported algorithm");
    const salt = unhex(p.salt);
    const nonce = unhex(p.nonce);
    const cost = Math.max(1, p.cost || 1);
    const length = p.keyLength > 0 ? p.keyLength : 32;
    const prefix = String(p.keyPrefix).toLowerCase();
    const batch = 256;
    for (let start = 0; start <= maxCounter; start += batch) {
      const jobs = [];
      for (let n = start; n < start + batch && n <= maxCounter; n++) {
        const data = new Uint8Array(salt.length + nonce.length + 4);
        data.set(salt);
        data.set(nonce, salt.length);
        new DataView(data.buffer).setUint32(data.length - 4, n);
        jobs.push(derive(data, cost, length));
      }
      const keys = await Promise.all(jobs);
      for (let i = 0; i < keys.length; i++) {
        const key = hex(keys[i]);
        if (key.startsWith(prefix)) return { challenge, solution: { counter: start + i, derivedKey: key } };
      }
    }
    throw new Error("challenge not solved");
  }

  globalThis.hyperhackAltcha = { solve };
})();
