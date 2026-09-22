// Package dashboard computes report periods and ranks account activity.
package dashboard

import (
	"math"
	"sort"
	"strings"
	"time"

	"hshow/internal/hledger"
)

// Change describes one account's net change in the current period compared
// to the same-length period last month.
type Change struct {
	Account  string
	Current  float64
	Previous float64
	Diff     float64
}

// PeriodBounds returns the [start, end) bounds for the current billing cycle
// (which starts on cycleDay of each month) and the matching prior cycle of
// equal length, given the current instant now. end values are exclusive,
// matching hledger's -e semantics.
func PeriodBounds(now time.Time, cycleDay int) (curStart, curEnd, prevStart, prevEnd time.Time) {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	if now.Day() >= cycleDay {
		curStart = time.Date(now.Year(), now.Month(), cycleDay, 0, 0, 0, 0, loc)
	} else {
		m := now.AddDate(0, -1, 0)
		curStart = time.Date(m.Year(), m.Month(), cycleDay, 0, 0, 0, 0, loc)
	}
	curEnd = today.AddDate(0, 0, 1)

	daysElapsed := int(today.Sub(curStart).Hours() / 24)
	prevStart = curStart.AddDate(0, -1, 0)
	prevEnd = prevStart.AddDate(0, 0, daysElapsed+1)
	return
}

// TopChanges merges current and previous period balances by account,
// ranks them by the magnitude of the current period's activity, and
// returns the top n.
func TopChanges(current, previous []hledger.Balance, n int) []Change {
	byAccount := make(map[string]*Change)

	get := func(account string) *Change {
		c, ok := byAccount[account]
		if !ok {
			c = &Change{Account: account}
			byAccount[account] = c
		}
		return c
	}

	for _, b := range current {
		get(b.Account).Current = b.Amount
	}
	for _, b := range previous {
		get(b.Account).Previous = b.Amount
	}

	changes := make([]Change, 0, len(byAccount))
	for _, c := range byAccount {
		c.Diff = c.Current - c.Previous
		changes = append(changes, *c)
	}

	sort.Slice(changes, func(i, j int) bool {
		return math.Abs(changes[i].Current) > math.Abs(changes[j].Current)
	})

	if n >= 0 && len(changes) > n {
		changes = changes[:n]
	}
	return changes
}

// FilterByPrefix returns only the balances whose account is prefix or a
// subaccount of it (prefix followed by ":"), matching hledger's own account
// hierarchy convention.
func FilterByPrefix(balances []hledger.Balance, prefix string) []hledger.Balance {
	filtered := make([]hledger.Balance, 0, len(balances))
	for _, b := range balances {
		if b.Account == prefix || strings.HasPrefix(b.Account, prefix+":") {
			filtered = append(filtered, b)
		}
	}
	return filtered
}
