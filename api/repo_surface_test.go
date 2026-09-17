package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The repo fields hold *repo.OrdersDB, which embeds *pgxpool.Pool, so a handler
// can write SQL directly and skip OrdersDB.emitEvent — the row changes, the
// request succeeds, and the event downstream consumers key off is never sent.
// The old interface-typed fields made that a compile error; this replaces the
// guardrail their removal deleted.
//
// Close is allowed (Shutdown owns the pool), and so are test files, where
// reading rows back is the point and a skipped event corrupts nothing.
func TestHandlersDoNotReachThePoolDirectly(t *testing.T) {
	banned := regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\.repo\.(Exec|Query|QueryRow|QueryFunc|Begin|BeginTx|BeginFunc|BeginTxFunc|SendBatch|CopyFrom|Acquire|AcquireFunc|AcquireAllIdle|Reset)\(`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var offenders []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(content), "\n") {
			if banned.MatchString(line) {
				offenders = append(offenders, name+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
	}

	if len(offenders) > 0 {
		t.Errorf("handlers reached the connection pool directly, which skips the event "+
			"every repo mutation emits — add a method to *OrdersDB instead. Found at:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
