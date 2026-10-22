package mailarchive

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
	"time"
)

type searchHit struct {
	row       map[string]any
	date      time.Time
	validDate bool
	path      string
}

type searchOrder struct {
	mode                string
	limit               int
	after, before       time.Time
	hasAfter, hasBefore bool
	hits                []searchHit
}

func newSearchOrder(opts SearchOptions) (*searchOrder, error) {
	o := &searchOrder{mode: opts.Sort, limit: opts.Limit, hasAfter: opts.After != "", hasBefore: opts.Before != ""}
	if o.mode != "" && o.mode != "none" && o.mode != "newest" && o.mode != "oldest" {
		return nil, errors.New("--sort must be none, newest or oldest")
	}
	parse := func(value, flag string) (time.Time, error) {
		if value == "" {
			return time.Time{}, nil
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, value); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("--%s must be YYYY-MM-DD or RFC3339", flag)
	}
	var err error
	if o.after, err = parse(opts.After, "after"); err != nil {
		return nil, err
	}
	if o.before, err = parse(opts.Before, "before"); err != nil {
		return nil, err
	}
	if o.hasAfter && o.hasBefore && !o.before.After(o.after) {
		return nil, errors.New("--before must be later than --after")
	}
	return o, nil
}

func (o *searchOrder) sorted() bool   { return o.mode == "newest" || o.mode == "oldest" }
func (o *searchOrder) filtered() bool { return o.hasAfter || o.hasBefore }
func (o *searchOrder) includes(t time.Time) bool {
	return (!o.hasAfter || !t.Before(o.after)) && (!o.hasBefore || t.Before(o.before))
}

func (o *searchOrder) earlier(a, b searchHit) bool {
	if a.validDate != b.validDate {
		return a.validDate
	}
	if a.validDate && !a.date.Equal(b.date) {
		if o.mode == "newest" {
			return a.date.After(b.date)
		}
		return a.date.Before(b.date)
	}
	return a.path < b.path
}

// The heap root is the worst retained result. A limit keeps only the best K
// headers in memory while still scanning every candidate before output.
func (o *searchOrder) Len() int           { return len(o.hits) }
func (o *searchOrder) Less(i, j int) bool { return o.earlier(o.hits[j], o.hits[i]) }
func (o *searchOrder) Swap(i, j int)      { o.hits[i], o.hits[j] = o.hits[j], o.hits[i] }
func (o *searchOrder) Push(v any)         { o.hits = append(o.hits, v.(searchHit)) }
func (o *searchOrder) Pop() any {
	i := len(o.hits) - 1
	v := o.hits[i]
	o.hits = o.hits[:i]
	return v
}
func (o *searchOrder) add(hit searchHit) {
	if o.limit == 0 || len(o.hits) < o.limit {
		heap.Push(o, hit)
		return
	}
	if o.earlier(hit, o.hits[0]) {
		o.hits[0] = hit
		heap.Fix(o, 0)
	}
}
func (o *searchOrder) results() []searchHit {
	sort.Slice(o.hits, func(i, j int) bool { return o.earlier(o.hits[i], o.hits[j]) })
	return o.hits
}
