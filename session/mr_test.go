package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoteMRs(t *testing.T) {
	w := &Instance{Title: "w"}
	scr := "● Запушил, открыл MR !21352.\n  Готово: https://git.example.com/team/app/-/merge_requests/21352.\n● Ещё MR !21358 и summary!123\n"
	require.True(t, w.NoteMRs(scr))
	require.Len(t, w.MRs, 2)
	require.Equal(t, "https://git.example.com/team/app/-/merge_requests/21352", w.MRs[0].Ref)
	require.Equal(t, "!21358", w.MRs[1].Ref)
	require.False(t, w.NoteMRs(scr), "nothing new")
	require.False(t, w.SetMRState("!21352", "merged"))
	require.Equal(t, []string{"!21358"}, w.OpenMRs())
	require.True(t, w.SetMRState("!21358", "merged"))
	require.Empty(t, w.OpenMRs())
	c := &Instance{Conductor: true}
	require.False(t, c.NoteMRs(scr))
	o := &Instance{}
	o.NoteMRs("MR !100 ")
	require.False(t, o.SetMRState("!100", "ignore"), "nothing merged — no offer to close")
}
