package koanf

import (
	"encoding"
	"errors"
	"fmt"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/maps"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidSpecification indicates that a specification is of the wrong type.
var ErrInvalidSpecification = errors.New("specification must be a struct pointer")

// Unmarshal2 unmarshals a given key path into the given struct using
// the mapstructure lib. If no path is specified, the whole map is unmarshalled.
// `koanf` is the struct field tag used to match field names. To customize,
// use UnmarshalWithConf(). It uses the mitchellh/mapstructure package.
func (ko *Koanf) Unmarshal2(path string, o interface{}) error {
	return ko.UnmarshalWithConf(path, o, UnmarshalConf{})
}

// UnmarshalWithConf2 is like Unmarshal but takes configuration params in UnmarshalConf.
// See mitchellh/mapstructure's DecoderConfig for advanced customization
// of the unmarshal behaviour.
func (ko *Koanf) UnmarshalWithConf2(path string, o interface{}, c UnmarshalConf) error {
	if c.DecoderConfig == nil {
		c.DecoderConfig = &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				textUnmarshalerHookFunc()),
			Metadata:         nil,
			Result:           o,
			WeaklyTypedInput: true,
		}
	}

	if c.Tag == "" {
		c.DecoderConfig.TagName = "koanf"
	} else {
		c.DecoderConfig.TagName = c.Tag
	}

	d, err := mapstructure.NewDecoder(c.DecoderConfig)
	if err != nil {
		return err
	}

	// Unmarshal using flat key paths.
	mp := ko.Get(path)
	if c.FlatPaths {
		if f, ok := mp.(map[string]interface{}); ok {
			fmp, _ := maps.Flatten(f, nil, ko.conf.Delim)
			mp = fmp
		}
	}

	err = d.Decode(mp)

	if err != nil {
		return err
	}

	err = ko.processStruct("", o)
	return err
}

// --- copied and adapted from envconfig

// Decoder has the same semantics as Setter, but takes higher precedence.
// It is provided for historical compatibility.
type Decoder interface {
	Decode(value string) error
}

// Setter is implemented by types can self-deserialize values.
// Any type that implements flag.Value also implements Setter.
type Setter interface {
	Set(value string) error
}

// varInfo maintains information about the configuration variable
type varInfo struct {
	Name  string
	Alt   string
	Key   string
	Field reflect.Value
	Tags  reflect.StructTag
}

func interfaceFrom(field reflect.Value, fn func(interface{}, *bool)) {
	// it may be impossible for a struct field to fail this check
	if !field.CanInterface() {
		return
	}
	var ok bool
	fn(field.Interface(), &ok)
	if !ok && field.CanAddr() {
		fn(field.Addr().Interface(), &ok)
	}
}

func decoderFrom(field reflect.Value) (d Decoder) {
	interfaceFrom(field, func(v interface{}, ok *bool) { d, *ok = v.(Decoder) })
	return d
}

func setterFrom(field reflect.Value) (s Setter) {
	interfaceFrom(field, func(v interface{}, ok *bool) { s, *ok = v.(Setter) })
	return s
}

func textUnmarshaler(field reflect.Value) (t encoding.TextUnmarshaler) {
	interfaceFrom(field, func(v interface{}, ok *bool) { t, *ok = v.(encoding.TextUnmarshaler) })
	return t
}

func binaryUnmarshaler(field reflect.Value) (b encoding.BinaryUnmarshaler) {
	interfaceFrom(field, func(v interface{}, ok *bool) { b, *ok = v.(encoding.BinaryUnmarshaler) })
	return b
}

func isTrue(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

// A ParseError occurs when an environment variable cannot be converted to
// the type required by a struct field during assignment.
type ParseError struct {
	KeyName   string
	FieldName string
	TypeName  string
	Value     string
	Err       error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf(
		"Process: assigning %[1]s to %[2]s: converting '%[3]s' to type %[4]s. details: %[5]s",
		e.KeyName,
		e.FieldName,
		e.Value,
		e.TypeName,
		e.Err)
}

func (ko *Koanf) processStruct(prefix string, spec interface{}) error {
	infos, err := gatherInfo(prefix, spec)

	for _, info := range infos {
		value := ko.String(info.Key)
		ok := ko.Exists(info.Key)

		def := info.Tags.Get("default")
		if def != "" && !ok {
			value = def
		}

		req := info.Tags.Get("required")
		if !ok && def == "" {
			if isTrue(req) {
				key := info.Key
				if info.Alt != "" {
					key = info.Alt
				}
				return fmt.Errorf("required key %s missing value", key)
			}
			continue
		}

		err = processField(value, info.Field)
		if err != nil {
			return &ParseError{
				KeyName:   info.Key,
				FieldName: info.Name,
				TypeName:  info.Field.Type().String(),
				Value:     value,
				Err:       err,
			}
		}
	}

	return err
}

func processField(value string, field reflect.Value) error {
	typ := field.Type()

	decoder := decoderFrom(field)
	if decoder != nil {
		return decoder.Decode(value)
	}
	// look for Set method if Decode not defined
	setter := setterFrom(field)
	if setter != nil {
		return setter.Set(value)
	}

	if t := textUnmarshaler(field); t != nil {
		return t.UnmarshalText([]byte(value))
	}

	if b := binaryUnmarshaler(field); b != nil {
		return b.UnmarshalBinary([]byte(value))
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		if field.IsNil() {
			field.Set(reflect.New(typ))
		}
		field = field.Elem()
	}

	switch typ.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var (
			val int64
			err error
		)
		if field.Kind() == reflect.Int64 && typ.PkgPath() == "time" && typ.Name() == "Duration" {
			var d time.Duration
			d, err = time.ParseDuration(value)
			val = int64(d)
		} else {
			val, err = strconv.ParseInt(value, 0, typ.Bits())
		}
		if err != nil {
			return err
		}

		field.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(value, 0, typ.Bits())
		if err != nil {
			return err
		}
		field.SetUint(val)
	case reflect.Bool:
		val, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(value, typ.Bits())
		if err != nil {
			return err
		}
		field.SetFloat(val)
	case reflect.Slice:
		sl := reflect.MakeSlice(typ, 0, 0)
		if typ.Elem().Kind() == reflect.Uint8 {
			sl = reflect.ValueOf([]byte(value))
		} else if strings.TrimSpace(value) != "" {
			vals := strings.Split(value, ",")
			sl = reflect.MakeSlice(typ, len(vals), len(vals))
			for i, val := range vals {
				err := processField(val, sl.Index(i))
				if err != nil {
					return err
				}
			}
		}
		field.Set(sl)
	case reflect.Map:
		mp := reflect.MakeMap(typ)
		if strings.TrimSpace(value) != "" {
			pairs := strings.Split(value, ",")
			for _, pair := range pairs {
				kvpair := strings.Split(pair, ":")
				if len(kvpair) != 2 {
					return fmt.Errorf("invalid map item: %q", pair)
				}
				k := reflect.New(typ.Key()).Elem()
				err := processField(kvpair[0], k)
				if err != nil {
					return err
				}
				v := reflect.New(typ.Elem()).Elem()
				err = processField(kvpair[1], v)
				if err != nil {
					return err
				}
				mp.SetMapIndex(k, v)
			}
		}
		field.Set(mp)
	}

	return nil
}

// GatherInfo gathers information about the specified struct
func gatherInfo(prefix string, spec interface{}) ([]varInfo, error) {
	s := reflect.ValueOf(spec)

	if s.Kind() != reflect.Ptr {
		return nil, ErrInvalidSpecification
	}
	s = s.Elem()
	if s.Kind() != reflect.Struct {
		return nil, ErrInvalidSpecification
	}
	typeOfSpec := s.Type()

	// over allocate an info array, we will extend if needed later
	infos := make([]varInfo, 0, s.NumField())
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		ftype := typeOfSpec.Field(i)
		if !f.CanSet() || isTrue(ftype.Tag.Get("ignored")) {
			continue
		}

		for f.Kind() == reflect.Ptr {
			if f.IsNil() {
				if f.Type().Elem().Kind() != reflect.Struct {
					// nil pointer to a non-struct: leave it alone
					break
				}
				// nil pointer to struct: create a zero instance
				f.Set(reflect.New(f.Type().Elem()))
			}
			f = f.Elem()
		}

		// Capture information about the config variable
		info := varInfo{
			Name:  ftype.Name,
			Field: f,
			Tags:  ftype.Tag,
			Alt:   strings.ToUpper(ftype.Tag.Get("envconfig")),
		}

		// Default to the field name as the env var name (will be upcased)
		info.Key = info.Name

		if info.Alt != "" {
			info.Key = info.Alt
		}
		if prefix != "" {
			info.Key = fmt.Sprintf("%s_%s", prefix, info.Key)
		}
		info.Key = strings.ToUpper(info.Key)
		infos = append(infos, info)

		if f.Kind() == reflect.Struct {
			// honor Decode if present
			if decoderFrom(f) == nil && setterFrom(f) == nil && textUnmarshaler(f) == nil && binaryUnmarshaler(f) == nil {
				innerPrefix := prefix
				if !ftype.Anonymous {
					innerPrefix = info.Key
				}

				embeddedPtr := f.Addr().Interface()
				embeddedInfos, err := gatherInfo(innerPrefix, embeddedPtr)
				if err != nil {
					return nil, err
				}
				infos = append(infos[:len(infos)-1], embeddedInfos...)

				continue
			}
		}
	}
	return infos, nil
}

func IsEmptyValue(e reflect.Value) bool {
	is_show_checking := false
	var checking_type string
	is_empty := true
	switch e.Type().Kind() {
	case reflect.String:
		checking_type = "string"
		if e.String() != "" {
			// fmt.Println("Empty string")
			is_empty = false
		}
	case reflect.Array:
		checking_type = "array"
		for j := e.Len() - 1; j >= 0; j-- {
			is_empty = IsEmptyValue(e.Index(j))
			if is_empty == false {
				break
			}
			/*if e.Index(j).Float() != 0 {
			    // fmt.Println("Empty float")
			    is_empty = false
			    break
			}*/
		}
	case reflect.Float32, reflect.Float64:
		checking_type = "float"
		if e.Float() != 0 {
			is_empty = false
		}
	case reflect.Int32, reflect.Int64:
		checking_type = "int"
		if e.Int() != 0 {
			is_empty = false

		}
	case reflect.Ptr:
		checking_type = "Ptr"
		if e.Pointer() != 0 {
			is_empty = false
		}
	case reflect.Struct:
		checking_type = "struct"
		for i := e.NumField() - 1; i >= 0; i-- {
			is_empty = IsEmptyValue(e.Field(i))
			// fmt.Println(e.Field(i).Type().Kind())
			if !is_empty {
				break
			}
		}
	default:
		checking_type = "default"
		// is_empty = IsEmptyStruct(e)
	}
	if is_show_checking {
		fmt.Println("Checking type :", checking_type, e.Type().Kind())
	}
	return is_empty
}
