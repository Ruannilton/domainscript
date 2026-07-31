## Tipos de Referencia

Tipos de referencia (popularmente conhecidos como Id) são wrapers em torno de primitivos para garantir type safety quando estivermos lidando com com Ids, sua tipagem é dada por a palavra reservada **ref** + tipo do agregado.
A exemplo o id do agregado Person será do tipo ref Person:
```
Aggregate Person {
    storage {
        state: PersonDb
        document: DocumentStorage    // campo FileRef → FileStorage específico
    }
    
    state {
        name HolderName
        document FileRef
    }
    
    Handle AttachDocument(file FileRef) {
        emit DocumentAttached(self.id, file) // a propriedade id é implicita a todo agregado
    }
}
```

Por padrão, internamente uma ref usa UUID v7 para garantir unicidade ordenável, podendo ser configurado para as seguintes opçoes:
- UUID v7
- string
- integer
Obs: Preciso pensar em uma sintaxe para indicar quando o id é auto gerado e se é uma geração incremental ou sequencial