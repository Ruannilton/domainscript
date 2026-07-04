// Package sqlrt embute o código-fonte do adapter de persistência opt-in sobre
// database/sql (G1, §design codegen 3.11): mesmo padrão de codegen/rtsrc
// (arquivos .go.txt via //go:embed, para não compilar junto do compilador),
// mas num pacote SEPARADO e SÓ EMITIDO quando o programa realmente precisa —
// é o único lugar do gerado que importa database/sql e o driver concreto
// (modernc.org/sqlite), mantendo o núcleo (runtime/, codegen/rtsrc) sem
// nenhuma dependência externa (NFR-12). O pacote gerado a partir daqui chama-
// se "sqlruntime" (ver a doc de cada .go.txt) — nome deliberadamente distinto
// de "runtime" (o pacote core) para deixar o import claro em quem consome.
package sqlrt
