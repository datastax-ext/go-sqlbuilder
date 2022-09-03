// Copyright 2018 Huan Du. All rights reserved.
// Licensed under the MIT license that can be found in the LICENSE file.

package sqlbuilder

import (
	"bytes"
	"math"
	"reflect"
	"regexp"
)

var (
	// DBTag is the struct tag to describe the name for a field in struct.
	DBTag = "db"

	// FieldTag is the struct tag to describe the tag name for a field in struct.
	// Use "," to separate different tags.
	FieldTag = "fieldtag"

	// FieldOpt is the options for a struct field.
	// As db column can contain "," in theory, field options should be provided in a separated tag.
	FieldOpt = "fieldopt"

	// FieldAs is the column alias (AS) for a struct field.
	FieldAs = "fieldas"
)

const (
	fieldOptWithQuote = "withquote"
	fieldOptOmitEmpty = "omitempty"

	optName   = "optName"
	optParams = "optParams"
)

var optRegex = regexp.MustCompile(`(?P<` + optName + `>\w+)(\((?P<` + optParams + `>.*)\))?`)

// Struct represents a struct type.
//
// All methods in Struct are thread-safe.
// We can define a global variable to hold a Struct and use it in any goroutine.
type Struct struct {
	Flavor Flavor

	structType         reflect.Type
	structFieldsParser structFieldsParser
	structTag          string
}

var emptyStruct Struct

// NewStruct analyzes type information in structValue
// and creates a new Struct with all structValue fields.
// If structValue is not a struct, NewStruct returns a dummy Struct.
func NewStruct(structValue interface{}) *Struct {
	t := reflect.TypeOf(structValue)
	t = dereferencedType(t)

	if t.Kind() != reflect.Struct {
		return &emptyStruct
	}

	return &Struct{
		Flavor:             DefaultFlavor,
		structType:         t,
		structFieldsParser: makeDefaultFieldsParser(t),
	}
}

// For sets the default flavor of s and returns a shadow copy of s.
// The original s.Flavor is not changed.
func (s *Struct) For(flavor Flavor) *Struct {
	c := *s
	c.Flavor = flavor
	return &c
}

// WithFieldMapper returns a new Struct based on s with custom field mapper.
// The original s is not changed.
func (s *Struct) WithFieldMapper(mapper FieldMapperFunc) *Struct {
	if s.structType == nil {
		return &emptyStruct
	}

	c := *s
	c.structFieldsParser = makeCustomFieldsParser(s.structType, mapper)
	return &c
}

// WithTag sets default tag for all builder methods.
// For instance, calling s.WithTag("tag").SelectFrom("t") is the same as calling s.SelectFromForTag("t", "tag").
func (s *Struct) WithTag(tag string) *Struct {
	c := *s
	c.structTag = tag
	return &c
}

// SelectFrom creates a new `SelectBuilder` with table name.
// By default, all exported fields of the s are listed as columns in SELECT.
//
// Caller is responsible to set WHERE condition to find right record.
func (s *Struct) SelectFrom(table string) *SelectBuilder {
	return s.SelectFromForTag(table, s.structTag)
}

// SelectFromForTag creates a new `SelectBuilder` with table name for a specified tag.
// By default, all fields of the s tagged with tag are listed as columns in SELECT.
//
// Caller is responsible to set WHERE condition to find right record.
func (s *Struct) SelectFromForTag(table string, tag string) (sb *SelectBuilder) {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	sb = s.Flavor.NewSelectBuilder()
	sb.From(table)

	if tagged == nil {
		sb.Select("*")
		return
	}

	buf := &bytes.Buffer{}
	cols := make([]string, 0, len(tagged.ForRead))

	for _, sf := range tagged.ForRead {
		buf.WriteString(table)
		buf.WriteRune('.')
		buf.WriteString(sf.NameForSelect(s.Flavor))

		cols = append(cols, buf.String())
		buf.Reset()
	}

	sb.Select(cols...)
	return sb
}

// Update creates a new `UpdateBuilder` with table name.
// By default, all exported fields of the s is assigned in UPDATE with the field values from value.
// If value's type is not the same as that of s, Update returns a dummy `UpdateBuilder` with table name.
//
// Caller is responsible to set WHERE condition to match right record.
func (s *Struct) Update(table string, value interface{}) *UpdateBuilder {
	return s.UpdateForTag(table, s.structTag, value)
}

// UpdateForTag creates a new `UpdateBuilder` with table name.
// By default, all fields of the s tagged with tag is assigned in UPDATE with the field values from value.
// If value's type is not the same as that of s, UpdateForTag returns a dummy `UpdateBuilder` with table name.
//
// Caller is responsible to set WHERE condition to match right record.
func (s *Struct) UpdateForTag(table string, tag string, value interface{}) *UpdateBuilder {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	ub := s.Flavor.NewUpdateBuilder()
	ub.Update(table)

	if tagged == nil {
		return ub
	}

	v := reflect.ValueOf(value)
	v = dereferencedValue(v)

	if v.Type() != s.structType {
		return ub
	}

	assignments := make([]string, 0, len(tagged.ForWrite))

	for _, sf := range tagged.ForWrite {
		name := sf.Name
		val := v.FieldByName(name)

		if isEmptyValue(val) {
			if sf.ShouldOmitEmpty("", tag) {
				continue
			}
		} else {
			val = dereferencedValue(val)
		}

		data := val.Interface()
		assignments = append(assignments, ub.Assign(sf.Quote(s.Flavor), data))
	}

	ub.Set(assignments...)
	return ub
}

// InsertInto creates a new `InsertBuilder` with table name using verb INSERT INTO.
// By default, all exported fields of s are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// InsertInto never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) InsertInto(table string, value ...interface{}) *InsertBuilder {
	return s.InsertIntoForTag(table, s.structTag, value...)
}

// InsertIgnoreInto creates a new `InsertBuilder` with table name using verb INSERT IGNORE INTO.
// By default, all exported fields of s are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// InsertIgnoreInto never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) InsertIgnoreInto(table string, value ...interface{}) *InsertBuilder {
	return s.InsertIgnoreIntoForTag(table, s.structTag, value...)
}

// ReplaceInto creates a new `InsertBuilder` with table name using verb REPLACE INTO.
// By default, all exported fields of s are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// ReplaceInto never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) ReplaceInto(table string, value ...interface{}) *InsertBuilder {
	return s.ReplaceIntoForTag(table, s.structTag, value...)
}

// buildColsAndValuesForTag uses ib to set exported fields tagged with tag as columns
// and add value as a list of values.
func (s *Struct) buildColsAndValuesForTag(ib *InsertBuilder, tag string, value ...interface{}) {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	if tagged == nil {
		return
	}

	vs := make([]reflect.Value, 0, len(value))

	for _, item := range value {
		v := reflect.ValueOf(item)
		v = dereferencedValue(v)

		if v.Type() == s.structType {
			vs = append(vs, v)
		}
	}

	if len(vs) == 0 {
		return
	}

	cols := make([]string, 0, len(tagged.ForWrite))
	values := make([][]interface{}, len(vs))
	nilCols := make([]int, 0, len(tagged.ForWrite))

	for _, sf := range tagged.ForWrite {
		cols = append(cols, sf.Quote(s.Flavor))
		name := sf.Name
		shouldOmitEmpty := sf.ShouldOmitEmpty("", tag)
		nilCnt := 0

		for i, v := range vs {
			val := v.FieldByName(name)

			if isEmptyValue(val) && shouldOmitEmpty {
				nilCnt++
			}

			val = dereferencedValue(val)

			if val.IsValid() {
				values[i] = append(values[i], val.Interface())
			} else {
				values[i] = append(values[i], nil)
			}
		}

		nilCols = append(nilCols, nilCnt)
	}

	// Try to filter out nil values if possible.
	filteredCols := make([]string, 0, len(cols))
	filteredValues := make([][]interface{}, len(values))

	for i, cnt := range nilCols {
		// If all values are nil in a column, ignore the column completely.
		if cnt == len(values) {
			continue
		}

		filteredCols = append(filteredCols, cols[i])

		for n, value := range values {
			filteredValues[n] = append(filteredValues[n], value[i])
		}
	}

	ib.Cols(filteredCols...)

	for _, value := range filteredValues {
		ib.Values(value...)
	}
}

// InsertIntoForTag creates a new `InsertBuilder` with table name using verb INSERT INTO.
// By default, exported fields tagged with tag are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// InsertIntoForTag never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) InsertIntoForTag(table string, tag string, value ...interface{}) *InsertBuilder {
	ib := s.Flavor.NewInsertBuilder()
	ib.InsertInto(table)

	s.buildColsAndValuesForTag(ib, tag, value...)
	return ib
}

// InsertIgnoreIntoForTag creates a new `InsertBuilder` with table name using verb INSERT IGNORE INTO.
// By default, exported fields tagged with tag are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// InsertIgnoreIntoForTag never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) InsertIgnoreIntoForTag(table string, tag string, value ...interface{}) *InsertBuilder {
	ib := s.Flavor.NewInsertBuilder()
	ib.InsertIgnoreInto(table)

	s.buildColsAndValuesForTag(ib, tag, value...)
	return ib
}

// ReplaceIntoForTag creates a new `InsertBuilder` with table name using verb REPLACE INTO.
// By default, exported fields tagged with tag are set as columns by calling `InsertBuilder#Cols`,
// and value is added as a list of values by calling `InsertBuilder#Values`.
//
// ReplaceIntoForTag never returns any error.
// If the type of any item in value is not expected, it will be ignored.
// If value is an empty slice, `InsertBuilder#Values` will not be called.
func (s *Struct) ReplaceIntoForTag(table string, tag string, value ...interface{}) *InsertBuilder {
	ib := s.Flavor.NewInsertBuilder()
	ib.ReplaceInto(table)

	s.buildColsAndValuesForTag(ib, tag, value...)
	return ib
}

// DeleteFrom creates a new `DeleteBuilder` with table name.
//
// Caller is responsible to set WHERE condition to match right record.
func (s *Struct) DeleteFrom(table string) *DeleteBuilder {
	db := s.Flavor.NewDeleteBuilder()
	db.DeleteFrom(table)
	return db
}

// Addr takes address of all exported fields of the s from the st.
// The returned result can be used in `Row#Scan` directly.
func (s *Struct) Addr(st interface{}) []interface{} {
	return s.AddrForTag(s.structTag, st)
}

// AddrForTag takes address of all fields of the s tagged with tag from the st.
// The returned value can be used in `Row#Scan` directly.
//
// If tag is not defined in s in advance, returns nil.
func (s *Struct) AddrForTag(tag string, st interface{}) []interface{} {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	if tagged == nil {
		return nil
	}

	return s.addrWithFields(tagged.ForRead, st)
}

// AddrWithCols takes address of all columns defined in cols from the st.
// The returned value can be used in `Row#Scan` directly.
func (s *Struct) AddrWithCols(cols []string, st interface{}) []interface{} {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(s.structTag)

	if tagged == nil {
		return nil
	}

	fields := tagged.Cols(cols)

	if fields == nil {
		return nil
	}

	return s.addrWithFields(fields, st)
}

func (s *Struct) addrWithFields(fields []*structField, st interface{}) []interface{} {
	v := reflect.ValueOf(st)
	v = dereferencedValue(v)

	if v.Type() != s.structType {
		return nil
	}

	addrs := make([]interface{}, 0, len(fields))

	for _, sf := range fields {
		name := sf.Name
		data := v.FieldByName(name).Addr().Interface()
		addrs = append(addrs, data)
	}

	return addrs
}

// Columns returns column names of s for all exported struct fields.
func (s *Struct) Columns() []string {
	return s.ColumnsForTag(s.structTag)
}

// ColumnsForTag returns column names of the s tagged with tag.
func (s *Struct) ColumnsForTag(tag string) (cols []string) {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	if tagged == nil {
		return
	}

	cols = make([]string, 0, len(tagged.ForWrite))

	for _, sf := range tagged.ForWrite {
		cols = append(cols, sf.Alias)
	}

	return
}

// Values returns a shadow copy of all exported fields in st.
func (s *Struct) Values(st interface{}) []interface{} {
	return s.ValuesForTag(s.structTag, st)
}

// ValuesForTag returns a shadow copy of all fields tagged with tag in st.
func (s *Struct) ValuesForTag(tag string, value interface{}) (values []interface{}) {
	sfs := s.structFieldsParser()
	tagged := sfs.Tag(tag)

	if tagged == nil {
		return
	}

	v := reflect.ValueOf(value)
	v = dereferencedValue(v)

	if v.Type() != s.structType {
		return
	}

	values = make([]interface{}, 0, len(tagged.ForWrite))

	for _, sf := range tagged.ForWrite {
		name := sf.Name
		data := v.FieldByName(name).Interface()
		values = append(values, data)
	}

	return
}

func dereferencedType(t reflect.Type) reflect.Type {
	for k := t.Kind(); k == reflect.Ptr || k == reflect.Interface; k = t.Kind() {
		t = t.Elem()
	}

	return t
}

func dereferencedValue(v reflect.Value) reflect.Value {
	for k := v.Kind(); k == reflect.Ptr || k == reflect.Interface; k = v.Kind() {
		v = v.Elem()
	}

	return v
}

// isEmptyValue checks if v is zero.
// Following code is borrowed from `IsZero` method in `reflect.Value` since Go 1.13.
func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(v.Float()) == 0
	case reflect.Complex64, reflect.Complex128:
		c := v.Complex()
		return math.Float64bits(real(c)) == 0 && math.Float64bits(imag(c)) == 0
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isEmptyValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		return v.IsNil()
	case reflect.String:
		return v.Len() == 0
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !isEmptyValue(v.Field(i)) {
				return false
			}
		}
		return true
	}

	return false
}
