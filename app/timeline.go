package app

import (
	"net/http"
	"sort"
	"time"

	"claude-squad/session"
)

// The «Статистика» tab: a lane per session over the last hours, built from the status
// history Squad already writes to events.log (recordStatuses). No tokens, no new state.

type tlSeg struct {
	From int64  `json:"a"` // unix ms
	To   int64  `json:"b"`
	Kind int    `json:"k"` // session.StatusKind
	Text string `json:"l,omitempty"`
}

type tlLane struct {
	Number    int     `json:"n"`
	Title     string  `json:"title"`
	Live      bool    `json:"live"` // the session still exists
	Conductor bool    `json:"conductor,omitempty"`
	Assistant bool    `json:"assistant,omitempty"`
	Kind      int     `json:"k"` // current (last) status
	Label     string  `json:"label"`
	Working   int64   `json:"work"`  // ms in the window
	WaitUser  int64   `json:"wait"`  // ms waiting for the user
	WaitCond  int64   `json:"wcond"` // ms waiting for the conductor
	Segs      []tlSeg `json:"segs"`
	LastWork  int64   `json:"lastWork"` // when it last worked in the window (0 — never)
	Asleep    bool    `json:"asleep"`   // slept (or sat idle) through the whole window
}

type tlResult struct {
	From     int64    `json:"from"`
	To       int64    `json:"to"`
	Lanes    []tlLane `json:"lanes"`
	Parallel []int    `json:"par"` // working workers per bucket
	Now      int      `json:"now"`
	Avg      float64  `json:"avg"`
	Peak     int      `json:"peak"`
}

type tlLive struct {
	number     int
	cond, asst bool
	label      string
	kind       int
}

// buildTimeline replays status events into lanes for [from, to]. live maps titles of the
// sessions that exist now; a closed session's lane ends at its last event.
func buildTimeline(events []session.Event, from, to time.Time, live map[string]tlLive) tlResult {
	type state struct {
		lane  *tlLane
		kind  int
		label string
		since time.Time
		open  bool
	}
	st := map[string]*state{}
	var order []string
	ms := func(t time.Time) int64 { return t.UnixMilli() }
	addSeg := func(s *state, end time.Time) {
		if !s.open {
			return
		}
		a, b := s.since, end
		if a.Before(from) {
			a = from
		}
		if b.After(to) {
			b = to
		}
		if !b.After(a) {
			return
		}
		d := b.Sub(a).Milliseconds()
		switch session.StatusKind(s.kind) {
		case session.StatusWorking, session.StatusStarting:
			s.lane.Working += d
		case session.StatusWaiting, session.StatusNeedsInput:
			s.lane.WaitUser += d
		case session.StatusConductor:
			s.lane.WaitCond += d
		}
		segs := s.lane.Segs
		if n := len(segs); n > 0 && segs[n-1].Kind == s.kind && segs[n-1].To == ms(a) {
			segs[n-1].To = ms(b)
		} else {
			segs = append(segs, tlSeg{From: ms(a), To: ms(b), Kind: s.kind, Text: s.label})
		}
		s.lane.Segs = segs
	}
	for _, e := range events {
		if e.Kind != "status" || e.Title == "" || e.At.After(to) {
			continue
		}
		s := st[e.Title]
		if s == nil {
			s = &state{lane: &tlLane{Title: e.Title}}
			st[e.Title] = s
			order = append(order, e.Title)
		}
		addSeg(s, e.At)
		s.kind, s.label, s.since, s.open = e.Status, e.Label, e.At, true
		if e.Number != 0 {
			s.lane.Number = e.Number
		}
	}
	res := tlResult{From: ms(from), To: ms(to)}
	for _, t := range order {
		s := st[t]
		l, isLive := live[t]
		if isLive {
			addSeg(s, to)
			s.lane.Live, s.lane.Conductor, s.lane.Assistant = true, l.cond, l.asst
			s.lane.Number, s.lane.Kind, s.lane.Label = l.number, l.kind, l.label
		} else {
			s.lane.Kind, s.lane.Label = s.kind, s.label
		}
		if len(s.lane.Segs) == 0 && !isLive {
			continue // closed before the window
		}
		if s.lane.Segs == nil {
			s.lane.Segs = []tlSeg{}
		}
		res.Lanes = append(res.Lanes, *s.lane)
	}
	// live sessions with no history yet still get a lane
	for t, l := range live {
		if _, ok := st[t]; !ok {
			res.Lanes = append(res.Lanes, tlLane{Number: l.number, Title: t, Live: true, Conductor: l.cond, Assistant: l.asst, Kind: l.kind, Label: l.label, Segs: []tlSeg{}})
		}
	}
	for i := range res.Lanes {
		l := &res.Lanes[i]
		l.Asleep = true
		for _, sg := range l.Segs {
			k := session.StatusKind(sg.Kind)
			if k == session.StatusWorking || k == session.StatusStarting {
				l.LastWork = sg.To
			}
			if k != session.StatusPaused {
				l.Asleep = false
			}
		}
		if l.Live && session.StatusKind(l.Kind) != session.StatusPaused {
			l.Asleep = false
		}
	}
	rank := func(l tlLane) int {
		switch {
		case l.Conductor:
			return 0
		case l.Assistant:
			return 1
		case l.Asleep:
			return 4
		case !l.Live:
			return 3
		}
		return 2
	}
	sort.SliceStable(res.Lanes, func(i, j int) bool {
		a, b := res.Lanes[i], res.Lanes[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.LastWork != b.LastWork { // the most recently working first
			return a.LastWork > b.LastWork
		}
		if la, lb := lastSeen(a), lastSeen(b); la != lb {
			return la > lb
		}
		return a.Number < b.Number
	})
	// how many workers were working at once
	const buckets = 120
	step := to.Sub(from) / buckets
	res.Parallel = make([]int, buckets)
	sum := 0
	for i := 0; i < buckets; i++ {
		at := ms(from.Add(step*time.Duration(i) + step/2))
		c := 0
		for _, l := range res.Lanes {
			if l.Conductor || l.Assistant {
				continue
			}
			for _, s := range l.Segs {
				if s.From <= at && at < s.To {
					if k := session.StatusKind(s.Kind); k == session.StatusWorking || k == session.StatusStarting {
						c++
					}
					break
				}
			}
		}
		res.Parallel[i] = c
		sum += c
		if c > res.Peak {
			res.Peak = c
		}
	}
	res.Avg = float64(sum) / buckets
	for _, l := range res.Lanes {
		if l.Live && !l.Conductor && !l.Assistant {
			if k := session.StatusKind(l.Kind); k == session.StatusWorking || k == session.StatusStarting {
				res.Now++
			}
		}
	}
	return res
}

func (m *home) webTimeline(w http.ResponseWriter, r *http.Request) {
	hours := formInt(r, "hours")
	if hours <= 0 || hours > 168 {
		hours = 3
	}
	to := time.Now()
	from := to.Add(-time.Duration(hours) * time.Hour)
	live := map[string]tlLive{}
	m.mu.Lock()
	for _, inst := range m.list.GetInstances() {
		label, kind := inst.StatusLabel()
		live[inst.Title] = tlLive{number: inst.Number, cond: inst.Conductor, asst: inst.Assistant, label: label, kind: int(kind)}
	}
	m.mu.Unlock()
	// a day before the window: the status each session was in when it started
	ev := session.ReadEvents(from.Add(-24 * time.Hour))
	writeJSON(w, buildTimeline(ev, from, to, live))
}

// lastSeen: when the lane last had a status other than sleeping.
func lastSeen(l tlLane) int64 {
	var t int64
	for _, sg := range l.Segs {
		if session.StatusKind(sg.Kind) != session.StatusPaused {
			t = sg.To
		}
	}
	return t
}
