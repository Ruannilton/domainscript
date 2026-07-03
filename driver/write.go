package driver

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"domainscript/codegen"
)

// manifestName é o arquivo que writeOutput mantém na raiz de out para saber,
// na PRÓXIMA geração, quais caminhos a geração ANTERIOR escreveu — sem ele
// não haveria como distinguir "arquivo órfão de uma declaração removida do
// .ds" de "arquivo que já estava em out por algum outro motivo, de antes de
// dsc gen nunca ter escrito ali" (REQ-32.3, §design 3.15/4.1). Extensão
// não-".go" de propósito: nunca é varrido por "go build ./..."/"go vet
// ./..." dentro do projeto gerado.
const manifestName = ".dsc-manifest"

// writeOutput escreve files em out de forma idempotente (REQ-32.3, NFR-13):
//
//  1. Um arquivo cujo conteúdo não mudou não é reescrito — preserva mtime,
//     evita diffs espúrios ao versionar o gerado.
//  2. Um caminho que a geração ANTERIOR escreveu (registrado no manifesto,
//     manifestName) mas que files não contém mais — porque a declaração que
//     o originou foi removida do .ds — é apagado do disco, junto de
//     diretórios de módulo/cmd que ficam vazios como consequência.
//  3. Sem manifesto anterior (out nunca foi escrito por dsc gen), nenhum
//     arquivo pré-existente em out é tocado além dos que files sobrescreve:
//     dsc gen nunca varre out "às cegas" torcendo para não apagar algo que
//     não gerou — só limpa o que ele mesmo registrou ter gerado antes.
func writeOutput(out string, files []codegen.File) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("driver: não consegui criar %q: %w", out, err)
	}

	prev := readManifest(out)

	current := make(map[string]bool, len(files))
	for _, f := range files {
		rel := filepath.FromSlash(f.Path)
		current[rel] = true
		target := filepath.Join(out, rel)
		if err := writeIfChanged(target, f.Content); err != nil {
			return fmt.Errorf("driver: não consegui escrever %q: %w", target, err)
		}
	}

	for _, rel := range prev {
		if current[rel] {
			continue
		}
		target := filepath.Join(out, rel)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("driver: não consegui remover artefato órfão %q: %w", target, err)
		}
		removeEmptyDirs(out, filepath.Dir(rel))
	}

	if err := writeIfChanged(filepath.Join(out, manifestName), manifestContent(files)); err != nil {
		return fmt.Errorf("driver: não consegui escrever o manifesto de %q: %w", out, err)
	}
	return nil
}

// writeIfChanged só grava content em path quando difere do que já está lá
// (ou path ainda não existe) — o coração da idempotência (NFR-13): regenerar
// sem mudanças no .ds não deve tocar mtime nem produzir diffs.
func writeIfChanged(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// removeEmptyDirs apaga o diretório rel (relativo a out) e sobe apagando
// pais enquanto ficarem vazios — o efeito colateral esperado quando o
// último arquivo gerado de um módulo/cmd some (ex.: um módulo perde sua
// última declaração). Nunca apaga out em si nem sobe acima dele.
func removeEmptyDirs(out, rel string) {
	for rel != "." && rel != string(filepath.Separator) && rel != "" {
		full := filepath.Join(out, rel)
		entries, err := os.ReadDir(full)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(full); err != nil {
			return
		}
		rel = filepath.Dir(rel)
	}
}

// readManifest lê os caminhos relativos (convertidos para o separador do SO)
// que a geração anterior escreveu em out; nil se não há manifesto (1ª
// geração nesse out, ou out nunca foi escrito por dsc gen).
func readManifest(out string) []string {
	data, err := os.ReadFile(filepath.Join(out, manifestName))
	if err != nil {
		return nil
	}
	var rels []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rels = append(rels, filepath.FromSlash(line))
	}
	return rels
}

// manifestContent serializa os Path de files (já "/"-separados; Generate os
// devolve ordenados, NFR-13) um por linha, ordenados de novo aqui por
// defensividade — determinístico: mesma entrada, mesmo manifesto byte a
// byte, o que por sua vez é o que garante que writeIfChanged não reescreva o
// manifesto (e não toque seu mtime) entre duas gerações idênticas.
func manifestContent(files []codegen.File) []byte {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(paths, "\n") + "\n")
}
