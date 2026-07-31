ISSUE: [[docs/sdd/issues/spec-v7-catalogo-de-metodos-embutidos]]
Solução:
Para os tipos primitivos vamos dar suporte a todos os métodos e funções presentes em sua contraparte no golang, um detalhe: tipos primitivos não tem "propriedades" em domainscript porém como sintax sugar, se um método não necessita de parametros então ele deve poder ser invocado sem os parenteses, ex:

```cs
ValueObject TransactionDescription(string) {
    Valid { self.length <= 256 } //self.length é o mesmo que self.length()
}
```
Nota: isso faz com que domainscript tenha como target golang para transpilação, isso é esperado