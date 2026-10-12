package main

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"text/tabwriter"
	"time"
)

// errorKey is the group of the requests that failed or whose body broke.
const errorKey = "error"

// stat summarizes the results of one group: a response status or errorKey.
type stat struct {
	key           string
	count         int
	bytes         int64
	p50, p95, p99 time.Duration
	slowest       time.Duration
}

// summarize groups the results by status, failed requests apart, and returns
// the groups ordered by status with the failed ones last.
func summarize(results []result) []stat {
	groups := make(map[string][]result)
	for _, r := range results {
		key := strconv.Itoa(r.status)
		if r.err != nil {
			key = errorKey
		}
		groups[key] = append(groups[key], r)
	}

	stats := make([]stat, 0, len(groups))
	for key, group := range groups {
		latencies := make([]time.Duration, len(group))
		s := stat{key: key, count: len(group)}
		for i, r := range group {
			latencies[i] = r.latency
			s.bytes += r.bytes
		}
		slices.Sort(latencies)
		s.p50 = percentile(latencies, 50)
		s.p95 = percentile(latencies, 95)
		s.p99 = percentile(latencies, 99)
		s.slowest = latencies[len(latencies)-1]
		stats = append(stats, s)
	}
	slices.SortFunc(stats, func(a, b stat) int {
		// errorKey sorts after the digits of the statuses.
		return cmp.Compare(a.key, b.key)
	})
	return stats
}

// percentile returns the nearest-rank percentile p (0 < p <= 100) of the
// sorted latencies, which must not be empty.
func percentile(sorted []time.Duration, p int) time.Duration {
	rank := (p*len(sorted) + 99) / 100
	return sorted[max(rank, 1)-1]
}

// report writes a table of the results per status and a total line.
func report(w io.Writer, results []result, elapsed time.Duration) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "status\trequests\tp50 ms\tp95 ms\tp99 ms\tmax ms\tMiB\t")
	for _, s := range summarize(results) {
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%.1f\t\n",
			s.key, s.count, ms(s.p50), ms(s.p95), ms(s.p99), ms(s.slowest), float64(s.bytes)/(1<<20))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	rps := 0.0
	if elapsed > 0 {
		rps = float64(len(results)) / elapsed.Seconds()
	}
	if _, err := fmt.Fprintf(w, "%d requests in %s, %.2f req/s\n", len(results), elapsed.Round(time.Millisecond), rps); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	for _, r := range results {
		if r.err != nil {
			if _, err := fmt.Fprintf(w, "first error: %v\n", r.err); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			break
		}
	}
	return nil
}

// ms formats a duration in milliseconds with one decimal.
func ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64)
}
