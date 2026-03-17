//go:build !nosqlitevec

package timeline

import sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

func sqliteVecAuto() {
	sqlite_vec.Auto()
}

func sqliteVecSerializeFloat32(values []float32) ([]byte, error) {
	return sqlite_vec.SerializeFloat32(values)
}
