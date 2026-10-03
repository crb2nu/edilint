package edilint

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestGenerate835Amounts(t *testing.T) {
	for _, count := range []int{1, 2, 17, 1000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			var b bytes.Buffer
			if err := Generate(&b, GenerateOptions{Kind: "835", Count: count, Date: "2024-02-29", ControlNumber: 42}); err != nil {
				t.Fatal(err)
			}
			var payment, claimsPaid, charge, paid, patient int64
			var claims, services, adjustments int
			seenClaims := make(map[string]bool)
			for _, segment := range strings.Split(strings.TrimSpace(b.String()), "~\n") {
				f := strings.Split(strings.TrimSuffix(segment, "~"), "*")
				switch f[0] {
				case "BPR":
					payment = fixtureCents(t, f[2])
					if f[4] != "CHK" {
						t.Fatalf("expected fictional check payment, got %q", segment)
					}
				case "TRN":
					if f[2] != "FAKETRACE000000042" {
						t.Fatalf("trace did not use requested control: %q", segment)
					}
				case "CLP":
					claims++
					if seenClaims[f[1]] || f[1] != fmt.Sprintf("FAKECLAIM%06d", claims) {
						t.Fatalf("nonfictional or repeated claim ID: %q", f[1])
					}
					seenClaims[f[1]] = true
					charge, paid, patient = fixtureCents(t, f[3]), fixtureCents(t, f[4]), fixtureCents(t, f[5])
					if charge != 12500 || paid != 10000 || patient != 2500 || charge != paid+patient {
						t.Fatalf("unbalanced claim: %q", segment)
					}
					claimsPaid += paid
				case "SVC":
					services++
					if fixtureCents(t, f[2]) != charge || fixtureCents(t, f[3]) != paid {
						t.Fatalf("service amounts disagree with claim: %q", segment)
					}
				case "CAS":
					adjustments++
					if f[1] != "PR" || fixtureCents(t, f[3]) != patient {
						t.Fatalf("adjustment disagrees with patient responsibility: %q", segment)
					}
				case "DTM":
					if f[2] != "20240229" {
						t.Fatalf("date was not applied: %q", segment)
					}
				}
			}
			if claims != count || services != count || adjustments != count || payment != claimsPaid {
				t.Fatalf("claims/services/adjustments=%d/%d/%d want %d; payment=%d sum=%d", claims, services, adjustments, count, payment, claimsPaid)
			}
		})
	}
}

func fixtureCents(t *testing.T, amount string) int64 {
	t.Helper()
	dollars, cents, ok := strings.Cut(amount, ".")
	if !ok || len(cents) != 2 {
		t.Fatalf("amount must have two decimal places: %q", amount)
	}
	n, err := strconv.ParseInt(dollars+cents, 10, 64)
	if err != nil {
		t.Fatalf("invalid amount %q: %v", amount, err)
	}
	return n
}

type fixturePrefixWriter struct {
	prefix []byte
	err    error
}

func (w *fixturePrefixWriter) Write(p []byte) (int, error) {
	w.prefix = append(w.prefix, p...)
	return 0, w.err
}

func TestGenerate835MaximumPayment(t *testing.T) {
	// Capture the first output buffer, then stop generation. The BPR total must
	// include the entire requested file without accumulating claim records.
	stop := errors.New("captured prefix")
	w := &fixturePrefixWriter{err: stop}
	err := Generate(w, GenerateOptions{Kind: "835", Count: MaxGenerateCount})
	if !errors.Is(err, stop) {
		t.Fatalf("writer error = %v", err)
	}
	if !bytes.Contains(w.prefix, []byte("BPR*I*100000000.00*C*CHK~\n")) {
		t.Fatalf("maximum-count payment missing or overflowed: %q", w.prefix)
	}
}
