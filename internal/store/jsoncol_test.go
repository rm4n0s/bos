package store

import (
	"math"
	"testing"

	"github.com/rm4n0s/bos/internal/domain"
	"github.com/rm4n0s/errors"
)

func TestJSONColumns(t *testing.T) {
	t.Run("a nil slice is stored as an empty list", func(t *testing.T) {
		v, err := jsonCol[[]domain.LoRA]{}.Value()
		check(t, err)
		if v != "[]" {
			t.Errorf("value = %v, want []", v)
		}
	})
	t.Run("a nil map is stored as an empty object", func(t *testing.T) {
		v, err := jsonCol[domain.OutputAttributes]{}.Value()
		check(t, err)
		if v != "{}" {
			t.Errorf("value = %v, want {}", v)
		}
	})
	t.Run("NULL reads as the zero value", func(t *testing.T) {
		var c jsonCol[[]string]
		check(t, c.Scan(nil))
		if c.V != nil {
			t.Errorf("value = %v", c.V)
		}
		var o jsonOpt[domain.IPAdapter]
		check(t, o.Scan(nil))
		if o.V != nil {
			t.Errorf("optional value = %v", o.V)
		}
	})
	t.Run("an optional value is NULL when nil", func(t *testing.T) {
		v, err := jsonOpt[domain.IPAdapter]{}.Value()
		check(t, err)
		if v != nil {
			t.Errorf("value = %v, want NULL", v)
		}
	})
	t.Run("text and bytes both scan", func(t *testing.T) {
		for _, src := range []any{`["a","b"]`, []byte(`["a","b"]`)} {
			var c jsonCol[[]string]
			check(t, c.Scan(src))
			if len(c.V) != 2 || c.V[1] != "b" {
				t.Errorf("%T scanned as %v", src, c.V)
			}
		}
	})
	t.Run("bad JSON is a store failure", func(t *testing.T) {
		var c jsonCol[[]string]
		wantRoute(t, c.Scan(`{not json`), "jsonCol", "StoreFailure")
		var o jsonOpt[domain.IPAdapter]
		wantRoute(t, o.Scan(`{not json`), "jsonOpt", "StoreFailure")
	})
}

func TestSeedColumnKeepsEveryUint64(t *testing.T) {
	for _, seed := range []uint64{0, 1, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64} {
		v, err := uintCol{V: &seed}.Value()
		check(t, err)
		var back uintCol
		check(t, back.Scan(v))
		if back.V == nil || *back.V != seed {
			t.Errorf("seed %d came back as %v", seed, back.V)
		}
	}
	var none uintCol
	check(t, none.Scan(nil))
	if none.V != nil {
		t.Errorf("NULL scanned as %v", *none.V)
	}
	err := none.Scan("-1")
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "StoreFailure" {
		t.Errorf("a bad seed gave %v", err)
	}
}
