package cmd

import (
	"database/sql"
	"fmt"
	"math/big"
	"reflect"
	"strconv"

	"github.com/pkg/errors"
)

type PlainData sql.RawBytes

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
func strconvErr(err error) error {
	if ne, ok := err.(*strconv.NumError); ok {
		return ne.Err
	}
	return err
}
func asBytes(buf []byte, rv reflect.Value) (b []byte, ok bool) {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(buf, rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.AppendUint(buf, rv.Uint(), 10), true
	case reflect.Float32:
		return strconv.AppendFloat(buf, rv.Float(), 'g', -1, 32), true
	case reflect.Float64:
		return strconv.AppendFloat(buf, rv.Float(), 'g', -1, 64), true
	case reflect.Bool:
		return strconv.AppendBool(buf, rv.Bool()), true
	case reflect.String:
		s := rv.String()
		return append(buf, s...), true
	}
	return
}
func asString(src any) string {
	switch v := src.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	rv := reflect.ValueOf(src)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64)
	case reflect.Float32:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 32)
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	}
	return fmt.Sprintf("%v", src)
}

var errNilPtr = errors.New("destination pointer is nil") // embedded in descriptive error

func (dest *PlainData) Scan(src interface{}) error {
	switch src.(type) {
	case *big.Rat:
		n := src.(*big.Rat)
		x, _ := n.Float64()
		tmp := strconv.FormatFloat(x, 'f', -1, 64)
		*dest = cloneBytes([]byte(tmp))
		return nil
	case []uint8:
		*dest = cloneBytes([]byte(src.([]byte)))
		return nil
	}

	dpv := reflect.ValueOf(dest)
	if dpv.Kind() != reflect.Pointer {
		return errors.New("destination not a pointer")
	}
	if dpv.IsNil() {
		return errNilPtr
	}
	var sv reflect.Value

	sv = reflect.ValueOf(src)
	if b, ok := asBytes(nil, sv); ok {
		*dest = b
		return nil
	}
	return errors.New("data type not supported")
}
