package codegen

import (
	"strings"
	"testing"

	"domainscript/program"
)

// sql_wiring_test.go prova a DoD de J1.2 (REQ-41.2/41.3, §design
// infra-providers 3.1): "postgres" passa a ser um provider SQL real
// reconhecido por sqlProviders/activeSQLProviders/EmitGoMod, ao lado de
// "sqlite" — sem nenhum outro provider ativo, nada muda (NFR-21).

func postgresDBProgram(dbProvider string) *program.Program {
	return &program.Program{
		Modules: map[string]*program.Module{
			"Wallet": {
				Name: "Wallet",
				Databases: map[string]*program.Database{
					"MainDb": {Name: "MainDb", Module: "Wallet", Provider: dbProvider, Manages: []string{"Wallet"}},
				},
			},
		},
	}
}

// TestActiveSQLProvidersRecognizesPostgres prova REQ-41.2: um Database com
// provider:"postgres" (case-insensitive, mesma regra de "sqlite") aparece em
// activeSQLProviders — o gate que liga sqlruntime/* e o require de go.mod.
func TestActiveSQLProvidersRecognizesPostgres(t *testing.T) {
	for _, provider := range []string{"postgres", "Postgres", "POSTGRES"} {
		prog := postgresDBProgram(provider)
		got := activeSQLProviders(prog)
		if len(got) != 1 || got[0] != "postgres" {
			t.Fatalf("activeSQLProviders(%q) = %v, want [\"postgres\"]", provider, got)
		}
		if !programNeedsSQLAdapter(prog) {
			t.Fatalf("programNeedsSQLAdapter(%q) = false, want true", provider)
		}
	}
}

// TestActiveSQLProvidersUnrecognizedProviderIsNFR21NoOp prova NFR-21: um
// provider que não é "sqlite" nem "postgres" (o estado "decorativo" que
// muitas fixtures deste repositório usam de propósito, ex. "pg") não ativa
// nada — activeSQLProviders vazio, programNeedsSQLAdapter false.
func TestActiveSQLProvidersUnrecognizedProviderIsNFR21NoOp(t *testing.T) {
	prog := postgresDBProgram("pg")
	if got := activeSQLProviders(prog); len(got) != 0 {
		t.Fatalf("activeSQLProviders(\"pg\") = %v, want vazio (NFR-21, provider não reconhecido)", got)
	}
	if programNeedsSQLAdapter(prog) {
		t.Fatal("programNeedsSQLAdapter(\"pg\") = true, want false (NFR-21)")
	}
}

// TestEmitGoModRequiresPgxForPostgres prova REQ-41.2/41.3: EmitGoMod, dado
// activeSQLProviders == ["postgres"], exige EXATAMENTE o driver pgx (nenhum
// outro) e eleva a versão de Go para postgresMinGoVersion.
func TestEmitGoModRequiresPgxForPostgres(t *testing.T) {
	content := string(EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", []string{"postgres"}, false, false, nil))

	want := "require " + postgresDriverModule + " " + postgresDriverVersion
	if !strings.Contains(content, want) {
		t.Fatalf("esperava %q em go.mod, não achei:\n%s", want, content)
	}
	if strings.Contains(content, sqliteDriverModule) {
		t.Fatalf("go.mod não deveria exigir %s (nenhum Database sqlite ativo), achei:\n%s", sqliteDriverModule, content)
	}
	if !strings.Contains(content, "go "+postgresMinGoVersion) {
		t.Fatalf("esperava \"go %s\" (postgresMinGoVersion) em go.mod, não achei:\n%s", postgresMinGoVersion, content)
	}
}

// TestEmitGoModWithoutPostgresHasNoPgx prova NFR-21: sem "postgres" em
// sqlProviderKeys (o caso comum, e o de qualquer provider não reconhecido),
// go.mod nunca menciona o driver pgx.
func TestEmitGoModWithoutPostgresHasNoPgx(t *testing.T) {
	for _, keys := range [][]string{nil, {}, {"sqlite"}} {
		content := string(EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", keys, false, false, nil))
		if strings.Contains(content, postgresDriverModule) {
			t.Fatalf("EmitGoMod(sqlProviderKeys=%v) não deveria mencionar %s (NFR-21), achei:\n%s", keys, postgresDriverModule, content)
		}
	}
}
