sk-bridge-f4a96c70d9982ba1e761e9bf6bcb6c733d3dc2e5ec5035e1e3b540d436fc9b52

curl -X POST http://localhost:4000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-bridge-f4a96c70d9982ba1e761e9bf6bcb6c733d3dc2e5ec5035e1e3b540d436fc9b52' \
  -d '{
    "messages": [
      {"role": "user", "content": "Quels sont les différentes techniques de chunking ? "}
    ]
  }'
