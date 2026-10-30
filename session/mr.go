package session

import (
	"regexp"
	"strings"
	"time"
)

// Merge requests a worker opened or pushed to. Squad picks them up from the worker's
// screen (GitLab links, "MR !123"); the conductor checks them with GitLab every
// MR-обход (pipeline, new review comments, merged) and records the state with
// cs ctl mr. When every MR of a session is merged, Squad offers to close it.

// MRRef is one merge request of a session.
type MRRef struct {
	Ref   string    `json:"ref"`             // the link, or "!123" when only the number was seen
	State string    `json:"state,omitempty"` // "" (open) | merged | closed | ignore
	Seen  time.Time `json:"seen,omitempty"`
}

// Done: nothing left to watch for this MR.
func (r MRRef) Done() bool { return r.State == "merged" || r.State == "closed" || r.State == "ignore" }

var (
	mrURLRe  = regexp.MustCompile(`https?://[^\s'"<>()\[\]` + "`" + `]+/-/merge_requests/(\d+)`)
	mrBangRe = regexp.MustCompile(`(?i)(?:\bMR|мр|merge request)\s*!(\d{3,7})\b`)
)

const maxMRs = 8

// mrIID is the number of the MR ("…/merge_requests/123" or "!123").
func mrIID(ref string) string {
	if m := mrURLRe.FindStringSubmatch(ref); m != nil {
		return m[1]
	}
	return strings.TrimPrefix(ref, "!")
}

// NoteMRs records the MRs mentioned on the screen (last lines only).
func (i *Instance) NoteMRs(plain string) bool {
	if i.Conductor || i.Assistant {
		return false
	}
	text := lastLines(plain, 200)
	changed := false
	for _, m := range mrURLRe.FindAllStringSubmatch(text, -1) {
		if i.addMR(strings.TrimRight(m[0], ".,;:"), m[1]) {
			changed = true
		}
	}
	for _, m := range mrBangRe.FindAllStringSubmatch(text, -1) {
		if i.addMR("!"+m[1], m[1]) {
			changed = true
		}
	}
	return changed
}

func (i *Instance) addMR(ref, iid string) bool {
	for n, r := range i.MRs {
		if mrIID(r.Ref) == iid {
			// A link is better than a bare number.
			if strings.HasPrefix(r.Ref, "!") && !strings.HasPrefix(ref, "!") {
				i.MRs[n].Ref = ref
				return true
			}
			return false
		}
	}
	i.MRs = append(i.MRs, MRRef{Ref: ref, Seen: time.Now()})
	if len(i.MRs) > maxMRs {
		i.MRs = i.MRs[len(i.MRs)-maxMRs:]
	}
	return true
}

// SetMRState records what the conductor found ("" / open, merged, closed, ignore);
// an MR Squad did not know is added. It returns whether every MR is done now and at
// least one was merged.
func (i *Instance) SetMRState(ref, state string) (allMerged bool) {
	if state == "open" {
		state = ""
	}
	iid := mrIID(ref)
	found := false
	for n := range i.MRs {
		if mrIID(i.MRs[n].Ref) == iid {
			i.MRs[n].State, found = state, true
			if strings.HasPrefix(i.MRs[n].Ref, "!") && !strings.HasPrefix(ref, "!") {
				i.MRs[n].Ref = ref
			}
		}
	}
	if !found {
		i.MRs = append(i.MRs, MRRef{Ref: ref, State: state, Seen: time.Now()})
	}
	merged := false
	for _, r := range i.MRs {
		if !r.Done() {
			return false
		}
		merged = merged || r.State == "merged"
	}
	return merged
}

// OpenMRs are the MRs still to watch.
func (i *Instance) OpenMRs() []string {
	var out []string
	for _, r := range i.MRs {
		if !r.Done() {
			out = append(out, r.Ref)
		}
	}
	return out
}
