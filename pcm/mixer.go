package pcm

// mixer converts frame channel counts using a deterministic gain matrix.
//
// Mixing rules (documented, deterministic, no crosstalk beyond the matrix):
//  1. from == to: identity, samples pass through unchanged.
//  2. N -> 1 (downmix to mono): out = mean of all inputs, gain 1/N each.
//  3. 1 -> M (upmix from mono): the mono sample is copied to every output
//     channel with gain 1.
//  4. N -> M with N > M > 1 (downmix): input channel j feeds output
//     channel (j mod M); each output averages its assigned inputs
//     (gain 1/k where k inputs map to it).
//  5. N -> M with M > N > 1 (upmix): out[i] = in[i] for i < N (gain 1);
//     channels i >= N are silent (exactly 0).
type mixer struct {
	from, to int
	gains    [][]float64 // gains[to][from]
}

// newMixer builds the gain matrix for from->to channel conversion.
func newMixer(from, to int) *mixer {
	m := &mixer{from: from, to: to, gains: make([][]float64, to)}
	for i := range m.gains {
		m.gains[i] = make([]float64, from)
	}
	switch {
	case from == to:
		for i := 0; i < to; i++ {
			m.gains[i][i] = 1
		}
	case to == 1:
		for j := 0; j < from; j++ {
			m.gains[0][j] = 1.0 / float64(from)
		}
	case from == 1:
		for i := 0; i < to; i++ {
			m.gains[i][0] = 1
		}
	case from > to:
		count := make([]int, to)
		for j := 0; j < from; j++ {
			count[j%to]++
		}
		for j := 0; j < from; j++ {
			m.gains[j%to][j] = 1.0 / float64(count[j%to])
		}
	default: // from < to, both > 1
		for i := 0; i < from; i++ {
			m.gains[i][i] = 1
		}
		// remaining rows stay zero: silent padding channels
	}
	return m
}

// apply converts one frame of `from` channels into `to` channels.
// in has length from, out has length to.
func (m *mixer) apply(in, out []float64) {
	for i := 0; i < m.to; i++ {
		var acc float64
		row := m.gains[i]
		for j := 0; j < m.from; j++ {
			if row[j] != 0 {
				acc += row[j] * in[j]
			}
		}
		out[i] = acc
	}
}
