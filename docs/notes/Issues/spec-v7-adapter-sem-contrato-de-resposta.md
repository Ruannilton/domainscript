ISSUE: [[docs/sdd/issues/spec-v7-adapter-sem-contrato-de-resposta]]
Solução:
Utilizar a sintaxes -> para indicar o tipo da resposta, exemplo:
```cs
Notification DepositNotification { to Email, amount Money } -> DepositNotificationResponse
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod } -> PaymentRequestResponse
Notification SendMessage { to Email, msg Message } // sem retorn ("retorna void")
```
Onde DepositNotificationResponse e PaymentRequestResponse são ValueObjects