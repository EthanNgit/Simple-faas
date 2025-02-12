# FaaS (Functions as a Service) Platform

A lightweight, Docker-based Functions as a Service platform that allows you to dynamically create and invoke Python functions.

## Features

✅ Dynamic function creation and registration  
✅ Stateless function invocation  
✅ Persistent function storage  
✅ Supports warm starts  
✅ Automatic container lifecycle management  

## Limitations

⚠️ No code execution security measures  
⚠️ No authentication/authorization system  
⚠️ Python-only support  
⚠️ Single instance deployment (no load balancing)  
⚠️ No api to delete functions  

## Prerequisites

- Docker
- Docker Compose
- cURL (for API examples)

## Getting Started

1. Ensure Docker is running on your system

2. Build the base image:
```bash
docker build -t faas-base-image -f ./base/Dockerfile.faas-base ./base
```

3. Build and start the services:
```bash
docker compose build
docker compose up -d
```

## Usage

### Registering a Function

```bash
curl -X POST -H "Content-Type: application/json" -d '{
    "name": "hello",
    "code": "def user_function(name): return f\"Hello, {name}!\""
}' http://localhost:8080/register
```

Expected response:
```json
{
    "result": "hello-1"
}
```

### Invoking a Function

```bash
curl -X POST -H "Content-Type: application/json" -d '{
    "name": "hello-1",
    "params": {
        "name": "World"
    }
}' http://localhost:8080/invoke
```

Expected response:
```json
{
    "result": "Hello, World!"
}
```

## API Reference

### POST /register
Registers a new function in the system.

**Request Body:**
- `name` (string): Function name
- `code` (string): Python function code

**Response:**
- `result` (string): Unique function identifier

### POST /invoke
Invokes a registered function.

**Request Body:**
- `name` (string): Function identifier
- `params` (object): Function parameters

**Response:**
- `result` (any): Function execution result

## ENV Reference
- `CLEANUP_INTERVAL` (#m, eg 1m): polls the running containers every # minutes
- `DELETE_AFTER` (#m, eg 1m): if a container was ran more than # minutes ago, shut it down

## Contributing

Feel free to open issues and pull requests to help improve this project.

## License
MIT
