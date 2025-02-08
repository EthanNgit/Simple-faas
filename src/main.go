package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func createFunction(context *gin.Context) {
	context.IndentedJSON(http.StatusCreated, "Created function")
}

func invokeFunction(context *gin.Context) {
	context.IndentedJSON(http.StatusOK, "Invoked function")
}

func main() {

	// Setup api routes
	router := gin.Default()
	router.POST("/api/functions/v1/create", createFunction)
	router.POST("/api/functions/v1/invoke", invokeFunction)

	// Run the api loop
	fmt.Println("Starting server")
	router.Run(":8080")
}
