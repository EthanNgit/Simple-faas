package main

import (
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
	router := gin.Default()
	router.POST("/api/functions/v1/create", createFunction)
	router.POST("/api/functions/v1/invoke", invokeFunction)
	router.Run("localhost:8080")
}
