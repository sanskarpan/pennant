package stats

import "math"

// RunningStats implements Welford's online algorithm for numerically stable
// mean and variance. Required because E[X²] - E[X]² catastrophically loses
// precision when the mean is large relative to the variance (e.g. revenue metrics).
type RunningStats struct {
	N    int64
	Mean float64
	M2   float64 // sum of squared deviations from the running mean
}

func (r *RunningStats) Add(x float64) {
	r.N++
	delta := x - r.Mean
	r.Mean += delta / float64(r.N)
	delta2 := x - r.Mean
	r.M2 += delta * delta2
}

// Variance returns the sample variance (Bessel-corrected).
func (r *RunningStats) Variance() float64 {
	if r.N < 2 {
		return 0
	}
	return r.M2 / float64(r.N-1)
}

func (r *RunningStats) StdDev() float64 { return math.Sqrt(r.Variance()) }

func (r *RunningStats) StdError() float64 {
	if r.N == 0 {
		return 0
	}
	return r.StdDev() / math.Sqrt(float64(r.N))
}

// Merge combines two partitions' stats for parallel aggregation.
func (r *RunningStats) Merge(o RunningStats) {
	if o.N == 0 {
		return
	}
	if r.N == 0 {
		*r = o
		return
	}
	n := r.N + o.N
	delta := o.Mean - r.Mean
	mean := r.Mean + delta*float64(o.N)/float64(n)
	m2 := r.M2 + o.M2 + delta*delta*float64(r.N)*float64(o.N)/float64(n)
	r.N, r.Mean, r.M2 = n, mean, m2
}
