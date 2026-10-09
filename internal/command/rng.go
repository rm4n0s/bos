package command

import "sort"

// rng is SplitMix64. It is used instead of math/rand so that `sample n seed s`
// selects the same combinations in every Go version and on every platform: a
// stored command must reproduce its selection (SPEC §6.7).
type rng struct{ state uint64 }

func (r *rng) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// below returns a uniform number in [0, n) without modulo bias.
func (r *rng) below(n uint64) uint64 {
	threshold := (0 - n) % n
	for {
		if x := r.next(); x >= threshold {
			return x % n
		}
	}
}

// sampleIndices picks k distinct numbers from [0, m) uniformly (Floyd's
// algorithm) and returns them in ascending order. All of them are returned
// when k is not smaller than m.
func sampleIndices(m, k int, seed uint64) []int {
	if k >= m {
		out := make([]int, m)
		for i := range out {
			out[i] = i
		}
		return out
	}
	r := &rng{state: seed}
	chosen := make(map[int]struct{}, k)
	for j := m - k; j < m; j++ {
		t := int(r.below(uint64(j + 1)))
		if _, taken := chosen[t]; taken {
			t = j
		}
		chosen[t] = struct{}{}
	}
	out := make([]int, 0, k)
	for i := range chosen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}
