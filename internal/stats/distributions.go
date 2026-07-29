package stats

import (
	"math"
)

// normalCDF via erfc — numerically stable in the tails.
// A naive Taylor series loses all precision past |z| ≈ 6, which matters
// because large experiments routinely produce z-scores of 8+.
func normalCDF(z float64) float64 {
	return 0.5 * math.Erfc(-z/math.Sqrt2)
}

// normalQuantile: inverse normal CDF via Acklam's rational approximation.
// |error| < 1.15e-9 over the full (0,1) range.
func normalQuantile(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}
	if p == 0.5 {
		return 0
	}

	// Coefficients for Acklam's approximation
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}

	pLow := 0.02425
	pHigh := 1 - pLow

	var x float64
	switch {
	case p < pLow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= pHigh:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r+a[5])*q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	return x
}

// chiSquareCDF via the regularized lower incomplete gamma function.
// Uses the continued fraction representation for the upper tail.
func chiSquareCDF(x float64, df int) float64 {
	if x <= 0 {
		return 0
	}
	return regularizedGammaP(float64(df)/2, x/2)
}

// regularizedGammaP: P(a, x) = lower incomplete gamma / Gamma(a)
func regularizedGammaP(a, x float64) float64 {
	if x < 0 {
		return 0
	}
	if x == 0 {
		return 0
	}
	if x < a+1 {
		return gammaSeries(a, x)
	}
	return 1 - gammaContinuedFraction(a, x)
}

func gammaSeries(a, x float64) float64 {
	const maxIter = 200
	const eps = 3e-7
	if x == 0 {
		return 0
	}
	ap := a
	del := 1.0 / a
	sum := del
	for i := 0; i < maxIter; i++ {
		ap++
		del *= x / ap
		sum += del
		if math.Abs(del) < math.Abs(sum)*eps {
			break
		}
	}
	return sum * math.Exp(-x+a*math.Log(x)-lgamma(a))
}

func gammaContinuedFraction(a, x float64) float64 {
	const maxIter = 200
	const eps = 3e-7
	const fpmin = 1e-300
	b := x + 1 - a
	c := 1.0 / fpmin
	d := 1.0 / b
	h := d
	for i := 1; i <= maxIter; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		d = an*d + b
		if math.Abs(d) < fpmin {
			d = fpmin
		}
		c = b + an/c
		if math.Abs(c) < fpmin {
			c = fpmin
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return math.Exp(-x+a*math.Log(x)-lgamma(a)) * h
}

func lgamma(x float64) float64 {
	lg, _ := math.Lgamma(x)
	return lg
}
