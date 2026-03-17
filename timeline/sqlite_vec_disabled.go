//go:build nosqlitevec

package timeline

import "errors"

func sqliteVecAuto() {}

func sqliteVecSerializeFloat32(_ []float32) ([]byte, error) {
	return nil, errors.New("sqlite-vec is disabled (build tag nosqlitevec)")
}
