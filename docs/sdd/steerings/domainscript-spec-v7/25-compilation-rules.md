# 25. Regras de Compilação (Resumo)

| Regra | Resultado |
|-------|-----------|
| Primitivo no Write Side | ❌ Erro |
| Handle sem entrada no `access` | ❌ Erro |
| `Notification` sem `Adapter` | ❌ Erro |
| `remove()`/`clear()` em `AppendList<T>` | ❌ Erro |
| UseCase cross-database sem XA / cross-service sem Saga | ❌ Erro |
| JOIN cross-database | ❌ Erro |
| `match` não-exaustivo / guards sem `_` | ❌ Erro |
| `Nop` em Handle/UseCase | ❌ Erro |
| `break`/`continue` fora de `for` | ❌ Erro |
| Policy cross-module escutando `Event` não público | ❌ Erro |
| Módulos em services diferentes sem canal | ❌ Erro |
| Acesso cross-tenant sem opt-in | ❌ Erro |
| Upcast de API com campo obrigatório sem default | ❌ Erro |
| Teste referenciando evento/comando inexistente | ❌ Erro |
| FFI/Adapter com assinatura incompatível | ❌ Erro |
| Aggregate cruzando fronteira FFI | ❌ Erro |
| FFI em `Apply` (pure ou impure) | ❌ Erro |
| FFI impura em Handle sem captura em evento | ❌ Erro |
| FFI impura em Query/ValueObject | ❌ Erro |
| Idempotency conflito (mesma chave, command diferente) | ❌ 422 |
| Canal `queue`/`stream` sem `orderBy` | ⚠️ Warning |
| Saga `await` sobre canal `queue` | ⚠️ Warning |
| Upcast substituível por default | ⚠️ Warning |
| ValueObject que poderia ser Enum | ⚠️ Warning |
| Cache em listagem de alta cardinalidade | ⚠️ Warning |
| UseCase cross-tenant declarado | ⚠️ Warning (auditoria) |
| Handle sem cenário de erro testado | ⚠️ Warning |
| UseCase/Query não exposto em interface | ⚠️ Warning |
| FFI impura dentro de transação | ⚠️ Warning (efeito não revertido) |
| Provider cloud sem equivalente local (profile dev) | ⚠️ Warning |

