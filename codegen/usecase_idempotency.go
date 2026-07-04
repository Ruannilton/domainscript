package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/token"
)

// usecase_idempotency.go emite a idempotência real de Command (G2, REQ-20.4,
// spec §14): wrapping da função pública de um UseCase que declara
// "idempotency { ... }" em torno do corpo de sempre (a mesma unit of work de
// E7.2/G1, intocada) — chave do cliente (runtime.IdempotencyKeyFrom, carrier
// desde E9.2), cache de sucesso/erro de negócio, conflito (mesma chave +
// Command diferente) e corrida da mesma chave (wait/reject conforme
// concurrentRetry). Ver codegen/rtsrc/idempotency.go.txt para o contrato
// Begin/Wait/Complete/Release que este arquivo dirige.
//
// --- Por que um wrapper, não um corpo reescrito (§design 3.8 preservado) ---
//
// emitUseCaseDecl (decl_usecase.go) continua emitindo EXATAMENTE o mesmo
// corpo (timeout/caller/uow.Run) de antes — só que, quando decl.Idempotency
// != nil, sob um nome PRIVADO ("<nome>Run" — unexportedRunName abaixo) em vez
// do nome público do UseCase. A função pública (o nome de sempre, o único
// símbolo que decl_query.go/http.go/o resto do gerado continuam chamando)
// vira o wrapper deste arquivo, que decide replay/conflito/corrida ANTES de
// chamar "<nome>Run" no caminho feliz. Um UseCase SEM idempotency (o caso
// comum — nenhum UseCase do wallet/shop a declara) não muda NADA: o nome
// público continua sendo a própria função com o corpo, byte a byte igual à
// saída de antes desta task.
//
// --- Config: module (mod.ds Idempotency{}) + per-UseCase (override) ---
//
// concurrentRetry/concurrentTimeout só existem no bloco de módulo (nenhum
// exemplo do spec os repete por UseCase); required/window podem vir dos
// DOIS lugares, com o bloco do UseCase (decl.Idempotency) tendo prioridade —
// mesma relação "module default, UseCase override" que Database/FileStorage
// já estabelecem para outras infra (program.Module).

// unexportedRunName devolve o nome privado da função que carrega o corpo de
// verdade de um UseCase idempotente — "PerformDeposit" -> "performDepositRun"
// — mesma convenção de minúsculo-inicial de sagaBase (decl_saga.go)/
// tickFuncName (decl_worker.go), com o sufixo "Run" evitando colisão com
// qualquer outro símbolo privado do pacote.
func unexportedRunName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:] + "Run"
}

// --- 1. Leitura de config: bool/ident/duration sobre []ast.ConfigEntry. ---

// configBoolEntry busca entries[key] e exige um literal booleano (true/false)
// — mesmo padrão de workerConfigInt (decl_worker.go), para outro tipo de
// literal. ok=false sem erro quando a chave está ausente.
func configBoolEntry(entries []ast.ConfigEntry, key string) (val bool, ok bool, err error) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		lit, isLit := entry.Value.(*ast.Literal)
		if !isLit || (lit.Kind != token.TRUE && lit.Kind != token.FALSE) {
			return false, false, fmt.Errorf("%s: esperava um literal booleano, veio %T", key, entry.Value)
		}
		return lit.Kind == token.TRUE, true, nil
	}
	return false, false, nil
}

// configIdentEntry busca entries[key] e exige um Ident nu (ex. "concurrentRetry:
// wait", sem aspas) — a forma que o mod.ds real do spec usa para essa
// chave (§12). ok=false sem erro quando a chave está ausente.
func configIdentEntry(entries []ast.ConfigEntry, key string) (name string, ok bool, err error) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		id, isIdent := entry.Value.(*ast.Ident)
		if !isIdent {
			return "", false, fmt.Errorf("%s: esperava um identificador nu, veio %T", key, entry.Value)
		}
		return id.Name, true, nil
	}
	return "", false, nil
}

// configDurationEntry busca entries[key] e loweriza seu valor (um literal
// DURATION) via l — mesmo padrão de workerDurationSetting (decl_worker.go),
// reaproveitado aqui verbatim (a assinatura já é genérica sobre
// []ast.ConfigEntry). Fatorado como alias com nome local só para manter a
// leitura deste arquivo autocontida.
func configDurationEntry(entries []ast.ConfigEntry, key string, l *lower.Lowerer) (goExpr string, ok bool, err error) {
	return workerDurationSetting(entries, key, l)
}

// moduleIdempotencyBlock devolve o bloco "Idempotency { ... }" do mod.ds de
// mod (§12), ou nil quando mod é nil ou não declara um — mesmo padrão de
// moduleFileStorageDefault/moduleFileStorageNames (decl_aggregate_storage.go)
// para outro Kind de ConfigBlock.
func moduleIdempotencyBlock(mod *program.Module) *ast.ConfigBlock {
	if mod == nil || mod.Decl == nil {
		return nil
	}
	for _, b := range mod.Decl.Blocks {
		if b.Kind == "Idempotency" {
			return b
		}
	}
	return nil
}

// --- 2. O plano resolvido de um UseCase idempotente. ---

// usecaseIdempotencyPlan é a config de idempotência já resolvida (module
// default + override por UseCase, ver a doc do arquivo) para UM UseCase —
// nil (via planUseCaseIdempotency) quando o UseCase não declara
// "idempotency" (o caso comum, nenhuma mudança de comportamento).
type usecaseIdempotencyPlan struct {
	required bool
	// windowGo é a expressão Go (time.Duration) do "window" efetivo, "" quando
	// nenhuma fonte (UseCase nem módulo) declara um — o gerado passa o
	// literal Go "0" nesse caso, e Complete (idempotency.go.txt) aplica
	// defaultIdempotencyTTL.
	windowGo string
	// concurrentReject é true quando o módulo declara "concurrentRetry:
	// reject"; false (o default, inclusive sem bloco Idempotency algum no
	// mod.ds) aplica "wait" — a escolha mais segura na ausência de config
	// explícita (nunca falha uma requisição só por ela ter chegado durante o
	// processamento de uma idêntica).
	concurrentReject bool
	// concurrentTimeoutGo é a expressão Go (time.Duration) que limita quanto
	// tempo um "wait" espera o run em voo — "30s" (o mesmo default do
	// exemplo de mod.ds do spec §12) quando o módulo não declara
	// "concurrentTimeout".
	concurrentTimeoutGo string
}

// planUseCaseIdempotency resolve a config efetiva de decl.Idempotency (nil
// devolve (nil, nil): UseCase não usa idempotência) contra o bloco de módulo
// moduleBlock (pode ser nil: mod.ds sem Idempotency{} algum — todo default
// vale). l loweriza os literais DURATION (window/concurrentTimeout) para Go.
func planUseCaseIdempotency(decl *ast.UseCaseDecl, moduleBlock *ast.ConfigBlock, l *lower.Lowerer) (*usecaseIdempotencyPlan, error) {
	if decl.Idempotency == nil {
		return nil, nil
	}
	obj, ok := decl.Idempotency.(*ast.ObjectExpr)
	if !ok {
		return nil, fmt.Errorf("UseCase %s: idempotency: esperava um objeto de configuração (\"idempotency { required: ..., window: ... }\"), veio %T", decl.Name, decl.Idempotency)
	}

	var moduleEntries []ast.ConfigEntry
	if moduleBlock != nil {
		moduleEntries = moduleBlock.Entries
	}

	required, hasRequired, err := configBoolEntry(obj.Entries, "required")
	if err != nil {
		return nil, fmt.Errorf("UseCase %s: idempotency.%w", decl.Name, err)
	}
	if !hasRequired {
		required, _, err = configBoolEntry(moduleEntries, "required")
		if err != nil {
			return nil, fmt.Errorf("UseCase %s: mod.ds Idempotency.%w", decl.Name, err)
		}
	}

	windowGo, hasWindow, err := configDurationEntry(obj.Entries, "window", l)
	if err != nil {
		return nil, fmt.Errorf("UseCase %s: idempotency.%w", decl.Name, err)
	}
	if !hasWindow {
		windowGo, _, err = configDurationEntry(moduleEntries, "window", l)
		if err != nil {
			return nil, fmt.Errorf("UseCase %s: mod.ds Idempotency.%w", decl.Name, err)
		}
	}

	concurrentReject := false
	if name, has, err := configIdentEntry(moduleEntries, "concurrentRetry"); err != nil {
		return nil, fmt.Errorf("UseCase %s: mod.ds Idempotency.%w", decl.Name, err)
	} else if has && name == "reject" {
		concurrentReject = true
	}

	concurrentTimeoutGo, hasTimeout, err := configDurationEntry(moduleEntries, "concurrentTimeout", l)
	if err != nil {
		return nil, fmt.Errorf("UseCase %s: mod.ds Idempotency.%w", decl.Name, err)
	}
	if !hasTimeout {
		// 30s, em nanossegundos — mesmo formato que lowerDurationLiteral
		// (lower/expr.go) produz para um literal DURATION real, e o mesmo
		// default do exemplo de mod.ds do spec (§12: "concurrentTimeout: 30s").
		concurrentTimeoutGo = "time.Duration(30000000000)"
	}

	return &usecaseIdempotencyPlan{
		required:            required,
		windowGo:            windowGo,
		concurrentReject:    concurrentReject,
		concurrentTimeoutGo: concurrentTimeoutGo,
	}, nil
}

// --- 3. Var de pacote + worker de limpeza automático (spec §14). ---

// idempotencyCleanupInterval é o intervalo fixo do worker de limpeza gerado
// automaticamente (emitIdempotencyCleanupStarter) — deliberadamente uma
// constante simples, não derivada do menor "window" configurado: o mesmo
// espírito de runtime.RateLimiter (codegen/rtsrc/worker.go.txt) preferir um
// limitador de intervalo fixo simples a um mecanismo mais elaborado sem um
// exemplo real que peça por ele. 1 minuto mantém chaves expiradas fora do
// mapa em memória sem exigir nenhuma config nova do dev (spec §14: "Worker
// gerado automaticamente" — nenhuma declaração .ds controla isto).
const idempotencyCleanupInterval = "time.Minute"

// emitIdempotencyStoreVar emite "var idem runtime.IdempotencyStore = ...":
// UMA declaração de pacote por módulo (mesmo padrão de "var uow", mas SEM
// Wire — como o SagaStore de uma Saga async (decl_saga.go), a instância
// default in-memory é suficiente para este marco; um backend "external" real
// [Redis/Dynamo] entra atrás do MESMO runtime.IdempotencyStore em trabalho
// futuro opt-in, sem mudar nenhum call site gerado, ver codegen/rtsrc/
// idempotency.go.txt).
func emitIdempotencyStoreVar(e *emit.Emitter, runtimeAlias string) {
	e.Line("")
	e.Line("// idem é o idempotency store do módulo (spec §14, G2), compartilhado por")
	e.Line("// todo UseCase que declara \"idempotency { ... }\" — o default in-memory")
	e.Line("// (\"storage: same\", ver a doc de idempotency.go.txt); sem Wire, mesmo")
	e.Line("// padrão do SagaStore de uma Saga async (decl_saga.go).")
	e.Line("var idem %s.IdempotencyStore = %s.NewMemoryIdempotencyStore()", runtimeAlias, runtimeAlias)
}

// emitIdempotencyCleanupStarter emite "func StartIdempotencyCleanup(ctx)":
// o worker de limpeza de chaves expiradas gerado AUTOMATICAMENTE (spec §14:
// "Limpeza de chaves expiradas: Worker gerado automaticamente" — nunca
// declarado pelo dev em .ds) — reusa a MESMA primitiva de agendamento de um
// Worker "every" (F2, time.Ticker — ver decl_worker.go/emitEvery), chamando
// idem.Cleanup a cada idempotencyCleanupInterval. Nome próprio, nunca "Wire"
// (mesma razão de "StartWorkers", ver a doc de decl_worker.go sobre a
// colisão de F1/F2) — cmd/<service>/main.go chama isto ao lado de
// StartWorkers/Wire (ver codegen.go, generateCmdMainFile).
func emitIdempotencyCleanupStarter(e *emit.Emitter, ctxAlias, runtimeAlias string) {
	timeAlias := e.Import("time")
	slogAlias := e.Import("log/slog")

	e.Line("")
	e.Line("// StartIdempotencyCleanup roda o worker de limpeza de chaves de idempotência")
	e.Line("// expiradas (spec §14, G2) — gerado automaticamente porque este módulo usa")
	e.Line("// idempotência; chamado por cmd/<service>/main.go na inicialização, ao lado")
	e.Line("// de StartWorkers/Wire. Roda até ctx ser cancelado.")
	e.Block(fmt.Sprintf("func StartIdempotencyCleanup(ctx %s.Context)", ctxAlias), func() {
		e.Line("ticker := %s.NewTicker(%s)", timeAlias, idempotencyCleanupInterval)
		e.Line("defer ticker.Stop()")
		e.Block("for", func() {
			e.Block("select", func() {
				e.Line("case <-ctx.Done():")
				e.Line("return")
				e.Line("case <-ticker.C:")
			})
			e.Block(fmt.Sprintf("if _, err := idem.Cleanup(ctx, %s.Now()); err != nil", timeAlias), func() {
				e.Line("%s.Error(%q, %q, err)", slogAlias, "idempotency: falha na limpeza de chaves expiradas", "error")
			})
		})
	})
}

// --- 4. Emissão do wrapper público. ---

// emitIdempotencyWrapper emite "func <decl.Name>(ctx, cmd) error" (o nome
// PÚBLICO do UseCase — ver a doc do arquivo): a dança Begin/Wait/Complete/
// Release em torno de uma chamada a runFn (o nome privado que carrega o
// corpo de sempre, emitido por emitUseCaseDecl).
func emitIdempotencyWrapper(e *emit.Emitter, decl *ast.UseCaseDecl, plan *usecaseIdempotencyPlan, runFn, ctxAlias, runtimeAlias string) {
	e.Import("time")
	windowGo := plan.windowGo
	if windowGo == "" {
		windowGo = "0"
	}

	e.Line("")
	e.Line("// %s é a borda idempotente do UseCase %s (spec §14, G2): decide, a partir", decl.Name, decl.Name)
	e.Line("// da Idempotency-Key do chamador (runtime.IdempotencyKeyFrom), se roda %s de", runFn)
	e.Line("// verdade, repete um resultado já cacheado (sucesso ou erro de negócio),")
	e.Line("// recusa um conflito (mesma chave, Command diferente) ou aplica a política")
	e.Line("// de corrida (concurrentRetry) sobre um run concorrente da MESMA chave.")
	sig := fmt.Sprintf("func %s(ctx %s.Context, cmd %s) error", decl.Name, ctxAlias, decl.Handles)
	e.Block(sig, func() {
		e.Line("key, hasKey := %s.IdempotencyKeyFrom(ctx)", runtimeAlias)
		e.Block("if !hasKey", func() {
			if plan.required {
				e.Line("return %s.ErrIdempotencyKeyRequired", runtimeAlias)
			} else {
				e.Line("return %s(ctx, cmd)", runFn)
			}
		})
		e.Line("")
		e.Line("fingerprint := %s.IdempotencyFingerprint(cmd)", runtimeAlias)
		e.Block("for", func() {
			e.Line("begin, err := idem.Begin(ctx, key, fingerprint)")
			e.Block("if err != nil", func() {
				e.Line("return err")
			})
			e.Line("switch begin.Outcome {")
			e.Line("case %s.BeginConflict:", runtimeAlias)
			e.Line("return %s.ErrIdempotencyKeyConflict", runtimeAlias)
			e.Line("case %s.BeginCached:", runtimeAlias)
			e.Line("return begin.Cached.Err()")
			e.Line("case %s.BeginInFlight:", runtimeAlias)
			if plan.concurrentReject {
				e.Line("return %s.ErrIdempotencyInFlight", runtimeAlias)
			} else {
				e.Line("waitCtx, cancel := %s.WithTimeout(ctx, %s)", ctxAlias, plan.concurrentTimeoutGo)
				e.Line("wr, err := idem.Wait(waitCtx, key)")
				e.Line("cancel()")
				e.Block("if err != nil", func() {
					e.Line("return err")
				})
				e.Block(fmt.Sprintf("if wr.Outcome == %s.WaitReleased", runtimeAlias), func() {
					e.Line("continue")
				})
				e.Line("return wr.Result.Err()")
			}
			e.Line("}")
			e.Line("")
			e.Line("runErr := %s(ctx, cmd)", runFn)
			e.Block(fmt.Sprintf("if runErr != nil && !%s.IsBusinessError(runErr)", runtimeAlias), func() {
				e.Line("_ = idem.Release(ctx, key)")
				e.Line("return runErr")
			})
			e.Line("_ = idem.Complete(ctx, key, %s.NewCompletedResult(runErr), %s)", runtimeAlias, windowGo)
			e.Line("return runErr")
		})
	})
}
