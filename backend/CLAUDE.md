# Golang Best Practices

- Better use concrete types instead of map[string]any, if there is none, define one instead.
- Don't forget to update annotations on API handers in [[backend/internal/api/handlers.go]] to include new endpoints in swagger documents.
