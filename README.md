## Instructions
Make sure docker is running\
Build base image `docker build -t faas-base-image -f Dockerfile.faas-base .`\
Build the compose `docker compose build`\
Run compose `docker compose up -d`

To create a function\
`curl -X POST -H "Content-Type: application/json" -d '{
  "name": "hello",
  "code": "def user_function(name): return f'Hello, {name}!'"
}' http://localhost:8080/register`

To invoke that function\
`curl -X POST -H "Content-Type: application/json" -d '{
  "name": "hello",
  "params": {"name": "World"}
}' http://localhost:8080/invoke`
