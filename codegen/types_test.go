package codegen

import "testing"

func TestGoPrimitive(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"integer", "int64"},
		{"decimal", "runtime.Decimal"},
		{"string", "string"},
		{"boolean", "bool"},
		{"datetime", "time.Time"},
		{"bytes", "[]byte"},
		{"duration", "time.Duration"},
		{"size", "int64"},
		{"rate", "runtime.Rate"},
	}
	for _, c := range cases {
		got, ok := GoPrimitive(c.in)
		if !ok {
			t.Errorf("GoPrimitive(%q) ok = false, want true", c.in)
		}
		if got != c.want {
			t.Errorf("GoPrimitive(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if _, ok := GoPrimitive("version"); ok {
		t.Errorf("GoPrimitive(version) ok = true, want false")
	}
}

func TestGoOpaqueType(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"File", "runtime.File"},
		{"FileStream", "runtime.FileStream"},
		{"FileRef", "runtime.FileRef"},
	}
	for _, c := range cases {
		got, ok := GoOpaqueType(c.in)
		if !ok {
			t.Errorf("GoOpaqueType(%q) ok = false, want true", c.in)
		}
		if got != c.want {
			t.Errorf("GoOpaqueType(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if _, ok := GoOpaqueType("string"); ok {
		t.Errorf("GoOpaqueType(string) ok = true, want false")
	}
}

func TestGoGeneric(t *testing.T) {
	cases := []struct {
		ctor string
		args []string
		want string
	}{
		{"List", []string{"string"}, "[]string"},
		{"AppendList", []string{"StatementEntry"}, "runtime.AppendList[StatementEntry]"},
		{"Set", []string{"WalletId"}, "map[WalletId]struct{}"},
		{"Map", []string{"string", "int64"}, "map[string]int64"},
	}
	for _, c := range cases {
		got, ok := GoGeneric(c.ctor, c.args)
		if !ok {
			t.Errorf("GoGeneric(%q, %v) ok = false, want true", c.ctor, c.args)
		}
		if got != c.want {
			t.Errorf("GoGeneric(%q, %v) = %q, want %q", c.ctor, c.args, got, c.want)
		}
	}

	if _, ok := GoGeneric("Queue", []string{"T"}); ok {
		t.Errorf("GoGeneric(Queue, ...) ok = true, want false (ctor desconhecido)")
	}
	if _, ok := GoGeneric("Map", []string{"K"}); ok {
		t.Errorf("GoGeneric(Map, [K]) ok = true, want false (mismatch de contagem de args)")
	}
	if _, ok := GoGeneric("List", []string{"K", "V"}); ok {
		t.Errorf("GoGeneric(List, [K,V]) ok = true, want false (mismatch de contagem de args)")
	}
}

func TestGoBuiltinCall(t *testing.T) {
	got, ok := GoBuiltinCall("value", BuiltinMethod{Receiver: "string", Method: "length"}, nil)
	if !ok {
		t.Fatalf("GoBuiltinCall(string.length) ok = false, want true")
	}
	if want := "len(value)"; got != want {
		t.Errorf("GoBuiltinCall(string.length) = %q, want %q", got, want)
	}

	got, ok = GoBuiltinCall("state.Entries", BuiltinMethod{Receiver: "AppendList", Method: "add"}, []string{"entry"})
	if !ok {
		t.Fatalf("GoBuiltinCall(AppendList.add) ok = false, want true")
	}
	if want := "state.Entries.Add(entry)"; got != want {
		t.Errorf("GoBuiltinCall(AppendList.add) = %q, want %q", got, want)
	}

	if _, ok := GoBuiltinCall("value", BuiltinMethod{Receiver: "string", Method: "reverse"}, nil); ok {
		t.Errorf("GoBuiltinCall(string.reverse) ok = true, want false (par desconhecido)")
	}

	got, ok = GoBuiltinCall("v", BuiltinMethod{Receiver: "string", Method: "uppercase"}, nil)
	if !ok {
		t.Fatalf("GoBuiltinCall(string.uppercase) ok = false, want true")
	}
	if want := "strings.ToUpper(v)"; got != want {
		t.Errorf("GoBuiltinCall(string.uppercase) = %q, want %q", got, want)
	}

	if _, ok := GoBuiltinCall("v", BuiltinMethod{Receiver: "string", Method: "uppercase"}, []string{"demais"}); ok {
		t.Errorf("GoBuiltinCall(string.uppercase) com aridade errada: ok = true, want false")
	}
}
