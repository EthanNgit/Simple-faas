package main

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
)

type CreateRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type InvokeRequest struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

type FuncDef struct {
	FuncCode string
	FuncCntr string
}

type Engine struct {
	cntrCli   *client.Client
	functions map[string]FuncDef
	netName   string
	engId     string
	mu        sync.Mutex
}

func createHandler(context *gin.Context) {
	context.IndentedJSON(http.StatusCreated, "Created function")
}

func invokeHandler(context *gin.Context) {
	context.IndentedJSON(http.StatusOK, "Invoked function")
}

func main() {

	// Setup api routes
	router := gin.Default()
	router.POST("/api/functions/v1/create", createHandler)
	router.POST("/api/functions/v1/invoke", invokeHandler)

	// Run the api loop
	fmt.Println("Starting server")
	router.Run(":8080")
}
