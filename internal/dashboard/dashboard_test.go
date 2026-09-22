package dashboard

import (
	"testing"
	"time"

	"hshow/internal/hledger"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestPeriodBounds(t *testing.T) {
	// All cases use cycleDay=25 (billing cycle starts on the 25th).
	cases := []struct {
		name                                          string
		now                                           time.Time
		wantCurStart, wantCurEnd                      time.Time
		wantPrevStart, wantPrevEnd                    time.Time
	}{
		{
			// Sep 16 < 25 → cycle started Aug 25; 22 days elapsed.
			name:          "mid cycle before cycle day",
			now:           date(2026, time.September, 16),
			wantCurStart:  date(2026, time.August, 25),
			wantCurEnd:    date(2026, time.September, 17),
			wantPrevStart: date(2026, time.July, 25),
			wantPrevEnd:   date(2026, time.August, 17),
		},
		{
			// Sep 1 < 25 → cycle started Aug 25; 7 days elapsed.
			name:          "start of calendar month still in previous cycle",
			now:           date(2026, time.September, 1),
			wantCurStart:  date(2026, time.August, 25),
			wantCurEnd:    date(2026, time.September, 2),
			wantPrevStart: date(2026, time.July, 25),
			wantPrevEnd:   date(2026, time.August, 2),
		},
		{
			// Sep 25 >= 25 → cycle just started today; 0 days elapsed.
			name:          "exactly on cycle start day",
			now:           date(2026, time.September, 25),
			wantCurStart:  date(2026, time.September, 25),
			wantCurEnd:    date(2026, time.September, 26),
			wantPrevStart: date(2026, time.August, 25),
			wantPrevEnd:   date(2026, time.August, 26),
		},
		{
			// Mar 31 >= 25 → cycle started Mar 25; 6 days elapsed.
			name:          "mid cycle after cycle day",
			now:           date(2026, time.March, 31),
			wantCurStart:  date(2026, time.March, 25),
			wantCurEnd:    date(2026, time.April, 1),
			wantPrevStart: date(2026, time.February, 25),
			wantPrevEnd:   date(2026, time.March, 4),
		},
		{
			// Jan 10 < 25 → cycle started Dec 25, 2025; rolls back across year.
			name:          "cycle spans year boundary",
			now:           date(2026, time.January, 10),
			wantCurStart:  date(2025, time.December, 25),
			wantCurEnd:    date(2026, time.January, 11),
			wantPrevStart: date(2025, time.November, 25),
			wantPrevEnd:   date(2025, time.December, 12),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			curStart, curEnd, prevStart, prevEnd := PeriodBounds(tc.now, 25)
			if !curStart.Equal(tc.wantCurStart) {
				t.Errorf("curStart = %v, want %v", curStart, tc.wantCurStart)
			}
			if !curEnd.Equal(tc.wantCurEnd) {
				t.Errorf("curEnd = %v, want %v", curEnd, tc.wantCurEnd)
			}
			if !prevStart.Equal(tc.wantPrevStart) {
				t.Errorf("prevStart = %v, want %v", prevStart, tc.wantPrevStart)
			}
			if !prevEnd.Equal(tc.wantPrevEnd) {
				t.Errorf("prevEnd = %v, want %v", prevEnd, tc.wantPrevEnd)
			}
		})
	}
}

func TestTopChanges(t *testing.T) {
	current := []hledger.Balance{
		{Account: "expenses:food", Amount: 500},
		{Account: "income:salary", Amount: -3000},
		{Account: "assets:cash", Amount: 10},
		{Account: "expenses:rent", Amount: 1200},
		{Account: "expenses:fun", Amount: 50},
		{Account: "liabilities:cc", Amount: -20},
	}
	previous := []hledger.Balance{
		{Account: "expenses:food", Amount: 400},
		{Account: "income:salary", Amount: -3000},
		{Account: "expenses:rent", Amount: 1200},
		{Account: "assets:new_account_last_month_only", Amount: 999},
	}

	got := TopChanges(current, previous, 5)
	if len(got) != 5 {
		t.Fatalf("got %d changes, want 5", len(got))
	}

	wantOrder := []string{
		"income:salary",
		"expenses:rent",
		"expenses:food",
		"expenses:fun",
		"liabilities:cc",
	}
	for i, w := range wantOrder {
		if got[i].Account != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Account, w)
		}
	}

	// spot-check diff computation for an account present in both periods
	var food Change
	for _, c := range got {
		if c.Account == "expenses:food" {
			food = c
		}
	}
	if food.Current != 500 || food.Previous != 400 || food.Diff != 100 {
		t.Errorf("expenses:food = %+v, want Current=500 Previous=400 Diff=100", food)
	}

	// account only present last month has zero current-period activity, so
	// it should rank below everything with nonzero current activity and
	// fall outside the top 5 here.
	full := TopChanges(current, previous, len(current)+len(previous))
	var onlyLastMonth Change
	for _, c := range full {
		if c.Account == "assets:new_account_last_month_only" {
			onlyLastMonth = c
		}
	}
	if onlyLastMonth.Current != 0 || onlyLastMonth.Previous != 999 {
		t.Errorf("assets:new_account_last_month_only = %+v, want Current=0 Previous=999", onlyLastMonth)
	}
}

func TestTopChangesFewerThanN(t *testing.T) {
	current := []hledger.Balance{{Account: "a", Amount: 1}}
	got := TopChanges(current, nil, 5)
	if len(got) != 1 {
		t.Fatalf("got %d changes, want 1", len(got))
	}
}

func TestFilterByPrefix(t *testing.T) {
	balances := []hledger.Balance{
		{Account: "expenses:food", Amount: 100},
		{Account: "expenses:food:groceries", Amount: 60},
		{Account: "expenses", Amount: 500},
		{Account: "expensesother:thing", Amount: 5},
		{Account: "income:salary", Amount: -3000},
	}

	got := FilterByPrefix(balances, "expenses")
	want := []string{"expenses:food", "expenses:food:groceries", "expenses"}
	if len(got) != len(want) {
		t.Fatalf("got %d balances, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Account != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Account, w)
		}
	}
}
