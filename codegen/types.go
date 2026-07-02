package codegen

import "fmt"

// types.go mapeia o modelo de tipos do front-end (pacote types — primitivos e
// coleções) para as formas Go equivalentes (§design 3.3/3.6), mais a tabela de
// métodos embutidos que não são operadores de VO nem built-ins de topo. Cobre
// só primitivos e coleções (REQ-15.6); VO/Enum/Shape ficam para E3 (REQ-17/18/19).

// goPrimitives mapeia types.Primitive.Name → forma Go (§design 3.3). decimal
// nunca vira float64 (dinheiro exato); duration/size/rate são os literais
// especializados do spec.
var goPrimitives = map[string]string{
	"integer":  "int64",
	"decimal":  "runtime.Decimal",
	"string":   "string",
	"boolean":  "bool",
	"datetime": "time.Time",
	"bytes":    "[]byte",
	"duration": "time.Duration",
	"size":     "int64",
	"rate":     "runtime.Rate",
}

// GoPrimitive mapeia o Name de um *types.Primitive para a string Go
// correspondente. ok é false para qualquer nome fora da tabela §design 3.3.
func GoPrimitive(name string) (string, bool) {
	s, ok := goPrimitives[name]
	return s, ok
}

// goOpaqueTypes mapeia os tipos de arquivo (opacos no types.Model — tratados
// como primitivos, sem membros checáveis) para as structs built-in do runtime.
var goOpaqueTypes = map[string]string{
	"File":       "runtime.File",
	"FileStream": "runtime.FileStream",
	"FileRef":    "runtime.FileRef",
}

// GoOpaqueType mapeia um dos três tipos opacos de arquivo para a struct do
// runtime correspondente. ok é false fora desses três.
func GoOpaqueType(name string) (string, bool) {
	s, ok := goOpaqueTypes[name]
	return s, ok
}

// genericArgc é a contagem de argumentos de tipo esperada por construtor de
// coleção (§2.4 do spec): List/AppendList/Set são unários, Map é binário.
var genericArgc = map[string]int{
	"List":       1,
	"AppendList": 1,
	"Set":        1,
	"Map":        2,
}

// GoGeneric mapeia um construtor de coleção (types.Generic.Ctor) e os tipos Go
// já resolvidos dos seus argumentos para a forma Go da coleção. ok é false
// para um ctor desconhecido ou uma contagem de args que não bate com o
// esperado (tratado como erro de geração, nunca panic/index-out-of-range).
func GoGeneric(ctor string, args []string) (string, bool) {
	want, known := genericArgc[ctor]
	if !known || len(args) != want {
		return "", false
	}
	switch ctor {
	case "List":
		return "[]" + args[0], true
	case "AppendList":
		return fmt.Sprintf("runtime.AppendList[%s]", args[0]), true
	case "Set":
		return fmt.Sprintf("map[%s]struct{}", args[0]), true
	case "Map":
		return fmt.Sprintf("map[%s]%s", args[0], args[1]), true
	}
	return "", false // inalcançável: todo ctor em genericArgc é tratado acima
}

// BuiltinMethod identifica um método embutido sobre um receptor por seu
// "shape" de tipo (não o tipo concreto — várias instâncias de string/AppendList
// compartilham o mesmo método), mais o nome do método DomainScript.
type BuiltinMethod struct {
	Receiver string // "string" | "AppendList" (o Ctor de types.Generic, ou "string" para o primitivo)
	Method   string // nome do método como escrito em DomainScript, ex. "length", "add"
}

// builtinArity é a quantidade de argumentos esperada por par (Receiver,
// Method) — usada só para validar a chamada; a emissão em si é o switch de
// GoBuiltinCall.
var builtinArity = map[BuiltinMethod]int{
	{Receiver: "string", Method: "length"}:  0,
	{Receiver: "AppendList", Method: "add"}: 1,
}

// GoBuiltinCall mapeia um BuiltinMethod para como ele é emitido em Go, dado o
// texto já lowerizado do receptor (recv) e dos argumentos (args, já como
// strings Go prontas). ok é false se o par (Receiver, Method) não é
// reconhecido, ou se a aridade de args não bate — é erro de geração (§design
// 3.6: "um par ausente é erro de geração, não Go arbitrário"), não algo para
// o chamador inventar.
//
// distinct/sum/focus (§20) entram no Marco F — não implementados aqui.
func GoBuiltinCall(recv string, m BuiltinMethod, args []string) (string, bool) {
	want, ok := builtinArity[m]
	if !ok || len(args) != want {
		return "", false
	}
	switch m {
	case BuiltinMethod{Receiver: "string", Method: "length"}:
		return fmt.Sprintf("len(%s)", recv), true
	case BuiltinMethod{Receiver: "AppendList", Method: "add"}:
		return fmt.Sprintf("%s.Add(%s)", recv, args[0]), true
	}
	return "", false // inalcançável: todo par em builtinArity é tratado acima
}
