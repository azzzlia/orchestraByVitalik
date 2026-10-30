package main

import "testing"

func TestMicroPetCoversEveryMood(t *testing.T) {
	for _, v := range []float64{0, 1, 3, 5, 6, 8, 10, 13, 17, 21, 30} {
		if microPet(v) == "" {
			t.Fatal(v)
		}
	}
}
