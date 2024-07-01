package util

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"time"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz")

func Randstring(n int) string {
	rand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func StructMap(s any) map[string]any {
	out := map[string]any{}
	typ := reflect.TypeOf(s)
	struc := reflect.ValueOf(s)
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		struc = struc.Elem()
	}
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		out[name] = struc.FieldByName(name).Interface()
	}
	return out
}

func SetField(obj any, name string, value any) error {
	structValue := reflect.ValueOf(obj).Elem()
	structFieldValue := structValue.FieldByName(name)

	if !structFieldValue.IsValid() {
		return fmt.Errorf("no such field: %s in obj", name)
	}

	if !structFieldValue.CanSet() {
		return fmt.Errorf("cannot set %s field value", name)
	}

	structFieldType := structFieldValue.Type()
	val := reflect.ValueOf(value)
	if structFieldType != val.Type() {
		return errors.New("provided value type didn't match obj field type")
	}

	structFieldValue.Set(val)
	return nil
}

func FillStruct(s any, m map[string]any) error {
	for k, v := range m {
		err := SetField(s, k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func LastNonEmptyLine(out []byte) string {
	lines := strings.Split(string(out), "\n")
	offset := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			offset = len(lines) - i
			break
		}
	}
	line := lines[len(lines)-offset]
	return line
}
