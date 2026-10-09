package store

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"strconv"

	"github.com/rm4n0s/errors"
)

// jsonCol is a column that holds T as JSON text and is never null: a nil slice
// is stored as [] and a nil map as {}.
type jsonCol[T any] struct{ V T }

// GormDataType tells gorm to treat the field as a text column.
func (jsonCol[T]) GormDataType() string { return "text" }

// Value implements driver.Valuer.
func (j jsonCol[T]) Value() (driver.Value, error) {
	b, err := json.Marshal(j.V)
	if err != nil {
		return nil, storeFailure(err, "encode")
	}
	if string(b) == "null" {
		if reflect.TypeFor[T]().Kind() == reflect.Slice {
			return "[]", nil
		}
		return "{}", nil
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (j *jsonCol[T]) Scan(src any) error {
	text, ok := jsonText(src)
	if !ok {
		var zero T
		j.V = zero
		return nil
	}
	if err := json.Unmarshal([]byte(text), &j.V); err != nil {
		return storeFailure(err, "decode")
	}
	return nil
}

// jsonOpt is a nullable column that holds *T as JSON text: nil is NULL.
type jsonOpt[T any] struct{ V *T }

// GormDataType tells gorm to treat the field as a text column.
func (jsonOpt[T]) GormDataType() string { return "text" }

// Value implements driver.Valuer.
func (j jsonOpt[T]) Value() (driver.Value, error) {
	if j.V == nil {
		return nil, nil //nolint:nilnil // a nil value is how driver.Valuer writes NULL
	}
	b, err := json.Marshal(j.V)
	if err != nil {
		return nil, storeFailure(err, "encode")
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (j *jsonOpt[T]) Scan(src any) error {
	text, ok := jsonText(src)
	if !ok {
		j.V = nil
		return nil
	}
	var v T
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return storeFailure(err, "decode")
	}
	j.V = &v
	return nil
}

// jsonText returns the text of a database value, and false for NULL.
func jsonText(src any) (string, bool) {
	switch v := src.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	}
	return "", false
}

// uintCol is a uint64 stored as decimal text, because SQLite integers are
// signed 64-bit; it is NULL when nil.
type uintCol struct{ V *uint64 }

// GormDataType tells gorm to treat the field as a text column.
func (uintCol) GormDataType() string { return "text" }

// Value implements driver.Valuer.
func (u uintCol) Value() (driver.Value, error) {
	if u.V == nil {
		return nil, nil //nolint:nilnil // a nil value is how driver.Valuer writes NULL
	}
	return strconv.FormatUint(*u.V, 10), nil
}

// Scan implements sql.Scanner.
func (u *uintCol) Scan(src any) error {
	text, ok := jsonText(src)
	if !ok {
		u.V = nil
		return nil
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return errors.NewErr("StoreFailure", err).SetMetadata("operation", "decode seed")
	}
	u.V = &n
	return nil
}
