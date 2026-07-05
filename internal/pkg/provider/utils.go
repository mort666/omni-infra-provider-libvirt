package provider

import (
	"crypto/rand"
	"fmt"
	"reflect"
)

// Pointer provides a helper function to return a pointer to a type
func Pointer[T any](v T) *T { return &v }

func ValueAny[T any](v T) bool {
	switch m := any(v).(type) {
	case interface{ Bool() bool }:
		return m.Bool()
	case interface{ IsZero() bool }:
		return !m.IsZero()
	}
	return reflectValue(&v)
}

func reflectValue(vp any) bool {
	switch rv := reflect.ValueOf(vp).Elem(); rv.Kind() {
	case reflect.Map, reflect.Slice:
		return rv.Len() != 0
	default:
		return !rv.IsZero()
	}
}

// ValueSlice returns true if the length of the slice is greater than 0.
// Note that it does not distinguish nil slices from empty slices.
func ValueSlice[T any, S ~[]T](v S) bool {
	return len(v) > 0
}

// ValueMap returns true if the length of the map is greater than 0.
// Note that it does not distinguish nil maps from empty maps.
func ValueMap[K comparable, V any, M ~map[K]V](v M) bool {
	return len(v) > 0
}

// Value returns the truthy value of comparable types.
// Values are truthy if they are not equal to the zero value for the type.
func Value[T comparable](v T) bool {
	return v != *new(T)
}


func MacSingle() (mac string) {
	qemuOui := []byte{0x52, 0x54, 0x00, 0xAB, 0xFF, 0xFF}

	buf := make([]byte, 6)
	_, err := rand.Read(buf)
	if err != nil {
		return
	}

	if buf[3] >= qemuOui[3] {
		buf[3] = qemuOui[3] 
	} 
	if buf[4] == qemuOui[4] {
		buf[4] = qemuOui[4] - buf[2]
	} 
	if buf[5] == qemuOui[5] {
		buf[5] = qemuOui[5] - buf[1]
	} 

	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", qemuOui[0], qemuOui[1], qemuOui[2], buf[3], buf[4], buf[5])
}

func Default[T comparable](vs ...T) (t T) {
	for _, v := range vs {
		if v != *new(T) {
			return v
		}
	}
	return
}

// FirstAny returns the first value in vs which is truthy.
func DefaultAny[T any](vs ...T) (t T) {
	for _, v := range vs {
		if ValueAny(v) {
			return v
		}
	}
	return
}

// SetDefault sets p to the first non-zero value in defaults
// if it is not already non-zero.
func SetDefault[T comparable](p *T, defaults ...T) {
	if *p == *new(T) {
		*p = Default(defaults...)
	}
}

// SetDefaultAny sets p to the first truthy value in defaults
// if it is not already truthy.
func SetDefaultAny[T any](p *T, defaults ...T) {
	if !ValueAny(*p) {
		*p = DefaultAny(defaults...)
	}
}