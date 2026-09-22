// Package hledger runs the hledger CLI and parses its balance report output.
package hledger

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Balance is the net change of a single account over a report period.
type Balance struct {
	Account string
	Amount  float64
}

// Runner invokes the hledger CLI against a specific journal file.
type Runner struct {
	Bin         string
	JournalPath string
}

// NewRunner returns a Runner, defaulting Bin to "hledger" if empty.
func NewRunner(bin, journalPath string) Runner {
	if bin == "" {
		bin = "hledger"
	}
	return Runner{Bin: bin, JournalPath: journalPath}
}

const dateLayout = "2006-01-02"

// Balances returns the net change per account between begin (inclusive) and
// end (exclusive), matching hledger's -b/-e semantics. An optional
// accountFilter (e.g. "^expenses") is appended as a positional hledger query
// argument to restrict which accounts are returned.
func (r Runner) Balances(begin, end time.Time, accountFilter ...string) ([]Balance, error) {
	args := []string{
		"balance",
		"-O", "csv",
		"--flat",
		"-N",
		"-f", r.JournalPath,
		"-b", begin.Format(dateLayout),
		"-e", end.Format(dateLayout),
	}
	for _, f := range accountFilter {
		if f != "" {
			args = append(args, f)
		}
	}
	cmd := exec.Command(r.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("hledger balance failed: %s", msg)
	}
	return parseBalanceCSV(stdout.Bytes())
}

var amountRe = regexp.MustCompile(`-?[0-9][0-9,]*\.?[0-9]*`)

func parseAmount(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	m := amountRe.FindString(s)
	if m == "" {
		return 0, fmt.Errorf("no numeric amount found in %q", s)
	}
	neg := strings.Contains(s, "-")
	m = strings.TrimPrefix(m, "-")
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing amount %q: %w", s, err)
	}
	if neg {
		v = -v
	}
	return v, nil
}

func parseBalanceCSV(data []byte) ([]Balance, error) {
	r := csv.NewReader(bytes.NewReader(data))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing hledger csv output: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// First row is the header; skip it.
	rows = rows[1:]

	balances := make([]Balance, 0, len(rows))
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		account := strings.TrimSpace(row[0])
		if account == "" || strings.EqualFold(account, "total") {
			continue
		}
		amount, err := parseAmount(row[len(row)-1])
		if err != nil {
			return nil, fmt.Errorf("account %q: %w", account, err)
		}
		balances = append(balances, Balance{Account: account, Amount: amount})
	}
	return balances, nil
}
