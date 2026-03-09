package edit

// Affine transformation matrix for PDF coordinate transformations.
//
// PDF uses a 3×3 matrix with the last column fixed to [0, 0, 1]:
//
//   [ a  b  0 ]
//   [ c  d  0 ]
//   [ e  f  1 ]
//
// Points are row vectors transformed on the left: [x', y', 1] = [x, y, 1] × M
//
//   x' = a·x + c·y + e
//   y' = b·x + d·y + f
//
// Spec: PDF 32000-1:2008 §8.3.3
//
// Note on `cm` concatenation order:
// The cm operator performs CTM' = M_arg × CTM (pre-multiplication).
// This means the most recently applied transform (innermost cm) acts
// on coordinates first, then the earlier (outer) transforms follow.

import "math"

// Matrix represents a 2D affine transformation.
// Elements correspond to the PDF matrix [a b c d e f]:
//
//   [ A  B  0 ]
//   [ C  D  0 ]
//   [ E  F  1 ]
type Matrix struct {
	A, B, C, D, E, F float64
}

// Identity returns the identity matrix.
func Identity() Matrix {
	return Matrix{A: 1, B: 0, C: 0, D: 1, E: 0, F: 0}
}

// Translate returns a translation matrix.
func Translate(tx, ty float64) Matrix {
	return Matrix{A: 1, B: 0, C: 0, D: 1, E: tx, F: ty}
}

// Scale returns a scaling matrix.
func Scale(sx, sy float64) Matrix {
	return Matrix{A: sx, B: 0, C: 0, D: sy, E: 0, F: 0}
}

// Multiply returns left × right (pre-multiplication).
//
// For the cm operator: CTM_new = M_arg.Multiply(CTM_old)
//
// Given left = [la, lb, lc, ld, le, lf] and right = [ra, rb, rc, rd, re, rf]:
//
//   [ la lb 0 ]   [ ra rb 0 ]
//   [ lc ld 0 ] × [ rc rd 0 ]
//   [ le lf 1 ]   [ re rf 1 ]
func (left Matrix) Multiply(right Matrix) Matrix {
	return Matrix{
		A: left.A*right.A + left.B*right.C,
		B: left.A*right.B + left.B*right.D,
		C: left.C*right.A + left.D*right.C,
		D: left.C*right.B + left.D*right.D,
		E: left.E*right.A + left.F*right.C + right.E,
		F: left.E*right.B + left.F*right.D + right.F,
	}
}

// Transform applies the matrix to a point (x, y).
//
//   x' = A·x + C·y + E
//   y' = B·x + D·y + F
func (m Matrix) Transform(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E,
		m.B*x + m.D*y + m.F
}

// ScaleX returns the horizontal scale factor: sqrt(a² + b²).
func (m Matrix) ScaleX() float64 {
	return math.Sqrt(m.A*m.A + m.B*m.B)
}

// ScaleY returns the vertical scale factor: sqrt(c² + d²).
func (m Matrix) ScaleY() float64 {
	return math.Sqrt(m.C*m.C + m.D*m.D)
}

// Rotation returns the rotation angle in degrees (counter-clockwise).
func (m Matrix) Rotation() float64 {
	return math.Atan2(m.B, m.A) * 180 / math.Pi
}

// IsIdentity returns true if this is (approximately) the identity matrix.
func (m Matrix) IsIdentity() bool {
	const eps = 1e-10
	return math.Abs(m.A-1) < eps && math.Abs(m.B) < eps &&
		math.Abs(m.C) < eps && math.Abs(m.D-1) < eps &&
		math.Abs(m.E) < eps && math.Abs(m.F) < eps
}

// IsFlippedY returns true if the matrix has a negative Y scale
// (common in web-generated PDFs using screen coordinate systems).
func (m Matrix) IsFlippedY() bool {
	return m.D < 0
}

// Determinant returns the determinant of the 2×2 submatrix [a b; c d].
func (m Matrix) Determinant() float64 {
	return m.A*m.D - m.B*m.C
}

// Inverse returns the inverse matrix, or the identity if not invertible.
func (m Matrix) Inverse() Matrix {
	det := m.Determinant()
	if math.Abs(det) < 1e-15 {
		return Identity()
	}
	invDet := 1.0 / det
	return Matrix{
		A: m.D * invDet,
		B: -m.B * invDet,
		C: -m.C * invDet,
		D: m.A * invDet,
		E: (m.C*m.F - m.D*m.E) * invDet,
		F: (m.B*m.E - m.A*m.F) * invDet,
	}
}
