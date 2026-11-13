package ui

import (
	"math"
	"strconv"
	"strings"
)

// The user's work density score («плотность»): moods for cs density and the Claude
// Code status line face (cs statusline). The animated overview pets were removed in 1.38.0.

// PetMoodLabels name the twelve moods, saddest first.
var PetMoodLabels = []string{"рыдает", "очень грустно", "грустно", "так себе", "скучает", "почти норм",
	"довольна", "радуется", "очень радуется", "в восторге", "сияет", "очень-очень радостно"}

// petMoodFrom[m] is the lowest density that gives mood m.
var petMoodFrom = []float64{0, 1, 2, 3, 4, 5, 6, 8, 10, 13, 17, 21}

// PetMood maps a density score to a mood 0..11.
func PetMood(density float64) int {
	mood := 0
	for m, from := range petMoodFrom {
		if density >= from {
			mood = m
		}
	}
	return mood
}

// FormatDensity prints 5 as "5" and 10.4 as "10,4".
func FormatDensity(v float64) string {
	s := strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1)
}

// ParallelZone names the zone of parallel hours. Emerald from 15 h is the company's;
// the lower borders are a guess until t39 says them.
func ParallelZone(v float64) string {
	switch {
	case v >= 15:
		return "изумрудная"
	case v >= 10:
		return "зелёная"
	case v >= 6:
		return "жёлтая"
	}
	return "красная"
}
