# 22. Smart Partial Loading

```ds
item = state.items.focus(itemId)              // SELECT * WHERE parent_id=X AND id=Y
ensure state.items.sum(i => i.price) < 10000  // SELECT SUM(...) sem materializar
```

`AppendList<T>` com `skip/take` → paginação nativa. Fallback: carrega aggregate todo.

