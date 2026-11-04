package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altcha-org/altcha-lib-go/v2"
)

// solveWithJS runs static/altcha.js in node (WebCrypto) and returns its payload.
func solveWithJS(t *testing.T, ch altcha.Challenge) altcha.Payload {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required in CI for the ALTCHA solver test")
		}
		t.Skip("node not installed")
	}
	src, err := webFS.ReadFile("web/static/altcha.js")
	if err != nil {
		t.Fatal(err)
	}
	solver := filepath.Join(t.TempDir(), "altcha.js")
	if err := os.WriteFile(solver, src, 0o600); err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(ch)
	// The browser file sets globalThis.hyperhackAltcha; load it as-is and call solve.
	script := `const fs = require("fs");
(0, eval)(fs.readFileSync(process.argv[1], "utf8"));
const ch = JSON.parse(fs.readFileSync(0, "utf8"));
globalThis.hyperhackAltcha.solve(ch).then((p) => process.stdout.write(JSON.stringify(p)), (e) => { console.error(e); process.exit(1); });`
	cmd := exec.Command(node, "-e", script, solver)
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var p altcha.Payload
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("payload %s: %v", out, err)
	}
	return p
}

func TestAltchaJSSolverPassesLibrary(t *testing.T) {
	g := newAltchaGuard("0123456789abcdef0123456789abcdef", 3000, 5*time.Minute)
	ch, err := g.challenge()
	if err != nil {
		t.Fatal(err)
	}
	p := solveWithJS(t, ch)

	// The library's own v2 verification, with the exact options the server uses.
	res, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge: p.Challenge, Solution: p.Solution, DeriveKey: altcha.DeriveKeySHA(),
		HMACSignatureSecret: g.sigKey, HMACKeySignatureSecret: g.keySigKey,
	})
	if err != nil || !res.Verified {
		t.Fatalf("VerifySolution = %+v, %v", res, err)
	}
	// Slow path too: re-derive without the key signature, so the counter itself is checked.
	res, err = altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge: p.Challenge, Solution: p.Solution, DeriveKey: altcha.DeriveKeySHA(), HMACSignatureSecret: g.sigKey,
	})
	if err != nil || !res.Verified {
		t.Fatalf("VerifySolution (re-derive) = %+v, %v", res, err)
	}
	// Same answer as the library solver.
	lib, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeySHA()})
	if err != nil || lib == nil || lib.Counter != p.Solution.Counter || lib.DerivedKey != p.Solution.DerivedKey {
		t.Fatalf("library solution %+v, JS %+v", lib, p.Solution)
	}

	if err := g.verify(p); err != nil {
		t.Fatalf("guard.verify = %v", err)
	}
	if err := g.verify(p); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay = %v", err)
	}
}

func TestAltchaRejects(t *testing.T) {
	g := newAltchaGuard("0123456789abcdef0123456789abcdef", 1000, time.Minute)
	ch, _ := g.challenge()
	sol, _ := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeySHA()})

	bad := *sol
	bad.DerivedKey = strings.Repeat("0", 64)
	if err := g.verify(altcha.Payload{Challenge: ch, Solution: bad}); err == nil {
		t.Error("wrong key accepted")
	}
	other := newAltchaGuard("another key, also 32 bytes long!!", 1000, time.Minute)
	if err := other.verify(altcha.Payload{Challenge: ch, Solution: *sol}); err == nil {
		t.Error("challenge from another key accepted")
	}
	tampered := ch
	tampered.Parameters.ExpiresAt += 3600
	if err := g.verify(altcha.Payload{Challenge: tampered, Solution: *sol}); err == nil {
		t.Error("extended expiry accepted")
	}
	ch2, _ := (&altchaGuard{sigKey: g.sigKey, keySigKey: g.keySigKey, maxNumber: 10, expires: time.Second, now: time.Now}).challenge()
	sol2, _ := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch2, DeriveKey: altcha.DeriveKeySHA()})
	time.Sleep(1100 * time.Millisecond)
	if err := g.verify(altcha.Payload{Challenge: ch2, Solution: *sol2}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired = %v", err)
	}
}
