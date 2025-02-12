package main

import (
	"fmt"

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

func main() {
	engine, err := NewEngine()
	if err != nil {
		panic(err)
	}
	defer engine.Close()

	fmt.Printf("[FAAS-debug] Engine id %s\n", engine.engId)

	// setup api routes
	router := gin.Default()
	router.POST("/api/functions/v1/create", engine.handleCreate)
	router.POST("/api/functions/v1/invoke", engine.handleInvoke)

	// run the api loop
	fmt.Println("[FAAS-debug] Starting server")
	router.Run(":8080")
}
