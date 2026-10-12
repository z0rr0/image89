package config

import (
	"encoding"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// checkTextFields reports values of text fields, such as Duration and Color,
// that are not TOML strings. go-toml calls UnmarshalText only for strings and
// decodes any other value straight into the underlying Go type: without this
// check `timeout = 30` becomes 30ns and `color = {R = 255}` sets the RGB fields.
// value is the document decoded into generic maps, t is the Go type it maps to.
// Other type mismatches are left to the decoder, which reports their position.
func checkTextFields(value any, t reflect.Type, path string) error {
	if reflect.PointerTo(t).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		if _, ok := value.(string); !ok {
			return errors.Join(ErrDecode, fmt.Errorf("%s: got %s, want a string", path, tomlType(value)))
		}
		return nil
	}

	var errs []error
	switch t.Kind() {
	case reflect.Struct:
		table, _ := value.(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(table)) {
			if field, ok := fieldByKey(t, key); ok {
				errs = append(errs, checkTextFields(table[key], field.Type, joinKey(path, key)))
			}
		}
	case reflect.Slice, reflect.Array:
		items, _ := value.([]any)
		for i, item := range items {
			errs = append(errs, checkTextFields(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i)))
		}
	default:
	}
	return errors.Join(errs...)
}

// fieldByKey finds the field that go-toml decodes key into. go-toml compares
// strings.ToLower of the key and the tag, not strings.EqualFold: they differ
// for keys like "tİmeout" (U+0130 lowers to "i" but does not fold to it).
// Every Config field has a toml tag, and the tags are unique in lower case.
func fieldByKey(t reflect.Type, key string) (reflect.StructField, bool) {
	lowerKey := strings.ToLower(key)
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if name != "" && strings.ToLower(name) == lowerKey {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// tomlType names the TOML type of a non-string value decoded into generic maps.
func tomlType(value any) string {
	switch value.(type) {
	case int64:
		return "integer"
	case float64:
		return "float"
	case bool:
		return "boolean"
	case map[string]any:
		return "table"
	case []any:
		return "array"
	default:
		return "date or time"
	}
}
